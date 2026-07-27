package clientv2

import (
	"errors"
	"io"

	"google.golang.org/grpc"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// The List* RPCs stream their results: every frame carries a resume cursor and
// at most one item, and exactly one terminal frame carries the reason the query
// stopped. A single call can stop short of the requested range — the server
// caps both the item count and the scan budget — so a full scan resumes from
// the last cursor until it reaches the range bound or the ledger tip.

// listFrame is one decoded response frame. item is nil on a frame that only
// reports scan progress, end is nil on every frame but the terminal one.
type listFrame[T any] struct {
	item   *T
	cursor []byte
	end    *pb.QueryEnd
}

// scanRound drains one List* stream, returning its items in stream order, the
// last resume cursor seen, and why the query stopped (UNKNOWN if the server
// ended the stream without saying).
func scanRound[R, T any](
	stream grpc.ServerStreamingClient[R],
	decode func(*R) listFrame[T],
) (items []*T, cursor []byte, reason pb.QueryEndReason, err error) {
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return items, cursor, reason, nil
		}
		if err != nil {
			return nil, nil, reason, err
		}
		frame := decode(resp)
		if frame.item != nil {
			items = append(items, frame.item)
		}
		if frame.cursor != nil {
			cursor = frame.cursor
		}
		if frame.end != nil {
			reason = frame.end.GetReason()
		}
	}
}

// resumable reports whether a query that stopped for this reason left part of
// the requested range unscanned; the other reasons mean the scan is complete
// (range bound reached) or cannot continue (ledger tip).
func resumable(reason pb.QueryEndReason) bool {
	switch reason {
	case pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT,
		pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT:
		return true
	default:
		return false
	}
}
