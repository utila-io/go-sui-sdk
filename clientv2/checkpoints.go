package clientv2

import (
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
	resp, err := c.ledger.GetCheckpoint(ctx, &pb.GetCheckpointRequest{
		CheckpointId: &pb.GetCheckpointRequest_SequenceNumber{SequenceNumber: seqNum},
		ReadMask:     &fieldmaskpb.FieldMask{Paths: pb.CheckpointReadMaskPaths},
	})
	if err != nil {
		return nil, fmt.Errorf("GetCheckpoint: %w", err)
	}
	return resp.GetCheckpoint().ToInternalType(), nil
}

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
	// Releases the stream when a scan stops before its final frame.
	defer cancel()

	// endCheckpoint is exclusive, so the range bound alone caps the scan at
	// limit checkpoints; no item limit is needed.
	end := startSeqNum + uint64(limit)
	collected := make([]*pb.Checkpoint, 0, limit)
	for {
		stream, err := c.ledger.ListCheckpoints(ctx, &pb.ListCheckpointsRequest{
			ReadMask:        &fieldmaskpb.FieldMask{Paths: pb.CheckpointReadMaskPaths},
			StartCheckpoint: proto.Uint64(startSeqNum + uint64(len(collected))),
			EndCheckpoint:   proto.Uint64(end),
		})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: %w", err)
		}
		items, _, reason, err := scanRound(stream, func(resp *pb.ListCheckpointsResponse) listFrame[pb.Checkpoint] {
			return listFrame[pb.Checkpoint]{
				item:   resp.GetCheckpoint(),
				cursor: resp.GetWatermark().GetCursor(),
				end:    resp.GetEnd(),
			}
		})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: %w", err)
		}

		// A gap means the scan skipped checkpoints the node does not have; keep
		// the hole-free prefix and let the classification below decide whether
		// that is the chain tip or pruning.
		gap := false
		for _, checkpoint := range items {
			if checkpoint.GetSequenceNumber() != startSeqNum+uint64(len(collected)) {
				gap = true
				break
			}
			collected = append(collected, checkpoint)
		}
		// An empty round cannot make progress on a retry.
		if gap || len(items) == 0 || len(collected) >= limit || !resumable(reason) {
			break
		}
	}

	if len(collected) < limit {
		// Distinguish "past the chain tip" (truncate) from "pruned" (error):
		// a pruned node has no checkpoints below its retention watermark
		// either, and skips them just the same.
		missingSeq := startSeqNum + uint64(len(collected))
		info, err := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: classifying missing checkpoint %d: %w", missingSeq, err)
		}
		if lowest := info.GetLowestAvailableCheckpoint(); missingSeq < lowest {
			return nil, fmt.Errorf("GetCheckpoints: checkpoint %d pruned; node retains from %d", missingSeq, lowest)
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
	responses, err := c.listTransactions(ctx, seqNum, seqNum+1, options)
	if err != nil {
		return nil, fmt.Errorf("GetCheckpointTransactions: %w", err)
	}
	return responses, nil
}
