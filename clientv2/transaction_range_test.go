package clientv2

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// rangeReadMask is the response read mask plus the scan's position field.
var rangeReadMask = &fieldmaskpb.FieldMask{
	Paths: []string{"digest", "checkpoint", "timestamp", "transaction_index"},
}

func txFrame(index int, cursor string) *pb.ListTransactionsResponse {
	return &pb.ListTransactionsResponse{
		Transaction: &pb.ExecutedTransaction{
			Digest:           proto.String(testDigest(index).String()),
			TransactionIndex: proto.Uint64(uint64(index)),
		},
		Watermark: &pb.Watermark{Cursor: []byte(cursor)},
	}
}

func watermarkFrame(cursor string) *pb.ListTransactionsResponse {
	return &pb.ListTransactionsResponse{Watermark: &pb.Watermark{Cursor: []byte(cursor)}}
}

func withCovered(frame *pb.ListTransactionsResponse, checkpoint uint64) *pb.ListTransactionsResponse {
	frame.Watermark.Checkpoint = proto.Uint64(checkpoint)
	return frame
}

func withEnd(frame *pb.ListTransactionsResponse, reason pb.QueryEndReason) *pb.ListTransactionsResponse {
	frame.End = &pb.QueryEnd{Reason: reason.Enum()}
	return frame
}

// TestListTransactionsRequest pins how a query maps onto the wire request.
func TestListTransactionsRequest(t *testing.T) {
	tests := []struct {
		name  string
		query types.TransactionRangeQuery
		want  *pb.ListTransactionsRequest
	}{
		{
			name:  "unbounded",
			query: types.TransactionRangeQuery{},
			want: &pb.ListTransactionsRequest{
				ReadMask: rangeReadMask,
				Options:  &pb.QueryOptions{},
			},
		},
		{
			name: "ascending range with limit",
			query: types.TransactionRangeQuery{
				StartCheckpoint: proto.Uint64(100),
				EndCheckpoint:   proto.Uint64(110),
				Limit:           25,
			},
			want: &pb.ListTransactionsRequest{
				ReadMask:        rangeReadMask,
				StartCheckpoint: proto.Uint64(100),
				EndCheckpoint:   proto.Uint64(110),
				Options:         &pb.QueryOptions{Limit: proto.Uint32(25)},
			},
		},
		{
			name:  "ascending cursor is a lower bound",
			query: types.TransactionRangeQuery{Cursor: []byte("cursor-1")},
			want: &pb.ListTransactionsRequest{
				ReadMask: rangeReadMask,
				Options:  &pb.QueryOptions{After: []byte("cursor-1")},
			},
		},
		{
			name:  "descending cursor is an upper bound",
			query: types.TransactionRangeQuery{Cursor: []byte("cursor-1"), Descending: true},
			want: &pb.ListTransactionsRequest{
				ReadMask: rangeReadMask,
				Options: &pb.QueryOptions{
					Before:   []byte("cursor-1"),
					Ordering: pb.Ordering_ORDERING_DESCENDING.Enum(),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, mocks := newMockClient(t)
			mocks.ledger.EXPECT().
				ListTransactions(gomock.Any(), protoEqual(test.want)).
				Return(&fakeStream[pb.ListTransactionsResponse]{
					frames: []*pb.ListTransactionsResponse{
						withEnd(watermarkFrame("c"), pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND),
					},
				}, nil)

			_, err := client.ListTransactions(context.Background(), test.query,
				types.SuiTransactionBlockResponseOptions{})
			require.NoError(t, err)
		})
	}
}

// TestListTransactionsPage pins how a drained stream maps onto a page.
func TestListTransactionsPage(t *testing.T) {
	tests := []struct {
		name           string
		frames         []*pb.ListTransactionsResponse
		wantIndices    []uint64
		wantCursors    []string
		wantNextCursor string
		wantHasMore    bool
		wantCovered    *uint64
	}{
		{
			name: "item limit leaves more behind",
			frames: []*pb.ListTransactionsResponse{
				txFrame(0, "c0"),
				withEnd(txFrame(1, "c1"), pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT),
			},
			wantIndices:    []uint64{0, 1},
			wantCursors:    []string{"c0", "c1"},
			wantNextCursor: "c1",
			wantHasMore:    true,
		},
		{
			name: "checkpoint bound exhausts the range",
			frames: []*pb.ListTransactionsResponse{
				withCovered(txFrame(0, "c0"), 100),
				withEnd(withCovered(txFrame(1, "c1"), 101), pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND),
			},
			wantIndices:    []uint64{0, 1},
			wantCursors:    []string{"c0", "c1"},
			wantNextCursor: "c1",
			wantCovered:    proto.Uint64(101),
		},
		{
			name: "ledger tip exhausts the range",
			frames: []*pb.ListTransactionsResponse{
				withEnd(txFrame(0, "c0"), pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP),
			},
			wantIndices:    []uint64{0},
			wantCursors:    []string{"c0"},
			wantNextCursor: "c0",
		},
		{
			name: "scan limit on an empty slice still advances the cursor",
			frames: []*pb.ListTransactionsResponse{
				withEnd(watermarkFrame("c9"), pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT),
			},
			wantNextCursor: "c9",
			wantHasMore:    true,
		},
		{
			name: "watermark-only frames advance past unmatched checkpoints",
			frames: []*pb.ListTransactionsResponse{
				watermarkFrame("c0"),
				txFrame(1, "c1"),
				withEnd(withCovered(watermarkFrame("c2"), 102), pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT),
			},
			wantIndices:    []uint64{1},
			wantCursors:    []string{"c1"},
			wantNextCursor: "c2",
			wantHasMore:    true,
			wantCovered:    proto.Uint64(102),
		},
		{
			// Stopping without saying why is not proof of exhaustion.
			name: "missing QueryEnd reports more",
			frames: []*pb.ListTransactionsResponse{
				txFrame(0, "c0"),
			},
			wantIndices:    []uint64{0},
			wantCursors:    []string{"c0"},
			wantNextCursor: "c0",
			wantHasMore:    true,
		},
		{
			name:        "empty stream",
			wantHasMore: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, mocks := newMockClient(t)
			mocks.ledger.EXPECT().
				ListTransactions(gomock.Any(), gomock.Any()).
				Return(&fakeStream[pb.ListTransactionsResponse]{frames: test.frames}, nil)

			page, err := client.ListTransactions(context.Background(), types.TransactionRangeQuery{},
				types.SuiTransactionBlockResponseOptions{})
			require.NoError(t, err)

			require.Len(t, page.Data, len(test.wantIndices))
			for i, listed := range page.Data {
				require.Equal(t, testDigest(int(test.wantIndices[i])).String(), listed.Transaction.Digest.String())
				require.Equal(t, test.wantIndices[i], listed.TransactionIndex)
				require.Equal(t, test.wantCursors[i], string(listed.Cursor))
			}
			require.Equal(t, test.wantNextCursor, string(page.NextCursor))
			require.Equal(t, test.wantHasMore, page.HasMore)
			require.Equal(t, test.wantCovered, page.Checkpoint)
		})
	}
}

func TestListTransactionsErrors(t *testing.T) {
	t.Run("stream open fails", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			ListTransactions(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("unavailable"))

		_, err := client.ListTransactions(context.Background(), types.TransactionRangeQuery{},
			types.SuiTransactionBlockResponseOptions{})
		require.ErrorContains(t, err, "ListTransactions")
		require.ErrorContains(t, err, "unavailable")
	})

	t.Run("recv fails mid-stream", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			ListTransactions(gomock.Any(), gomock.Any()).
			Return(&fakeStream[pb.ListTransactionsResponse]{
				frames: []*pb.ListTransactionsResponse{txFrame(0, "c0")},
				err:    errors.New("connection reset"),
			}, nil)

		// A partial page's cursor would skip the unread remainder.
		page, err := client.ListTransactions(context.Background(), types.TransactionRangeQuery{},
			types.SuiTransactionBlockResponseOptions{})
		require.Nil(t, page)
		require.ErrorContains(t, err, "connection reset")
	})
}
