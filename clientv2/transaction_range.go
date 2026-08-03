package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// ListTransactions returns one page of the transactions in a checkpoint range,
// in ledger order. An empty page with HasMore set is normal: the node can spend
// its whole scan budget on checkpoints holding no match.
func (c *Client) ListTransactions(
	ctx context.Context,
	query types.TransactionRangeQuery,
	options types.SuiTransactionBlockResponseOptions,
) (*types.TransactionRangePage, error) {
	queryOptions := &pb.QueryOptions{}
	if query.Limit > 0 {
		queryOptions.Limit = proto.Uint32(query.Limit)
	}
	// The cursor is the bound the scan moves away from.
	if query.Descending {
		queryOptions.Ordering = pb.Ordering_ORDERING_DESCENDING.Enum()
		queryOptions.Before = query.Cursor
	} else {
		queryOptions.After = query.Cursor
	}

	// The scan's effective start, for reporting a range the node has pruned.
	var start uint64
	if query.StartCheckpoint != nil {
		start = *query.StartCheckpoint
	}

	stream, err := c.ledger.ListTransactions(ctx, &pb.ListTransactionsRequest{
		// transaction_index is outside the response options' mask.
		ReadMask:        &fieldmaskpb.FieldMask{Paths: append(pb.ResponseReadMaskPaths(options), "transaction_index")},
		StartCheckpoint: query.StartCheckpoint,
		EndCheckpoint:   query.EndCheckpoint,
		Options:         queryOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("ListTransactions: %w", c.prunedBelowRetention(ctx, start, err))
	}

	var covered *uint64
	scanned, cursor, resumable, err := scanRound(stream,
		func(frame *pb.ListTransactionsResponse) listFrame[pb.ExecutedTransaction] {
			// Never regresses, so the last frame carrying one wins.
			if watermark := frame.GetWatermark(); watermark != nil && watermark.Checkpoint != nil {
				covered = proto.Uint64(watermark.GetCheckpoint())
			}
			return listFrame[pb.ExecutedTransaction]{
				item:   frame.GetTransaction(),
				cursor: frame.GetWatermark().GetCursor(),
				end:    frame.GetEnd(),
			}
		})
	if err != nil {
		return nil, fmt.Errorf("ListTransactions: %w", c.prunedBelowRetention(ctx, start, err))
	}

	page := &types.TransactionRangePage{
		Data:       make([]types.ListedTransaction, 0, len(scanned)),
		NextCursor: cursor,
		HasMore:    resumable,
		Checkpoint: covered,
	}
	for _, entry := range scanned {
		response := entry.item.ToInternalType(options)
		if response == nil {
			return nil, fmt.Errorf("ListTransactions: node returned an empty transaction")
		}
		page.Data = append(page.Data, types.ListedTransaction{
			Transaction:      response,
			TransactionIndex: entry.item.GetTransactionIndex(),
			Cursor:           entry.cursor,
		})
	}
	return page, nil
}
