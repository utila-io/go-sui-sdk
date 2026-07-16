package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

const batchTransactionsChunkSize = 50

func (c *Client) GetTransactionBlock(
	ctx context.Context,
	digest sui_types.TransactionDigest,
	options types.SuiTransactionBlockResponseOptions,
) (*types.SuiTransactionBlockResponse, error) {
	resp, err := c.ledger.GetTransaction(ctx, &pb.GetTransactionRequest{
		Digest:   proto.String(digest.String()),
		ReadMask: &fieldmaskpb.FieldMask{Paths: adapt.ResponseReadMaskPaths(options)},
	})
	if err != nil {
		return nil, fmt.Errorf("GetTransactionBlock: %w", err)
	}
	response := adapt.Response(resp.GetTransaction(), options)
	if response == nil {
		return nil, fmt.Errorf("GetTransactionBlock %s: node returned no transaction", digest.String())
	}
	return response, nil
}

// MultiGetTransactionBlocks returns the executed transactions in digest
// order. Like JSON-RPC, any unknown digest fails the whole call.
func (c *Client) MultiGetTransactionBlocks(
	ctx context.Context,
	digests []sui_types.TransactionDigest,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	digestStrs := make([]string, len(digests))
	for i, digest := range digests {
		digestStrs[i] = digest.String()
	}
	responses, err := c.batchGetTransactions(ctx, digestStrs, options)
	if err != nil {
		return nil, fmt.Errorf("MultiGetTransactionBlocks: %w", err)
	}
	return responses, nil
}

func (c *Client) batchGetTransactions(
	ctx context.Context,
	digests []string,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	readMask := &fieldmaskpb.FieldMask{Paths: adapt.ResponseReadMaskPaths(options)}
	responses := make([]*types.SuiTransactionBlockResponse, 0, len(digests))
	for start := 0; start < len(digests); start += batchTransactionsChunkSize {
		end := min(start+batchTransactionsChunkSize, len(digests))
		resp, err := c.ledger.BatchGetTransactions(ctx, &pb.BatchGetTransactionsRequest{
			Digests:  digests[start:end],
			ReadMask: readMask,
		})
		if err != nil {
			return nil, err
		}
		results := resp.GetTransactions()
		if len(results) != end-start {
			return nil, fmt.Errorf("got %d results for %d digests", len(results), end-start)
		}
		for i, result := range results {
			if resultErr := result.GetError(); resultErr != nil {
				return nil, fmt.Errorf("transaction %s: %s", digests[start+i], resultErr.GetMessage())
			}
			response := adapt.Response(result.GetTransaction(), options)
			if response == nil {
				return nil, fmt.Errorf("transaction %s: node returned no transaction", digests[start+i])
			}
			responses = append(responses, response)
		}
	}
	return responses, nil
}
