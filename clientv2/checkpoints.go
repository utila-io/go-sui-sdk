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

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// GetLatestCheckpointSequenceNumber returns the height of the most recently
// executed checkpoint as a decimal string, like JSON-RPC.
func (c *Client) GetLatestCheckpointSequenceNumber(ctx context.Context) (string, error) {
	resp, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
	if err != nil {
		return "", fmt.Errorf("GetLatestCheckpointSequenceNumber: %w", err)
	}
	return strconv.FormatUint(resp.GetCheckpointHeight(), 10), nil
}

// GetCheckpoint returns the checkpoint with the given sequence number.
func (c *Client) GetCheckpoint(ctx context.Context, seqNum uint64) (*types.Checkpoint, error) {
	checkpoint, err := c.getCheckpoint(ctx, seqNum, adapt.CheckpointReadMaskPaths)
	if err != nil {
		return nil, fmt.Errorf("GetCheckpoint: %w", err)
	}
	return adapt.Checkpoint(checkpoint), nil
}

// checkpointFetchConcurrency caps the number of parallel GetCheckpoint calls
// issued by GetCheckpoints.
const checkpointFetchConcurrency = 8

// GetCheckpoints returns up to limit sequential checkpoints starting at
// startSeqNum (inclusive), in ascending order. Like the JSON-RPC page, it ends
// early at the tip of the chain: checkpoints past the last known one are
// simply not included. A limit <= 0 yields no checkpoints, like the v1
// backend.
func (c *Client) GetCheckpoints(ctx context.Context, startSeqNum uint64, limit int) ([]*types.Checkpoint, error) {
	if limit <= 0 {
		return nil, nil
	}
	// The range is fetched with bounded concurrency. firstMissing tracks the
	// lowest offset that came back NotFound so later offsets can skip their
	// fetch: checkpoints are contiguous, so everything past the first missing
	// one is missing too. Offsets below firstMissing are never skipped, which
	// keeps the collected prefix hole-free.
	var (
		checkpoints  = make([]*pb.Checkpoint, limit)
		errs         = make([]error, limit)
		semaphore    = make(chan struct{}, checkpointFetchConcurrency)
		wg           sync.WaitGroup
		firstMissing atomic.Int64
	)
	firstMissing.Store(int64(limit))
	for i := range limit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			if int64(i) >= firstMissing.Load() {
				return
			}
			checkpoint, err := c.getCheckpoint(ctx, startSeqNum+uint64(i), adapt.CheckpointReadMaskPaths)
			if status.Code(err) == codes.NotFound {
				// Lower firstMissing to i (it only ever decreases).
				for current := firstMissing.Load(); int64(i) < current; current = firstMissing.Load() {
					if firstMissing.CompareAndSwap(current, int64(i)) {
						break
					}
				}
				return
			}
			checkpoints[i], errs[i] = checkpoint, err
		}()
	}
	wg.Wait()

	out := make([]*types.Checkpoint, 0, firstMissing.Load())
	for i := range int(firstMissing.Load()) {
		if errs[i] != nil {
			return nil, fmt.Errorf("GetCheckpoints: %w", errs[i])
		}
		out = append(out, adapt.Checkpoint(checkpoints[i]))
	}
	return out, nil
}

// GetCheckpointTransactions returns every transaction in the checkpoint,
// shaped per options and in checkpoint order. It fetches the digest list from
// the checkpoint and hydrates it via BatchGetTransactions, replacing JSON-RPC
// queryTransactionBlocks with a Checkpoint filter.
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

// getCheckpoint fetches one checkpoint by sequence number with the given
// read mask.
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
