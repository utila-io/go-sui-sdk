package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// balancesPageSize is the maximum ListBalances page size accepted by nodes.
const balancesPageSize = 1000

// GetBalance returns the combined balance (coin objects + SIP-58 address
// balance) for the given coin type. An empty coinType defaults to
// 0x2::sui::SUI.
func (c *Client) GetBalance(ctx context.Context, owner sui_types.SuiAddress, coinType string) (*types.Balance, error) {
	if coinType == "" {
		coinType = types.SUI_COIN_TYPE
	}
	resp, err := c.state.GetBalance(ctx, &pb.GetBalanceRequest{
		Owner:    proto.String(owner.String()),
		CoinType: proto.String(coinType),
	})
	if err != nil {
		return nil, fmt.Errorf("GetBalance: %w", err)
	}
	balance := adapt.Balance(resp.GetBalance())
	return &balance, nil
}

// GetAllBalances returns the balances of every coin type owned by owner,
// walking all ListBalances pages.
func (c *Client) GetAllBalances(ctx context.Context, owner sui_types.SuiAddress) ([]types.Balance, error) {
	var balances []types.Balance
	var pageToken []byte
	for {
		req := &pb.ListBalancesRequest{
			Owner:    proto.String(owner.String()),
			PageSize: proto.Uint32(balancesPageSize),
		}
		if len(pageToken) > 0 {
			req.PageToken = pageToken
		}
		resp, err := c.state.ListBalances(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("GetAllBalances: %w", err)
		}
		for _, balance := range resp.GetBalances() {
			balances = append(balances, adapt.Balance(balance))
		}
		pageToken = resp.GetNextPageToken()
		if len(pageToken) == 0 {
			return balances, nil
		}
	}
}
