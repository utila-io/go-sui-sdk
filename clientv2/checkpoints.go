package clientv2

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// GetLatestCheckpointSequenceNumber returns the height of the most recently
// executed checkpoint as a decimal string.
func (c *Client) GetLatestCheckpointSequenceNumber(ctx context.Context) (string, error) {
	resp, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
	if err != nil {
		return "", fmt.Errorf("GetLatestCheckpointSequenceNumber: %w", err)
	}
	return strconv.FormatUint(resp.GetCheckpointHeight(), 10), nil
}

func (c *Client) GetCheckpoint(ctx context.Context, seqNum uint64) (*types.Checkpoint, error) {
	checkpoint, err := c.getCheckpoint(ctx, seqNum, pb.CheckpointReadMaskPaths)
	if err != nil {
		return nil, fmt.Errorf("GetCheckpoint: %w", err)
	}
	return checkpoint.ToInternalType(), nil
}

const checkpointFetchConcurrency = 8

// GetCheckpoints returns up to limit sequential checkpoints from startSeqNum
// (inclusive), ascending, ending early at the tip of the chain. A limit <= 0
// yields no checkpoints. If the range is truncated because the node pruned the
// missing checkpoints (rather than not having produced them yet), an error is
// returned instead.
func (c *Client) GetCheckpoints(ctx context.Context, startSeqNum uint64, limit int) ([]*types.Checkpoint, error) {
	if limit <= 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		checkpoints = make([]*pb.Checkpoint, limit)
		firstErr    atomic.Pointer[error]
		// firstMissing is the lowest NotFound offset; only ever decreases.
		// Initialized to limit — the first-missing offset of a fully present
		// range — so it always equals the collected hole-free prefix length.
		firstMissing atomic.Int64
		indices      = make(chan int)
		wg           sync.WaitGroup
	)
	firstMissing.Store(int64(limit))

	go func() {
		defer close(indices)
		for i := range limit {
			if int64(i) >= firstMissing.Load() {
				return
			}
			select {
			case indices <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	for range min(limit, checkpointFetchConcurrency) {
		wg.Go(func() {
			for i := range indices {
				if int64(i) >= firstMissing.Load() || ctx.Err() != nil {
					continue
				}
				checkpoint, err := c.getCheckpoint(ctx, startSeqNum+uint64(i), pb.CheckpointReadMaskPaths)
				switch {
				case err == nil:
					checkpoints[i] = checkpoint
				case status.Code(err) == codes.NotFound:
					for current := firstMissing.Load(); int64(i) < current; current = firstMissing.Load() {
						if firstMissing.CompareAndSwap(current, int64(i)) {
							break
						}
					}
				default:
					if firstErr.CompareAndSwap(nil, &err) {
						cancel()
					}
				}
			}
		})
	}
	wg.Wait()

	if err := firstErr.Load(); err != nil {
		return nil, fmt.Errorf("GetCheckpoints: %w", *err)
	}
	missing := int(firstMissing.Load())
	if missing < limit {
		// Distinguish "past the chain tip" (truncate) from "pruned" (error):
		// a pruned node returns NotFound below its retention watermark too.
		missingSeq := startSeqNum + uint64(missing)
		info, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: classifying missing checkpoint %d: %w", missingSeq, err)
		}
		if lowest := info.GetLowestAvailableCheckpoint(); missingSeq < lowest {
			return nil, fmt.Errorf("GetCheckpoints: checkpoint %d pruned; node retains from %d", missingSeq, lowest)
		}
	}
	out := make([]*types.Checkpoint, 0, missing)
	for i := range missing {
		out = append(out, checkpoints[i].ToInternalType())
	}
	return out, nil
}

// GetCheckpointTransactions returns every transaction in the checkpoint,
// shaped per options and in checkpoint order.
func (c *Client) GetCheckpointTransactions(
	ctx context.Context,
	seqNum uint64,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	checkpoint, err := c.getCheckpoint(ctx, seqNum, []string{"sequence_number", "transactions.digest"})
	if err != nil {
		return nil, fmt.Errorf("GetCheckpointTransactions: %w", err)
	}
	digests := make([]string, len(checkpoint.GetTransactions()))
	for i, tx := range checkpoint.GetTransactions() {
		digests[i] = tx.GetDigest()
	}
	responses, err := c.batchGetTransactions(ctx, digests, options)
	if err != nil {
		return nil, fmt.Errorf("GetCheckpointTransactions: %w", err)
	}
	return responses, nil
}

func (c *Client) getCheckpoint(ctx context.Context, seqNum uint64, readMaskPaths []string) (*pb.Checkpoint, error) {
	resp, err := c.ledger.GetCheckpoint(ctx, &pb.GetCheckpointRequest{
		CheckpointId: &pb.GetCheckpointRequest_SequenceNumber{SequenceNumber: seqNum},
		ReadMask:     &fieldmaskpb.FieldMask{Paths: readMaskPaths},
	})
	if err != nil {
		return nil, err
	}
	return resp.GetCheckpoint(), nil
}
