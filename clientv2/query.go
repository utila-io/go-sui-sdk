package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// How the List* range queries behave, beyond draining their stream: a single
// call can stop short of the requested range -- the node caps both the item
// count and the scan budget -- and a range outside the data it holds is
// answered two different ways depending on which side it falls on.

// prunedBelowRetention names the retained range when the node refused a scan
// starting under its earliest available data. A range above the tip streams back
// empty, but one below the retention watermark is rejected with OutOfRange, so
// this is the only way pruning surfaces on a List* call. The refusal reports a
// transaction sequence rather than a checkpoint, hence the GetServiceInfo hop.
// Any other error passes through untouched.
func (c *Client) prunedBelowRetention(ctx context.Context, seqNum uint64, err error) error {
	if status.Code(err) != codes.OutOfRange {
		return err
	}
	info, infoErr := c.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
	if infoErr != nil {
		return err
	}
	if lowest := info.GetLowestAvailableCheckpoint(); seqNum < lowest {
		return fmt.Errorf("checkpoint %d pruned; node retains from %d", seqNum, lowest)
	}
	return err
}

// scanMayContinue reports whether the node stopped on one of its own budgets
// rather than on a bound of the query, leaving items behind. An unrecognised or
// absent reason counts as continuable, so a reason added upstream later cannot
// silently truncate a scan.
func scanMayContinue(reason pb.QueryEndReason) bool {
	switch reason {
	case pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND,
		pb.QueryEndReason_QUERY_END_REASON_CURSOR_BOUND,
		pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP:
		return false
	default:
		// ITEM_LIMIT, SCAN_LIMIT, and reasons added after this SDK.
		return true
	}
}
