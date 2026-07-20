package rpcv2

import (
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

func parseAddress(str string) (sui_types.SuiAddress, error) {
	addr, err := sui_types.NewAddressFromHex(str)
	if err != nil {
		return sui_types.SuiAddress{}, fmt.Errorf("invalid address %q: %w", str, err)
	}
	return *addr, nil
}

// Invalid base58 decodes to an empty digest rather than an error: the node
// only emits canonical base58, so it is not worth threading an error through.
func parseDigest(str string) sui_types.Digest {
	digest, _ := lib.NewBase58(str)
	return *digest
}

// Via big.Int so values above MaxInt64 do not overflow.
func decimalFromUint64(num uint64) decimal.Decimal {
	return decimal.NewFromBigInt(new(big.Int).SetUint64(num), 0)
}
