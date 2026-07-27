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

var checkpointReadMaskPaths = []string{
	"sequence_number", "digest", "summary", "signature", "transactions.digest",
}

func listCheckpointsRequest(start, end uint64) *pb.ListCheckpointsRequest {
	return &pb.ListCheckpointsRequest{
		ReadMask:        &fieldmaskpb.FieldMask{Paths: checkpointReadMaskPaths},
		StartCheckpoint: proto.Uint64(start),
		EndCheckpoint:   proto.Uint64(end),
	}
}

// checkpointFrames returns one item frame per sequence number followed by a
// terminal frame carrying reason, mirroring how the node ends a query.
func checkpointFrames(reason pb.QueryEndReason, seqNums ...uint64) []*pb.ListCheckpointsResponse {
	frames := make([]*pb.ListCheckpointsResponse, 0, len(seqNums)+1)
	for _, seqNum := range seqNums {
		frames = append(frames, &pb.ListCheckpointsResponse{
			Checkpoint: &pb.Checkpoint{
				SequenceNumber: proto.Uint64(seqNum),
				Digest:         proto.String(testDigest(int(seqNum)).String()),
			},
			Watermark: &pb.Watermark{Cursor: []byte(fmt.Sprint(seqNum))},
		})
	}
	return append(frames, &pb.ListCheckpointsResponse{
		Watermark: &pb.Watermark{Cursor: []byte(fmt.Sprint(len(seqNums)))},
		End:       &pb.QueryEnd{Reason: reason.Enum()},
	})
}

func seqRange(start uint64, count int) []uint64 {
	seqNums := make([]uint64, count)
	for i := range seqNums {
		seqNums[i] = start + uint64(i)
	}
	return seqNums
}

func serviceInfoResponse(lowest, height uint64) *pb.GetServiceInfoResponse {
	return &pb.GetServiceInfoResponse{
		LowestAvailableCheckpoint: proto.Uint64(lowest),
		CheckpointHeight:          proto.Uint64(height),
	}
}

func requireCheckpointSeqNums(t *testing.T, checkpoints []*types.Checkpoint, start uint64, count int) {
	t.Helper()
	require.Len(t, checkpoints, count)
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64())
	}
}

func TestGetCheckpointsAscendingOrder(t *testing.T) {
	const start, limit = uint64(100), 20
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit))).
		Return(newFakeStream(checkpointFrames(
			pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND, seqRange(start, limit)...)...), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	requireCheckpointSeqNums(t, checkpoints, start, limit)
}

// The server coerces an oversized item limit down to its own maximum, so a
// range wider than that maximum takes several rounds.
func TestGetCheckpointsResumesAfterItemLimit(t *testing.T) {
	const start, limit, firstRound = uint64(100), 20, 8
	client, mocks := newMockClient(t)
	gomock.InOrder(
		mocks.ledger.EXPECT().
			ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit))).
			Return(newFakeStream(checkpointFrames(
				pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT, seqRange(start, firstRound)...)...), nil),
		// The scan resumes at the next checkpoint, keeping the same range bound.
		mocks.ledger.EXPECT().
			ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start+firstRound, start+limit))).
			Return(newFakeStream(checkpointFrames(
				pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND,
				seqRange(start+firstRound, limit-firstRound)...)...), nil),
	)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	requireCheckpointSeqNums(t, checkpoints, start, limit)
}

func TestGetCheckpointsTruncatesAtLedgerTip(t *testing.T) {
	const start, limit, available = uint64(100), 20, 7
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit))).
		Return(newFakeStream(checkpointFrames(
			pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP, seqRange(start, available)...)...), nil)
	// Truncation triggers exactly one classification call; the missing
	// checkpoint is past the tip, not pruned.
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, start+available-1), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	requireCheckpointSeqNums(t, checkpoints, start, available)
}

func TestGetCheckpointsPrunedReturnsError(t *testing.T) {
	const start, limit = uint64(100), 5
	const lowest, height = uint64(500), uint64(1000)
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit))).
		Return(newFakeStream(checkpointFrames(pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP)...), nil)
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(lowest, height), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.Nil(t, checkpoints)
	require.ErrorContains(t, err, "checkpoint 100 pruned; node retains from 500")
}

// A gap in the returned sequence numbers truncates the result: the caller is
// promised a hole-free ascending prefix.
func TestGetCheckpointsStopsAtGap(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit))).
		Return(newFakeStream(checkpointFrames(
			pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND, 100, 101, 103, 104)...), nil)
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, start+limit), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	requireCheckpointSeqNums(t, checkpoints, start, 2)
}

// Regression test for an endless resume loop: a resumable end reason on a round
// that delivered nothing must not be retried.
func TestGetCheckpointsEmptyRoundStopsScan(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), protoEqual(listCheckpointsRequest(start, start+limit))).
		Return(newFakeStream(checkpointFrames(pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT)...), nil).
		Times(1)
	mocks.ledger.EXPECT().
		GetServiceInfo(gomock.Any(), gomock.Any()).
		Return(serviceInfoResponse(0, start), nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
	require.NoError(t, err)
	require.Empty(t, checkpoints)
}

func TestGetCheckpointsStreamError(t *testing.T) {
	const start, limit = uint64(100), 5
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListCheckpoints(gomock.Any(), gomock.Any()).
		Return(&fakeStream[pb.ListCheckpointsResponse]{
			frames: checkpointFrames(pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND, start)[:1],
			err:    status.Error(codes.Unavailable, "node down"),
		}, nil)

	checkpoints, err := client.GetCheckpoints(context.Background(), start, limit)
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

func TestGetCheckpoint(t *testing.T) {
	const seqNum = uint64(42)
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		GetCheckpoint(gomock.Any(), protoEqual(&pb.GetCheckpointRequest{
			CheckpointId: &pb.GetCheckpointRequest_SequenceNumber{SequenceNumber: seqNum},
			ReadMask:     &fieldmaskpb.FieldMask{Paths: checkpointReadMaskPaths},
		})).
		Return(&pb.GetCheckpointResponse{Checkpoint: &pb.Checkpoint{
			SequenceNumber: proto.Uint64(seqNum),
			Digest:         proto.String(testDigest(int(seqNum)).String()),
		}}, nil)

	checkpoint, err := client.GetCheckpoint(context.Background(), seqNum)
	require.NoError(t, err)
	require.Equal(t, seqNum, checkpoint.SequenceNumber.Uint64())
}
