package clientv2

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// decodeTransactionFrame is the decode function a List* caller supplies: it
// pulls the item, the frame's resume cursor and the terminal marker out of one
// response, and scanRound handles the streaming around it. Both range scans in
// this package are built this way.
func decodeTransactionFrame(frame *pb.ListTransactionsResponse) listFrame[pb.ExecutedTransaction] {
	return listFrame[pb.ExecutedTransaction]{
		item:   frame.GetTransaction(),
		cursor: frame.GetWatermark().GetCursor(),
		end:    frame.GetEnd(),
	}
}

func TestScanRound(t *testing.T) {
	tests := []struct {
		name    string
		frames  []*pb.ListTransactionsResponse
		recvErr error
		// wantItems pairs each returned item's digest index with the cursor of
		// the frame that carried it.
		wantItems     map[int]string
		wantCursor    string
		wantResumable bool
		wantErr       string
	}{
		{
			name: "items keep the cursor of their own frame",
			frames: []*pb.ListTransactionsResponse{
				txFrame(0, "c0"),
				withEnd(txFrame(1, "c1"), pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT),
			},
			wantItems:     map[int]string{0: "c0", 1: "c1"},
			wantCursor:    "c1",
			wantResumable: true,
		},
		{
			// The node reports progress across checkpoints holding no match, so
			// the round cursor can outrun the last item.
			name: "watermark-only frames advance the cursor without adding items",
			frames: []*pb.ListTransactionsResponse{
				txFrame(0, "c0"),
				withEnd(watermarkFrame("c9"), pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT),
			},
			wantItems:     map[int]string{0: "c0"},
			wantCursor:    "c9",
			wantResumable: true,
		},
		{
			name: "a query bound ends the scan",
			frames: []*pb.ListTransactionsResponse{
				withEnd(txFrame(0, "c0"), pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND),
			},
			wantItems:  map[int]string{0: "c0"},
			wantCursor: "c0",
		},
		{
			// The contract that keeps a scan from truncating silently: a stream
			// that stops without saying why is not proof of exhaustion.
			name:          "absent QueryEnd stays resumable",
			frames:        []*pb.ListTransactionsResponse{txFrame(0, "c0")},
			wantItems:     map[int]string{0: "c0"},
			wantCursor:    "c0",
			wantResumable: true,
		},
		{
			// Same reasoning for a reason this SDK has never seen: assume more
			// rather than drop the tail of the range.
			name: "unrecognised reason stays resumable",
			frames: []*pb.ListTransactionsResponse{
				withEnd(txFrame(0, "c0"), pb.QueryEndReason(9999)),
			},
			wantItems:     map[int]string{0: "c0"},
			wantCursor:    "c0",
			wantResumable: true,
		},
		{
			name:          "empty stream",
			wantResumable: true,
		},
		{
			name:    "a mid-stream error discards the round",
			frames:  []*pb.ListTransactionsResponse{txFrame(0, "c0")},
			recvErr: errors.New("connection reset"),
			wantErr: "connection reset",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := &fakeStream[pb.ListTransactionsResponse]{frames: test.frames, err: test.recvErr}
			items, cursor, resumable, err := scanRound(stream, decodeTransactionFrame)

			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				// Nothing partial escapes: the caller cannot tell a truncated
				// round from a complete one.
				require.Empty(t, items)
				require.Empty(t, cursor)
				require.False(t, resumable)
				return
			}
			require.NoError(t, err)
			require.Len(t, items, len(test.wantItems))
			for i, entry := range items {
				wantCursor, ok := test.wantItems[i]
				require.True(t, ok, "unexpected item at %d", i)
				require.Equal(t, testDigest(i).String(), entry.item.GetDigest())
				require.Equal(t, wantCursor, string(entry.cursor))
			}
			require.Equal(t, test.wantCursor, string(cursor))
			require.Equal(t, test.wantResumable, resumable)
		})
	}
}

// The node rejects a scan below its retention watermark instead of streaming
// nothing back, and reports a transaction sequence rather than a checkpoint, so
// the retained checkpoint has to come from GetServiceInfo.
func TestPrunedBelowRetention(t *testing.T) {
	outOfRange := status.Error(codes.OutOfRange, "requested data below earliest available; lowest available tx_seq is 3753123297")

	t.Run("names the retained range", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			GetServiceInfo(gomock.Any(), gomock.Any()).
			Return(serviceInfoResponse(500, 1000), nil)

		err := client.prunedBelowRetention(context.Background(), 100, outOfRange)
		require.ErrorContains(t, err, "checkpoint 100 pruned; node retains from 500")
	})

	t.Run("in-range OutOfRange is left alone", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			GetServiceInfo(gomock.Any(), gomock.Any()).
			Return(serviceInfoResponse(0, 1000), nil)

		err := client.prunedBelowRetention(context.Background(), 100, outOfRange)
		require.Equal(t, outOfRange, err)
	})

	t.Run("other codes pass through without an extra call", func(t *testing.T) {
		client, _ := newMockClient(t)
		unavailable := status.Error(codes.Unavailable, "node down")
		require.Equal(t, unavailable, client.prunedBelowRetention(context.Background(), 100, unavailable))
	})

	t.Run("an unclassifiable refusal keeps the original error", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			GetServiceInfo(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("info unavailable"))

		require.Equal(t, outOfRange, client.prunedBelowRetention(context.Background(), 100, outOfRange))
	})
}

func TestScanMayContinue(t *testing.T) {
	tests := []struct {
		reason pb.QueryEndReason
		want   bool
	}{
		{reason: pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT, want: true},
		{reason: pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT, want: true},
		{reason: pb.QueryEndReason_QUERY_END_REASON_UNKNOWN, want: true},
		{reason: pb.QueryEndReason(9999), want: true},
		{reason: pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND},
		{reason: pb.QueryEndReason_QUERY_END_REASON_CURSOR_BOUND},
		{reason: pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP},
	}
	for _, test := range tests {
		t.Run(test.reason.String(), func(t *testing.T) {
			require.Equal(t, test.want, scanMayContinue(test.reason))
		})
	}
}

// The generic decode keeps the item's concrete type, so no cast is needed.
func TestScanRoundDecodesCheckpoints(t *testing.T) {
	stream := &fakeStream[pb.ListCheckpointsResponse]{
		frames: []*pb.ListCheckpointsResponse{{
			Checkpoint: &pb.Checkpoint{SequenceNumber: proto.Uint64(100)},
			Watermark:  &pb.Watermark{Cursor: []byte("c100")},
			End:        &pb.QueryEnd{Reason: pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND.Enum()},
		}},
	}
	items, cursor, resumable, err := scanRound(stream,
		func(frame *pb.ListCheckpointsResponse) listFrame[pb.Checkpoint] {
			return listFrame[pb.Checkpoint]{
				item:   frame.GetCheckpoint(),
				cursor: frame.GetWatermark().GetCursor(),
				end:    frame.GetEnd(),
			}
		})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, uint64(100), items[0].item.GetSequenceNumber())
	require.Equal(t, "c100", string(cursor))
	require.False(t, resumable)
}
