package rpcv2

import (
	"fmt"

	"github.com/utila-io/go-sui-sdk/types"
)

// ToInternalType converts a Balance into the internal balance shape.
// CoinObjectCount and LockedBalance have no gRPC source and are left zero.
func (x *Balance) ToInternalType() types.Balance {
	return types.Balance{
		CoinType:              normalizeTypeString(x.GetCoinType()),
		TotalBalance:          decimalFromUint64(x.GetBalance()),
		FundsInAddressBalance: decimalFromUint64(x.GetAddressBalance()),
	}
}

// ToInternalType converts a GetCoinInfo response into the internal coin
// metadata shape. The metadata object ID may be absent (e.g. for wrapped
// metadata) and is then left as the zero ObjectID.
func (x *GetCoinInfoResponse) ToInternalType() (*types.SuiCoinMetadata, error) {
	metadata := x.GetMetadata()
	if metadata == nil {
		return nil, fmt.Errorf("no metadata for coin type %s", x.GetCoinType())
	}
	out := &types.SuiCoinMetadata{
		Decimals:    uint8(metadata.GetDecimals()),
		Description: metadata.GetDescription(),
		IconUrl:     metadata.GetIconUrl(),
		Name:        metadata.GetName(),
		Symbol:      metadata.GetSymbol(),
	}
	if metadata.GetId() != "" {
		id, err := parseAddress(metadata.GetId())
		if err != nil {
			return nil, err
		}
		out.Id = id
	}
	return out, nil
}
