package clientv2

import (
	"context"
	"fmt"
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

func serviceInfoResponse(lowest, height uint64) *pb.GetServiceInfoResponse {
	return &pb.GetServiceInfoResponse{
		LowestAvailableCheckpoint: proto.Uint64(lowest),
		CheckpointHeight:          proto.Uint64(height),
	}
}

func checkpointFrames(seqNums []uint64, reason pb.QueryEndReason) []*pb.ListCheckpointsResponse {
	frames := make([]*pb.ListCheckpointsResponse, 0, len(seqNums))
	for _, seqNum := range seqNums {
		frames = append(frames, &pb.ListCheckpointsResponse{
			Checkpoint: &pb.Checkpoint{
				SequenceNumber: proto.Uint64(seqNum),
				Digest:         proto.String(testDigest(int(seqNum)).String()),
			},
			Watermark: &pb.Watermark{Cursor: fmt.Appendf(nil, "c%d", seqNum)},
		})
	}
	if len(frames) > 0 {
		frames[len(frames)-1].End = &pb.QueryEnd{Reason: reason.Enum()}
	}
	return frames
}

func seqRange(start uint64, count int) []uint64 {
	seqNums := make([]uint64, count)
	for i := range seqNums {
		seqNums[i] = start + uint64(i)
	}
	return seqNums
}

func listCheckpointsRequestMasked(start, end uint64, limit uint32, mask []string) *pb.ListCheckpointsRequest {
	return &pb.ListCheckpointsRequest{
		ReadMask:        &fieldmaskpb.FieldMask{Paths: mask},
		StartCheckpoint: proto.Uint64(start),
		EndCheckpoint:   proto.Uint64(end),
		Options:         &pb.QueryOptions{Limit: proto.Uint32(limit)},
	}
}

func listCheckpointsRequest(start, end uint64, limit uint32) *pb.ListCheckpointsRequest {
	return &pb.ListCheckpointsRequest{
		ReadMask:        &fieldmaskpb.FieldMask{Paths: checkpointReadMaskPaths},
		StartCheckpoint: proto.Uint64(start),
		EndCheckpoint:   proto.Uint64(end),
		Options:         &pb.QueryOptions{Limit: proto.Uint32(limit)},
	}
}

// A node may end a stream below the asked-for limit, so the range must be paged.
func TestGetCheckpointsPagesUntilLimit(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	gomock.InOrder(
		mocks.ledger.EXPECT().
			ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit, limit))).
			Return(&fakeStream[pb.ListCheckpointsResponse]{
				frames: checkpointFrames(seqRange(start, 2), pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT),
			}, nil),
		mocks.ledger.EXPECT().
			ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start+2, start+limit, limit-2))).
			Return(&fakeStream[pb.ListCheckpointsResponse]{
				frames: checkpointFrames(seqRange(start+2, 3), pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT),
			}, nil),
	)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit)
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64())
	}
}

func TestGetCheckpointsTipKeepsPrefix(t *testing.T) {
	const start, limit, available = uint64(100), 20, 7
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), gomock.Any()).
		Return(&fakeStream[pb.ListCheckpointsResponse]{
			frames: checkpointFrames(seqRange(start, available), pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP),
		}, nil)
	// Truncation triggers exactly one classification call; the missing
	// checkpoint is past the tip, not pruned.
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, start+available-1), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, available)
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64())
	}
}

// A pruned range is refused outright, so OutOfRange must become the documented
// error rather than leaking.
func TestGetCheckpointsPrunedReturnsError(t *testing.T) {
	const start, limit = uint64(100), 5
	const lowest, height = uint64(500), uint64(1000)
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), gomock.Any()).
		Return(&fakeStream[pb.ListCheckpointsResponse]{
			err: status.Error(codes.OutOfRange, "requested data below earliest available; lowest available checkpoint is 500"),
		}, nil)
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(lowest, height), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.Nil(t, checkpoints)
	require.ErrorContains(t, err, "checkpoint 100 pruned; node retains from 500")
}

func TestGetCheckpointsGapReturnsError(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), gomock.Any()).
		Return(&fakeStream[pb.ListCheckpointsResponse]{
			frames: checkpointFrames([]uint64{start, start + 1, start + 3},
				pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND),
		}, nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.Nil(t, checkpoints)
	require.ErrorContains(t, err, "expected checkpoint 102, node sent 103")
}

// No QueryEnd means the range was never declared exhausted, so the scan must ask
// again rather than truncate.
func TestGetCheckpointsAbsentQueryEndKeepsScanning(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	// checkpointFrames always closes with a reason; this round must carry none.
	unterminated := checkpointFrames(seqRange(start, 2), pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT)
	unterminated[len(unterminated)-1].End = nil
	gomock.InOrder(
		mocks.ledger.EXPECT().
			ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit, limit))).
			Return(&fakeStream[pb.ListCheckpointsResponse]{frames: unterminated}, nil),
		mocks.ledger.EXPECT().
			ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start+2, start+limit, limit-2))).
			Return(&fakeStream[pb.ListCheckpointsResponse]{
				frames: checkpointFrames(seqRange(start+2, 3), pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND),
			}, nil),
	)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit, "an unterminated stream must not truncate the range")
}

// Reporting more to come while returning nothing must not loop forever.
func TestGetCheckpointsStalledRoundStops(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit, limit))).
		Return(&fakeStream[pb.ListCheckpointsResponse]{
			frames: []*pb.ListCheckpointsResponse{{
				End: &pb.QueryEnd{Reason: pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT.Enum()},
			}},
		}, nil).
		Times(1)
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, start), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Empty(t, checkpoints)
}

func TestGetCheckpointsStreamError(t *testing.T) {
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), gomock.Any()).
		Return(&fakeStream[pb.ListCheckpointsResponse]{
			frames: checkpointFrames(seqRange(100, 1), pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT),
			err:    status.Error(codes.Unavailable, "node down"),
		}, nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), 100, 5)
	require.Nil(t, checkpoints)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.ErrorContains(t, err, "node down")
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

// WithMask narrows what the node fetches. sequence_number is forced in: the scan
// validates contiguity and resumes by it, so a mask omitting it would make every
// frame read as checkpoint 0.
func TestGetCheckpointsWithMask(t *testing.T) {
	const start, limit = uint64(100), 3
	tests := []struct {
		name     string
		mask     []string
		wantMask []string
	}{
		{
			name:     "unset falls back to the full mask",
			wantMask: checkpointReadMaskPaths,
		},
		{
			name:     "caller mask is sent as given",
			mask:     []string{"sequence_number", "digest", "transactions.digest"},
			wantMask: []string{"sequence_number", "digest", "transactions.digest"},
		},
		{
			name:     "sequence_number is prepended when omitted",
			mask:     []string{"transactions.digest"},
			wantMask: []string{"sequence_number", "transactions.digest"},
		},
		{
			name:     "already-present sequence_number is not duplicated",
			mask:     []string{"digest", "sequence_number"},
			wantMask: []string{"digest", "sequence_number"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, mocks := newMockClient(t)
			mocks.ledger.EXPECT().
				ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequestMasked(start, start+limit, limit, test.wantMask))).
				Return(&fakeStream[pb.ListCheckpointsResponse]{
					frames: checkpointFrames(seqRange(start, limit), pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND),
				}, nil)

			var opts []types.CheckpointOption
			if test.mask != nil {
				opts = append(opts, types.WithMask(test.mask...))
			}
			checkpoints, err := client.GetCheckpoints(context.Background(), start, limit, opts...)
			require.NoError(t, err)
			require.Len(t, checkpoints, limit)
		})
	}
}
