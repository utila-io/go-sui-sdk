package clientv2

import (
	"context"
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
