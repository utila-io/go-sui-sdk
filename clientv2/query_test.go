package clientv2

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// The node rejects a scan below its retention watermark instead of streaming
// nothing back, and reports a transaction sequence rather than a checkpoint, so
// the retained checkpoint has to come from GetServiceInfo.
func TestPrunedBelowRetention(t *testing.T) {
	outOfRange := status.Error(codes.OutOfRange, "requested data below earliest available; lowest available tx_seq is 3753123297")

	t.Run("names the retained range", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			GetServiceInfo(gomock.Any(), gomock.Any()).
			Return(serviceInfoResponse(500, 1000), nil)

		err := client.prunedBelowRetention(context.Background(), 100, outOfRange)
		require.ErrorContains(t, err, "checkpoint 100 pruned; node retains from 500")
	})

	t.Run("in-range OutOfRange is left alone", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			GetServiceInfo(gomock.Any(), gomock.Any()).
			Return(serviceInfoResponse(0, 1000), nil)

		err := client.prunedBelowRetention(context.Background(), 100, outOfRange)
		require.Equal(t, outOfRange, err)
	})

	t.Run("other codes pass through without an extra call", func(t *testing.T) {
		client, _ := newMockClient(t)
		unavailable := status.Error(codes.Unavailable, "node down")
		require.Equal(t, unavailable, client.prunedBelowRetention(context.Background(), 100, unavailable))
	})

	t.Run("an unclassifiable refusal keeps the original error", func(t *testing.T) {
		client, mocks := newMockClient(t)
		mocks.ledger.EXPECT().
			GetServiceInfo(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("info unavailable"))

		require.Equal(t, outOfRange, client.prunedBelowRetention(context.Background(), 100, outOfRange))
	})
}

func TestScanMayContinue(t *testing.T) {
	tests := []struct {
		reason pb.QueryEndReason
		want   bool
	}{
		{reason: pb.QueryEndReason_QUERY_END_REASON_ITEM_LIMIT, want: true},
		{reason: pb.QueryEndReason_QUERY_END_REASON_SCAN_LIMIT, want: true},
		{reason: pb.QueryEndReason_QUERY_END_REASON_UNKNOWN, want: true},
		{reason: pb.QueryEndReason(9999), want: true},
		{reason: pb.QueryEndReason_QUERY_END_REASON_CHECKPOINT_BOUND},
		{reason: pb.QueryEndReason_QUERY_END_REASON_CURSOR_BOUND},
		{reason: pb.QueryEndReason_QUERY_END_REASON_LEDGER_TIP},
	}
	for _, test := range tests {
		t.Run(test.reason.String(), func(t *testing.T) {
			require.Equal(t, test.want, scanMayContinue(test.reason))
		})
	}
}
