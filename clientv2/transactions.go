package clientv2

import (
	"bytes"
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

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
		ReadMask: &fieldmaskpb.FieldMask{Paths: pb.ResponseReadMaskPaths(options)},
	})
	if err != nil {
		return nil, fmt.Errorf("GetTransactionBlock: %w", err)
	}
	response := resp.GetTransaction().ToInternalType(options)
	if response == nil {
		return nil, fmt.Errorf("GetTransactionBlock %s: node returned no transaction", digest.String())
	}
	return response, nil
}

// MultiGetTransactionBlocks returns the executed transactions in digest
// order. Any unknown digest fails the whole call.
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

// ListTransactions returns every transaction in checkpoints [startCheckpoint,
// endCheckpoint), ascending by checkpoint and by position within it, shaped per
// options. An empty or inverted range yields nothing; a range reaching past the
// chain tip stops there.
func (c *Client) ListTransactions(
	ctx context.Context,
	startCheckpoint, endCheckpoint uint64,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	responses, err := c.listTransactions(ctx, startCheckpoint, endCheckpoint, options)
	if err != nil {
		return nil, fmt.Errorf("ListTransactions: %w", err)
	}
	return responses, nil
}

func (c *Client) listTransactions(
	ctx context.Context,
	startCheckpoint, endCheckpoint uint64,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	if endCheckpoint <= startCheckpoint {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	// Releases the stream when a scan stops before its final frame.
	defer cancel()

	readMask := &fieldmaskpb.FieldMask{Paths: pb.ResponseReadMaskPaths(options)}
	var (
		out    []*types.SuiTransactionBlockResponse
		cursor []byte
	)
	for {
		// Transactions are addressed by their position inside a checkpoint, so
		// unlike checkpoints a resumed scan must continue from the opaque
		// cursor rather than from the next checkpoint number.
		req := &pb.ListTransactionsRequest{
			ReadMask:        readMask,
			StartCheckpoint: proto.Uint64(startCheckpoint),
			EndCheckpoint:   proto.Uint64(endCheckpoint),
		}
		if cursor != nil {
			req.Options = &pb.QueryOptions{After: cursor}
		}
		stream, err := c.ledger.ListTransactions(ctx, req)
		if err != nil {
			return nil, err
		}
		items, next, reason, err := scanRound(stream, func(resp *pb.ListTransactionsResponse) listFrame[pb.ExecutedTransaction] {
			return listFrame[pb.ExecutedTransaction]{
				item:   resp.GetTransaction(),
				cursor: resp.GetWatermark().GetCursor(),
				end:    resp.GetEnd(),
			}
		})
		if err != nil {
			return nil, err
		}
		for _, transaction := range items {
			response := transaction.ToInternalType(options)
			if response == nil {
				return nil, fmt.Errorf("node returned an empty transaction in checkpoint range [%d, %d)", startCheckpoint, endCheckpoint)
			}
			out = append(out, response)
		}

		if !resumable(reason) {
			return out, nil
		}
		// Erroring beats returning a short range: the caller cannot tell a
		// truncated scan from a checkpoint range that held nothing else.
		if next == nil || (len(items) == 0 && bytes.Equal(next, cursor)) {
			return nil, fmt.Errorf("query stopped at %s without advancing past checkpoint %d", reason, startCheckpoint)
		}
		cursor = next
	}
}

func (c *Client) batchGetTransactions(
	ctx context.Context,
	digests []string,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	readMask := &fieldmaskpb.FieldMask{Paths: pb.ResponseReadMaskPaths(options)}
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
			response := result.GetTransaction().ToInternalType(options)
			if response == nil {
				return nil, fmt.Errorf("transaction %s: node returned no transaction", digests[start+i])
			}
			responses = append(responses, response)
		}
	}
	return responses, nil
}
