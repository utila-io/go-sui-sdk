package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/utila-io/go-sui-sdk/types"
)

func TestBalanceChanges(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		got, errs := toInternalBalanceChanges(nil)
		require.Nil(t, got)
		require.Empty(t, errs)
		got, errs = toInternalBalanceChanges([]*BalanceChange{})
		require.Nil(t, got)
		require.Empty(t, errs)
	})

	t.Run("coin type normalized owner address kept long", func(t *testing.T) {
		ownerAddr := mustAddress(t, longOwnerAddress)
		got, errs := toInternalBalanceChanges([]*BalanceChange{{
			Address:  proto.String(longOwnerAddress),
			CoinType: proto.String(longSuiType),
			Amount:   proto.String("-100"),
		}})
		require.Empty(t, errs)
		require.Equal(t, []types.BalanceChange{{
			Owner: types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
			},
			CoinType: shortSuiType,
			Amount:   "-100",
		}}, got)
		// The owner address is the full 32-byte address, not a shortened one.
		require.Equal(t, longOwnerAddress, got[0].Owner.AddressOwner.String())
	})

	t.Run("invalid address is reported", func(t *testing.T) {
		got, errs := toInternalBalanceChanges([]*BalanceChange{{
			Address:  proto.String("not-an-address-zz"),
			CoinType: proto.String(longSuiType),
			Amount:   proto.String("1"),
		}})
		require.Empty(t, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "not-an-address-zz")
	})
}
