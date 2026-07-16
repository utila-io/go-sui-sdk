package clientv2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

func TestGetAllBalances(t *testing.T) {
	owner := testAddress(t)
	client, mocks := newMockClient(t)

	balance := func(coinType string, total uint64) *pb.Balance {
		return &pb.Balance{CoinType: proto.String(coinType), Balance: proto.Uint64(total)}
	}
	gomock.InOrder(
		mocks.state.EXPECT().
			ListBalances(gomock.Any(), protoEqual(&pb.ListBalancesRequest{
				Owner:    proto.String(owner.String()),
				PageSize: proto.Uint32(1000),
			})).
			Return(&pb.ListBalancesResponse{
				Balances: []*pb.Balance{
					balance("0x2::sui::SUI", 10),
					balance("0xa::usdc::USDC", 20),
				},
				NextPageToken: []byte("page-2"),
			}, nil),
		mocks.state.EXPECT().
			ListBalances(gomock.Any(), protoEqual(&pb.ListBalancesRequest{
				Owner:     proto.String(owner.String()),
				PageSize:  proto.Uint32(1000),
				PageToken: []byte("page-2"),
			})).
			Return(&pb.ListBalancesResponse{
				Balances: []*pb.Balance{balance("0xb::weth::WETH", 30)},
			}, nil),
	)

	balances, err := client.GetAllBalances(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, balances, 3)
	wantTypes := []string{"0x2::sui::SUI", "0xa::usdc::USDC", "0xb::weth::WETH"}
	wantTotals := []int64{10, 20, 30}
	for i, got := range balances {
		require.Equal(t, wantTypes[i], got.CoinType)
		require.Equal(t, wantTotals[i], got.TotalBalance.IntPart())
	}
}
