package clientv2

import (
	"bytes"
	"context"
	"fmt"

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
		scanned, _, resumable, err := scanRound(stream,
			func(frame *pb.ListCheckpointsResponse) listFrame[pb.Checkpoint] {
				return listFrame[pb.Checkpoint]{
					item: frame.GetCheckpoint(),
					end:  frame.GetEnd(),
				}
			})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: %w", c.prunedBelowRetention(ctx, next, err))
		}
		for _, entry := range scanned {
			// Checkpoint sequence numbers are contiguous, so a gap inside the
			// range is the node misreporting, not missing history.
			if seqNum := entry.item.GetSequenceNumber(); seqNum != next {
				return nil, fmt.Errorf("GetCheckpoints: expected checkpoint %d, node sent %d", next, seqNum)
			}
			collected = append(collected, entry.item)
			next++
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

// GetCheckpointTransactions returns every transaction in the checkpoint, shaped
// per options and in checkpoint order. It is a ListTransactions scan over the
// single-checkpoint range, replacing the GetCheckpoint-then-BatchGetTransactions
// two-hop with one stream.
func (c *Client) GetCheckpointTransactions(
	ctx context.Context,
	seqNum uint64,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	query := types.TransactionRangeQuery{
		StartCheckpoint: proto.Uint64(seqNum),
		EndCheckpoint:   proto.Uint64(seqNum + 1),
	}
	var responses []*types.SuiTransactionBlockResponse
	for {
		page, err := c.ListTransactions(ctx, query, options)
		if err != nil {
			return nil, fmt.Errorf("GetCheckpointTransactions: %w", err)
		}
		for _, listed := range page.Data {
			responses = append(responses, listed.Transaction)
		}
		if !page.HasMore {
			break
		}
		// Resumable but with nowhere to resume from: returning what arrived
		// would be indistinguishable from a complete checkpoint.
		if len(page.NextCursor) == 0 || bytes.Equal(page.NextCursor, query.Cursor) {
			return nil, fmt.Errorf(
				"GetCheckpointTransactions: checkpoint %d stopped after %d transactions with no cursor to resume from",
				seqNum, len(responses))
		}
		query.Cursor = page.NextCursor
	}

	if len(responses) == 0 {
		// A checkpoint the node cannot serve scans just as clean as one holding
		// nothing, so say which it was.
		info, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpointTransactions: classifying empty checkpoint %d: %w", seqNum, err)
		}
		if lowest := info.GetLowestAvailableCheckpoint(); seqNum < lowest {
			return nil, fmt.Errorf("GetCheckpointTransactions: checkpoint %d pruned; node retains from %d", seqNum, lowest)
		}
		if height := info.GetCheckpointHeight(); seqNum > height {
			return nil, fmt.Errorf("GetCheckpointTransactions: checkpoint %d is beyond the chain tip %d", seqNum, height)
		}
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
