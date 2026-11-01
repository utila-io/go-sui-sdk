package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// prunedBelowRetention converts the node's OutOfRange refusal of a range under
// its earliest available data into an error naming the retained range. The
// refusal reports a transaction sequence, not a checkpoint, hence the extra hop.
// Other errors pass through.
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
// rather than a bound of the query, leaving items behind. Unrecognised reasons
// count as continuable so an upstream addition cannot silently truncate a scan.
func scanMayContinue(reason pb.QueryEndReason) bool {
	switch reason {
	case pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND,
		pb.QueryEndReason_QUERY_END_REASON_CURSOR_BOUND,
		pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP:
		return false
	default:
		return true
	}
}
