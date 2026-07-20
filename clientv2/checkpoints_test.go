package clientv2

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
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

func serviceInfoResponse(lowest, height uint64) *pb.GetServiceInfoResponse {
	return &pb.GetServiceInfoResponse{
		LowestAvailableCheckpoint: proto.Uint64(lowest),
		CheckpointHeight:          proto.Uint64(height),
	}
}

func TestGetCheckpointsNotFoundAtTipKeepsPrefix(t *testing.T) {
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
	// Truncation triggers exactly one classification call; the missing
	// checkpoint is past the tip, not pruned.
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, start+missing-1), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, missing)
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64())
	}
}

func TestGetCheckpointsNotFoundPrunedReturnsError(t *testing.T) {
	const start, limit = uint64(100), 5
	const lowest, height = uint64(500), uint64(1000)
	client, mocks := newMockClient(t)
	for i := range limit {
		seqNum := start + uint64(i)
		call := mocks.ledger.EXPECT().
			GetCheckpoint(gomock.Any(), protoEqual(checkpointRequest(seqNum, checkpointReadMaskPaths...))).
			Return(nil, status.Error(codes.NotFound, "checkpoint not found"))
		if i > 0 {
			call.MaxTimes(1)
		}
	}
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(lowest, height), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.Nil(t, checkpoints)
	require.ErrorContains(t, err, "checkpoint 100 pruned; node retains from 500")
}

// Regression test for unbounded goroutine spawn: a large limit must not create
// one goroutine per checkpoint.
func TestGetCheckpointsBoundedGoroutines(t *testing.T) {
	const start, limit = uint64(0), 10_000
	client, mocks := newMockClient(t)
	var maxGoroutines atomic.Int64
	mocks.ledger.EXPECT().
		GetCheckpoint(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, *pb.GetCheckpointRequest, ...grpc.CallOption) (*pb.GetCheckpointResponse, error) {
			for n := int64(runtime.NumGoroutine()); ; {
				if current := maxGoroutines.Load(); n <= current || maxGoroutines.CompareAndSwap(current, n) {
					break
				}
			}
			return nil, status.Error(codes.NotFound, "checkpoint not found")
		}).MinTimes(1)
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, 0), nil)
	baseline := int64(runtime.NumGoroutine())

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Empty(t, checkpoints)
	require.Less(t, maxGoroutines.Load()-baseline, int64(100),
		"goroutine count must stay bounded regardless of limit")
}

func TestGetCheckpointsHardErrorAbortsRemainingFetches(t *testing.T) {
	const start, limit = uint64(0), 1_000
	client, mocks := newMockClient(t)
	var calls atomic.Int64
	mocks.ledger.EXPECT().
		GetCheckpoint(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, req *pb.GetCheckpointRequest, _ ...grpc.CallOption) (*pb.GetCheckpointResponse, error) {
			calls.Add(1)
			if req.GetSequenceNumber() == start {
				return nil, status.Error(codes.Unavailable, "node down")
			}
			// Later fetches behave like real RPCs: they only fail once the
			// hard error cancels the shared context.
			<-ctx.Done()
			return nil, status.FromContextError(ctx.Err()).Err()
		}).AnyTimes()

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.Nil(t, checkpoints)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.ErrorContains(t, err, "node down")
	require.Less(t, calls.Load(), int64(limit),
		"a hard error must cancel remaining fetches instead of running all of them")
	require.LessOrEqual(t, calls.Load(), int64(3*checkpointFetchConcurrency))
}

func TestGetCheckpointsNonPositiveLimit(t *testing.T) {
	client, _ := newMockClient(t)
	for _, limit := range []int{0, -1} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			checkpoints, err := client.GetCheckpoints(context.Background(), 100, limit)
			require.NoError(t, err)
			require.Nil(t, checkpoints)
		})
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
