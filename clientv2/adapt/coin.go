package adapt

import (
	"fmt"
	"strings"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// CoinReadMaskPaths is the ListOwnedObjects read mask needed to build a
// types.Coin from each returned object.
var CoinReadMaskPaths = []string{
	"object_id", "version", "digest", "object_type", "balance", "previous_transaction",
}

// Coin converts a proto Coin<T> object into the JSON-RPC suix_getCoins entry
// shape. LockedUntilEpoch has no gRPC source and is left nil.
func Coin(obj *pb.Object) (types.Coin, error) {
	coinObjectID, err := parseAddress(obj.GetObjectId())
	if err != nil {
		return types.Coin{}, err
	}
	coinType, err := CoinTypeFromObjectType(obj.GetObjectType())
	if err != nil {
		return types.Coin{}, err
	}
	return types.Coin{
		CoinType:            coinType,
		CoinObjectId:        coinObjectID,
		Version:             types.NewSafeSuiBigInt(obj.GetVersion()),
		Digest:              parseDigest(obj.GetDigest()),
		Balance:             types.NewSafeSuiBigInt(obj.GetBalance()),
		PreviousTransaction: parseDigest(obj.GetPreviousTransaction()),
	}, nil
}

// CoinTypeFromObjectType extracts the normalized inner type T from a
// "0x2::coin::Coin<T>" object type string.
func CoinTypeFromObjectType(objectType string) (string, error) {
	open := strings.Index(objectType, "<")
	close_ := strings.LastIndex(objectType, ">")
	if open < 0 || close_ < open {
		return "", fmt.Errorf("unexpected coin object type format: %s", objectType)
	}
	return NormalizeTypeString(objectType[open+1 : close_]), nil
}

// CoinMetadata converts a proto GetCoinInfo response into the JSON-RPC
// suix_getCoinMetadata shape. The metadata object ID may be absent (e.g. for
// wrapped metadata) and is then left as the zero ObjectID, matching v1.
func CoinMetadata(info *pb.GetCoinInfoResponse) (*types.SuiCoinMetadata, error) {
	metadata := info.GetMetadata()
	if metadata == nil {
		return nil, fmt.Errorf("no metadata for coin type %s", info.GetCoinType())
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
