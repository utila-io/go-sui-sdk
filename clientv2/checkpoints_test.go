package clientv2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

func checkpointRequest(seqNum uint64, readMaskPaths ...string) *pb.GetCheckpointRequest {
	return &pb.GetCheckpointRequest{
		CheckpointId: &pb.GetCheckpointRequest_SequenceNumber{SequenceNumber: seqNum},
		ReadMask:     &fieldmaskpb.FieldMask{Paths: readMaskPaths},
	}
}

var checkpointReadMaskPaths = []string{
	"sequence_number", "digest", "summary", "signature", "transactions.digest",
}

func checkpointResponse(seqNum uint64) *pb.GetCheckpointResponse {
	return &pb.GetCheckpointResponse{Checkpoint: &pb.Checkpoint{
		SequenceNumber: proto.Uint64(seqNum),
		Digest:         proto.String(testDigest(int(seqNum)).String()),
	}}
}

func TestGetCheckpointsAscendingOrder(t *testing.T) {
	const start, limit = uint64(100), 20
	client, mocks := newMockClient(t)
	for i := range limit {
		seqNum := start + uint64(i)
		mocks.ledger.EXPECT().
			GetCheckpoint(gomock.Any(), protoEqual(checkpointRequest(seqNum, checkpointReadMaskPaths...))).
			Return(checkpointResponse(seqNum), nil)
	}

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit)
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64())
	}
}

func TestGetCheckpointsNotFoundKeepsPrefix(t *testing.T) {
	const start, limit, missing = uint64(100), 20, 7
	client, mocks := newMockClient(t)
	for i := range limit {
		seqNum := start + uint64(i)
		call := mocks.ledger.EXPECT().
			GetCheckpoint(gomock.Any(), protoEqual(checkpointRequest(seqNum, checkpointReadMaskPaths...)))
		if i < missing {
			call.Return(checkpointResponse(seqNum), nil)
		} else {
			// Offsets past the first hole race with the skip logic: they may
			// or may not be fetched, and the node reports them all NotFound.
			call.Return(nil, status.Error(codes.NotFound, "checkpoint not found")).MaxTimes(1)
		}
	}

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, missing)
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64())
	}
}

func TestGetCheckpointsNonPositiveLimit(t *testing.T) {
	client, _ := newMockClient(t)
	for _, limit := range []int{0, -1} {
		checkpoints, err := client.GetCheckpoints(context.Background(), 100, limit)
		require.NoError(t, err)
		require.Nil(t, checkpoints)
	}
}

func TestGetCheckpointTransactions(t *testing.T) {
	const seqNum = uint64(42)
	client, mocks := newMockClient(t)

	digests := []string{testDigest(0).String(), testDigest(1).String(), testDigest(2).String()}
	results := make([]*pb.GetTransactionResult, len(digests))
	checkpointTxs := make([]*pb.ExecutedTransaction, len(digests))
	for i, digest := range digests {
		results[i] = &pb.GetTransactionResult{
			Result: &pb.GetTransactionResult_Transaction{
				Transaction: &pb.ExecutedTransaction{Digest: proto.String(digest)},
			},
		}
		checkpointTxs[i] = &pb.ExecutedTransaction{Digest: proto.String(digest)}
	}
	gomock.InOrder(
		mocks.ledger.EXPECT().
			GetCheckpoint(gomock.Any(), protoEqual(checkpointRequest(seqNum, "sequence_number", "transactions.digest"))).
			Return(&pb.GetCheckpointResponse{Checkpoint: &pb.Checkpoint{
				SequenceNumber: proto.Uint64(seqNum),
				Transactions:   checkpointTxs,
			}}, nil),
		mocks.ledger.EXPECT().
			BatchGetTransactions(gomock.Any(), protoEqual(&pb.BatchGetTransactionsRequest{
				Digests: digests,
				// read_mask derived from options: events on top of the always-on paths
				ReadMask: &fieldmaskpb.FieldMask{Paths: []string{"digest", "checkpoint", "timestamp", "events"}},
			})).
			Return(&pb.BatchGetTransactionsResponse{Transactions: results}, nil),
	)

	responses, err := client.GetCheckpointTransactions(context.Background(), seqNum,
		types.SuiTransactionBlockResponseOptions{ShowEvents: true})
	require.NoError(t, err)
	require.Len(t, responses, len(digests))
	for i, response := range responses {
		require.Equal(t, digests[i], response.Digest.String())
	}
}
