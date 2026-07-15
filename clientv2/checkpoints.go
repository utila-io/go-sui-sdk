package clientv2

import (
	"context"
	"fmt"
	"strconv"

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

// GetCheckpoints returns up to limit sequential checkpoints starting at
// startSeqNum (inclusive), in ascending order. Like the JSON-RPC page, it ends
// early at the tip of the chain: checkpoints past the last known one are
// simply not included.
func (c *Client) GetCheckpoints(ctx context.Context, startSeqNum uint64, limit int) ([]*types.Checkpoint, error) {
	checkpoints := make([]*types.Checkpoint, 0, limit)
	for seqNum := startSeqNum; seqNum < startSeqNum+uint64(limit); seqNum++ {
		checkpoint, err := c.getCheckpoint(ctx, seqNum, adapt.CheckpointReadMaskPaths)
		if status.Code(err) == codes.NotFound {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("GetCheckpoints: %w", err)
		}
		checkpoints = append(checkpoints, adapt.Checkpoint(checkpoint))
	}
	return checkpoints, nil
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
