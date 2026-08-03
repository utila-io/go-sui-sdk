package clientv2

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// The List* RPCs stream their results: every frame carries a resume cursor and
// at most one item, and exactly one terminal frame says why the query stopped.
// A single call can stop short of the requested range -- the node caps both the
// item count and the scan budget -- so a full scan resumes from the last cursor.

// listFrame is one decoded response frame. item is nil on a frame that only
// reports scan progress; end is nil on every frame but the terminal one.
type listFrame[T any] struct {
	item   *T
	cursor []byte
	end    *pb.QueryEnd
}

// scannedItem pairs an item with the cursor that resumes just after it.
type scannedItem[T any] struct {
	item   *T
	cursor []byte
}

// scanRound drains one List* stream, returning its items in stream order, the
// last resume cursor seen, and whether the node left part of the range
// unscanned.
func scanRound[R, T any](
	stream grpc.ServerStreamingClient[R],
	decode func(*R) listFrame[T],
) (items []scannedItem[T], cursor []byte, resumable bool, err error) {
	// Without a terminal frame the stream never said the range was exhausted.
	resumable = true
	for {
		response, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return items, cursor, resumable, nil
		}
		if recvErr != nil {
			return nil, nil, false, recvErr
		}
		frame := decode(response)
		if len(frame.cursor) > 0 {
			cursor = frame.cursor
		}
		if frame.item != nil {
			items = append(items, scannedItem[T]{item: frame.item, cursor: frame.cursor})
		}
		if frame.end != nil {
			resumable = scanMayContinue(frame.end.GetReason())
		}
	}
}

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
// rather than on a bound of the query, leaving items behind.
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
