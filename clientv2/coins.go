package clientv2

import (
	"context"
	"encoding/base64"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// CoinPage is a page of coins with an opaque base64 cursor, mirroring
// suiclient.CoinPage.
type CoinPage = types.Page[types.Coin, string]

// GetCoins returns a page of coin objects owned by owner via ListOwnedObjects
// with a Coin<T> object_type filter. A nil coinType defaults to 0x2::sui::SUI.
// The cursor is the base64 page token from a previous page's NextCursor.
func (c *Client) GetCoins(
	ctx context.Context,
	owner sui_types.SuiAddress,
	coinType *string,
	cursor *string,
	limit uint,
) (*CoinPage, error) {
	targetType := types.SUI_COIN_TYPE
	if coinType != nil {
		targetType = *coinType
	}
	req := &pb.ListOwnedObjectsRequest{
		Owner:      proto.String(owner.String()),
		ObjectType: proto.String("0x2::coin::Coin<" + targetType + ">"),
		ReadMask:   &fieldmaskpb.FieldMask{Paths: adapt.CoinReadMaskPaths},
	}
	if limit > 0 {
		req.PageSize = proto.Uint32(uint32(limit))
	}
	if cursor != nil && *cursor != "" {
		pageToken, err := base64.StdEncoding.DecodeString(*cursor)
		if err != nil {
			return nil, fmt.Errorf("GetCoins: invalid cursor: %w", err)
		}
		req.PageToken = pageToken
	}
	resp, err := c.state.ListOwnedObjects(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetCoins: %w", err)
	}

	page := &CoinPage{Data: make([]types.Coin, 0, len(resp.GetObjects()))}
	for _, obj := range resp.GetObjects() {
		coin, err := adapt.Coin(obj)
		if err != nil {
			return nil, fmt.Errorf("GetCoins: %w", err)
		}
		page.Data = append(page.Data, coin)
	}
	if nextToken := resp.GetNextPageToken(); len(nextToken) > 0 {
		nextCursor := base64.StdEncoding.EncodeToString(nextToken)
		page.NextCursor = &nextCursor
		page.HasNextPage = true
	}
	return page, nil
}

func (c *Client) GetCoinMetadata(ctx context.Context, coinType string) (*types.SuiCoinMetadata, error) {
	resp, err := c.state.GetCoinInfo(ctx, &pb.GetCoinInfoRequest{CoinType: proto.String(coinType)})
	if err != nil {
		return nil, fmt.Errorf("GetCoinMetadata: %w", err)
	}
	metadata, err := adapt.CoinMetadata(resp)
	if err != nil {
		return nil, fmt.Errorf("GetCoinMetadata: %w", err)
	}
	return metadata, nil
}

func (c *Client) GetReferenceGasPrice(ctx context.Context) (*types.SafeSuiBigInt[uint64], error) {
	resp, err := c.ledger.GetEpoch(ctx, &pb.GetEpochRequest{
		ReadMask: &fieldmaskpb.FieldMask{Paths: []string{"reference_gas_price"}},
	})
	if err != nil {
		return nil, fmt.Errorf("GetReferenceGasPrice: %w", err)
	}
	price := types.NewSafeSuiBigInt(resp.GetEpoch().GetReferenceGasPrice())
	return &price, nil
}
