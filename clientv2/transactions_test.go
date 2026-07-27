package clientv2

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

var defaultResponseReadMask = &fieldmaskpb.FieldMask{Paths: []string{"digest", "checkpoint", "timestamp"}}

func transactionResults(digests []string) []*pb.GetTransactionResult {
	results := make([]*pb.GetTransactionResult, len(digests))
	for i, digest := range digests {
		results[i] = &pb.GetTransactionResult{
			Result: &pb.GetTransactionResult_Transaction{
				Transaction: &pb.ExecutedTransaction{Digest: proto.String(digest)},
			},
		}
	}
	return results
}

func TestMultiGetTransactionBlocksChunking(t *testing.T) {
	const total = 51
	client, mocks := newMockClient(t)

	digests := make([]sui_types.TransactionDigest, total)
	digestStrs := make([]string, total)
	for i := range digests {
		digests[i] = testDigest(i)
		digestStrs[i] = digests[i].String()
	}
	gomock.InOrder(
		mocks.ledger.EXPECT().
			BatchGetTransactions(gomock.Any(), protoEqual(&pb.BatchGetTransactionsRequest{
				Digests:  digestStrs[:50],
				ReadMask: defaultResponseReadMask,
			})).
			Return(&pb.BatchGetTransactionsResponse{Transactions: transactionResults(digestStrs[:50])}, nil),
		mocks.ledger.EXPECT().
			BatchGetTransactions(gomock.Any(), protoEqual(&pb.BatchGetTransactionsRequest{
				Digests:  digestStrs[50:],
				ReadMask: defaultResponseReadMask,
			})).
			Return(&pb.BatchGetTransactionsResponse{Transactions: transactionResults(digestStrs[50:])}, nil),
	)

	responses, err := client.MultiGetTransactionBlocks(context.Background(), digests,
		types.SuiTransactionBlockResponseOptions{})
	require.NoError(t, err)
	require.Len(t, responses, total)
	for i, response := range responses {
		require.Equal(t, digestStrs[i], response.Digest.String())
	}
}

func TestMultiGetTransactionBlocksResultError(t *testing.T) {
	client, mocks := newMockClient(t)

	digests := []sui_types.TransactionDigest{testDigest(0), testDigest(1)}
	results := transactionResults([]string{digests[0].String()})
	results = append(results, &pb.GetTransactionResult{
		Result: &pb.GetTransactionResult_Error{
			Error: &rpcstatus.Status{Message: "transaction not found"},
		},
	})
	mocks.ledger.EXPECT().
		BatchGetTransactions(gomock.Any(), protoEqual(&pb.BatchGetTransactionsRequest{
			Digests:  []string{digests[0].String(), digests[1].String()},
			ReadMask: defaultResponseReadMask,
		})).
		Return(&pb.BatchGetTransactionsResponse{Transactions: results}, nil)

	// v1 semantics: any per-item error fails the whole call
	_, err := client.MultiGetTransactionBlocks(context.Background(), digests,
		types.SuiTransactionBlockResponseOptions{})
	require.ErrorContains(t, err, digests[1].String())
	require.ErrorContains(t, err, "transaction not found")
}

func TestGetTransactionBlockNoTransaction(t *testing.T) {
	client, mocks := newMockClient(t)
	digest := testDigest(0)
	mocks.ledger.EXPECT().
		GetTransaction(gomock.Any(), gomock.Any()).
		Return(&pb.GetTransactionResponse{}, nil)
	_, err := client.GetTransactionBlock(context.Background(), digest,
		types.SuiTransactionBlockResponseOptions{})
	require.ErrorContains(t, err, digest.String())
	require.ErrorContains(t, err, "node returned no transaction")
}

func TestMultiGetTransactionBlocksEmptyResult(t *testing.T) {
	client, mocks := newMockClient(t)
	digests := []sui_types.TransactionDigest{testDigest(0)}
	// a result with neither transaction nor error must not yield a nil response
	mocks.ledger.EXPECT().
		BatchGetTransactions(gomock.Any(), gomock.Any()).
		Return(&pb.BatchGetTransactionsResponse{
			Transactions: []*pb.GetTransactionResult{{}},
		}, nil)
	_, err := client.MultiGetTransactionBlocks(context.Background(), digests,
		types.SuiTransactionBlockResponseOptions{})
	require.ErrorContains(t, err, digests[0].String())
	require.ErrorContains(t, err, "node returned no transaction")
}

func listTransactionsRequest(start, end uint64, cursor []byte) *pb.ListTransactionsRequest {
	req := &pb.ListTransactionsRequest{
		ReadMask:        defaultResponseReadMask,
		StartCheckpoint: proto.Uint64(start),
		EndCheckpoint:   proto.Uint64(end),
	}
	if cursor != nil {
		req.Options = &pb.QueryOptions{After: cursor}
	}
	return req
}

// transactionFrames returns one item frame per digest, each carrying the cursor
// that resumes after it, followed by a terminal frame carrying reason.
func transactionFrames(reason pb.QueryEndReason, cursor []byte, digests ...string) []*pb.ListTransactionsResponse {
	frames := make([]*pb.ListTransactionsResponse, 0, len(digests)+1)
	for _, digest := range digests {
		frames = append(frames, &pb.ListTransactionsResponse{
			Transaction: &pb.ExecutedTransaction{Digest: proto.String(digest)},
			Watermark:   &pb.Watermark{Cursor: cursor},
		})
	}
	return append(frames, &pb.ListTransactionsResponse{
		Watermark: &pb.Watermark{Cursor: cursor},
		End:       &pb.QueryEnd{Reason: reason.Enum()},
	})
}

func requireDigests(t *testing.T, responses []*types.SuiTransactionBlockResponse, digests []string) {
	t.Helper()
	require.Len(t, responses, len(digests))
	for i, response := range responses {
		require.Equal(t, digests[i], response.Digest.String())
	}
}

func TestListTransactions(t *testing.T) {
	const start, end = uint64(10), uint64(13)
	client, mocks := newMockClient(t)
	digests := []string{testDigest(0).String(), testDigest(1).String(), testDigest(2).String()}
	mocks.ledger.EXPECT().
		ListTransactions(gomock.Any(), protoEqual(listTransactionsRequest(start, end, nil))).
		Return(newFakeStream(transactionFrames(
			pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND, []byte("cursor"), digests...)...), nil)

	responses, err := client.ListTransactions(context.Background(), start, end,
		types.SuiTransactionBlockResponseOptions{})
	require.NoError(t, err)
	requireDigests(t, responses, digests)
}

// A range holding more transactions than the server's item limit takes several
// rounds, each resuming from the last watermark cursor.
func TestListTransactionsResumesAfterItemLimit(t *testing.T) {
	const start, end = uint64(10), uint64(13)
	client, mocks := newMockClient(t)
	digests := []string{testDigest(0).String(), testDigest(1).String(), testDigest(2).String()}
	gomock.InOrder(
		mocks.ledger.EXPECT().
			ListTransactions(gomock.Any(), protoEqual(listTransactionsRequest(start, end, nil))).
			Return(newFakeStream(transactionFrames(
				pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT, []byte("cursor-1"), digests[:2]...)...), nil),
		mocks.ledger.EXPECT().
			ListTransactions(gomock.Any(), protoEqual(listTransactionsRequest(start, end, []byte("cursor-1")))).
			Return(newFakeStream(transactionFrames(
				pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND, []byte("cursor-2"), digests[2:]...)...), nil),
	)

	responses, err := client.ListTransactions(context.Background(), start, end,
		types.SuiTransactionBlockResponseOptions{})
	require.NoError(t, err)
	requireDigests(t, responses, digests)
}

// A resumable stop with no way to resume must not pass for a complete range.
func TestListTransactionsUnresumableQueryErrors(t *testing.T) {
	const start, end = uint64(10), uint64(13)
	client, mocks := newMockClient(t)
	mocks.ledger.EXPECT().
		ListTransactions(gomock.Any(), protoEqual(listTransactionsRequest(start, end, nil))).
		Return(newFakeStream(transactionFrames(
			pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT, nil, testDigest(0).String())...), nil)

	responses, err := client.ListTransactions(context.Background(), start, end,
		types.SuiTransactionBlockResponseOptions{})
	require.Nil(t, responses)
	require.ErrorContains(t, err, "without advancing past checkpoint 10")
}

func TestListTransactionsEmptyRange(t *testing.T) {
	client, _ := newMockClient(t)
	for _, bounds := range [][2]uint64{{10, 10}, {10, 9}} {
		t.Run(fmt.Sprintf("[%d, %d)", bounds[0], bounds[1]), func(t *testing.T) {
			responses, err := client.ListTransactions(context.Background(), bounds[0], bounds[1],
				types.SuiTransactionBlockResponseOptions{})
			require.NoError(t, err)
			require.Nil(t, responses)
		})
	}
}

func TestGetCheckpointTransactions(t *testing.T) {
	const seqNum = uint64(42)
	client, mocks := newMockClient(t)
	digests := []string{testDigest(0).String(), testDigest(1).String()}
	mocks.ledger.EXPECT().
		ListTransactions(gomock.Any(), protoEqual(&pb.ListTransactionsRequest{
			// read_mask derived from options: events on top of the always-on paths
			ReadMask:        &fieldmaskpb.FieldMask{Paths: []string{"digest", "checkpoint", "timestamp", "events"}},
			StartCheckpoint: proto.Uint64(seqNum),
			EndCheckpoint:   proto.Uint64(seqNum + 1),
		})).
		Return(newFakeStream(transactionFrames(
			pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND, []byte("cursor"), digests...)...), nil)

	responses, err := client.GetCheckpointTransactions(context.Background(), seqNum,
		types.SuiTransactionBlockResponseOptions{ShowEvents: true})
	require.NoError(t, err)
	requireDigests(t, responses, digests)
}
