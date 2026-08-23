package clientv2

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// GetLatestCheckpointSequenceNumber returns the height of the most recently
// executed checkpoint.
func (c *Client) GetLatestCheckpointSequenceNumber(ctx context.Context) (uint64, error) {
	resp, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
	if err != nil {
		return 0, fmt.Errorf("GetLatestCheckpointSequenceNumber: %w", err)
	}
	return resp.GetCheckpointHeight(), nil
}

func (c *Client) GetCheckpoint(ctx context.Context, seqNum uint64) (*types.Checkpoint, error) {
	checkpoint, err := c.getCheckpoint(ctx, seqNum, pb.CheckpointReadMaskPaths)
	if err != nil {
		return nil, fmt.Errorf("GetCheckpoint: %w", err)
	}
	return checkpoint.ToInternalType(), nil
}

// checkpointsPageLimit caps a single ListCheckpoints request. The node coerces
// larger asks down to its own maximum anyway, and the scan pages until it has
// limit checkpoints.
const checkpointsPageLimit = 1000

// GetCheckpoints returns up to limit sequential checkpoints from startSeqNum
// (inclusive), ascending, ending early at the tip of the chain. A limit <= 0
// yields no checkpoints. If the range is truncated because the node pruned the
// missing checkpoints (rather than not having produced them yet), an error is
// returned instead.
func (c *Client) GetCheckpoints(ctx context.Context, startSeqNum uint64, limit int) ([]*types.Checkpoint, error) {
	if limit <= 0 {
		return nil, nil
	}
	collected := make([]*pb.Checkpoint, 0, min(limit, checkpointsPageLimit))
	// next is the sequence number the scan owes; once it stops, the first one
	// missing.
	next := startSeqNum

	for len(collected) < limit {
		before := len(collected)
		stream, err := c.ledger.ListCheckpoints(ctx, &pb.ListCheckpointsRequest{
			ReadMask: &fieldmaskpb.FieldMask{Paths: pb.CheckpointReadMaskPaths},
			// One item per sequence number, so the scan resumes by advancing the
			// range rather than carrying the watermark's opaque cursor.
			StartCheckpoint: proto.Uint64(next),
			EndCheckpoint:   proto.Uint64(startSeqNum + uint64(limit)),
			Options: &pb.QueryOptions{
				Limit: proto.Uint32(uint32(min(limit-len(collected), checkpointsPageLimit))),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: %w", c.prunedBelowRetention(ctx, next, err))
		}
		// Without a terminal frame the stream never said the range was exhausted.
		resumable := true
		for {
			frame, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if recvErr != nil {
				return nil, fmt.Errorf("GetCheckpoints: %w", c.prunedBelowRetention(ctx, next, recvErr))
			}
			if checkpoint := frame.GetCheckpoint(); checkpoint != nil {
				// Checkpoint sequence numbers are contiguous, so a gap inside the
				// range is the node misreporting, not missing history.
				if seqNum := checkpoint.GetSequenceNumber(); seqNum != next {
					return nil, fmt.Errorf("GetCheckpoints: expected checkpoint %d, node sent %d", next, seqNum)
				}
				collected = append(collected, checkpoint)
				next++
			}
			if end := frame.GetEnd(); end != nil {
				resumable = scanMayContinue(end.GetReason())
			}
		}
		// Resuming means asking again from next, so a round that yielded nothing
		// would repeat forever.
		if !resumable || len(collected) == before {
			break
		}
	}

	if len(collected) < limit {
		// Distinguish "past the chain tip" (truncate) from "pruned" (error):
		// the scan stops short of the requested range either way.
		info, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: classifying missing checkpoint %d: %w", next, err)
		}
		if lowest := info.GetLowestAvailableCheckpoint(); next < lowest {
			return nil, fmt.Errorf("GetCheckpoints: checkpoint %d pruned; node retains from %d", next, lowest)
		}
	}
	out := make([]*types.Checkpoint, 0, len(collected))
	for _, checkpoint := range collected {
		out = append(out, checkpoint.ToInternalType())
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
