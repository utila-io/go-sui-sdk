package adapt

import (
	"math/big"

	"github.com/shopspring/decimal"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// Balance converts a proto Balance into the JSON-RPC suix_getBalance shape.
// CoinObjectCount and LockedBalance have no gRPC source and are left zero.
func Balance(balance *pb.Balance) types.Balance {
	return types.Balance{
		CoinType:              NormalizeTypeString(balance.GetCoinType()),
		TotalBalance:          decimalFromUint64(balance.GetBalance()),
		FundsInAddressBalance: decimalFromUint64(balance.GetAddressBalance()),
	}
}

// Via big.Int so values above MaxInt64 do not overflow.
func decimalFromUint64(num uint64) decimal.Decimal {
	return decimal.NewFromBigInt(new(big.Int).SetUint64(num), 0)
}
