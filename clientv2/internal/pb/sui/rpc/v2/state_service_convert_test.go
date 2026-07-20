package rpcv2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestBalance(t *testing.T) {
	tests := []struct {
		name                      string
		in                        *Balance
		wantCoinType              string
		wantTotalBalance          string
		wantFundsInAddressBalance string
	}{
		{
			name: "all fields set",
			in: &Balance{
				CoinType:       proto.String(longSuiType),
				Balance:        proto.Uint64(1000),
				AddressBalance: proto.Uint64(250),
				CoinBalance:    proto.Uint64(750), // has no types.Balance field; must not leak anywhere
			},
			wantCoinType:              shortSuiType,
			wantTotalBalance:          "1000",
			wantFundsInAddressBalance: "250",
		},
		{
			name: "absent optional balances default to zero",
			in: &Balance{
				CoinType: proto.String(longUsdcType),
			},
			wantCoinType:              shortUsdcType,
			wantTotalBalance:          "0",
			wantFundsInAddressBalance: "0",
		},
		{
			name: "max uint64 does not overflow",
			in: &Balance{
				CoinType:       proto.String(shortSuiType),
				Balance:        proto.Uint64(18446744073709551615),
				AddressBalance: proto.Uint64(9223372036854775808), // MaxInt64 + 1
			},
			wantCoinType:              shortSuiType,
			wantTotalBalance:          "18446744073709551615",
			wantFundsInAddressBalance: "9223372036854775808",
		},
		{
			name:                      "empty proto message",
			in:                        &Balance{},
			wantCoinType:              "",
			wantTotalBalance:          "0",
			wantFundsInAddressBalance: "0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.ToInternalType()
			require.Equal(t, tt.wantCoinType, got.CoinType)
			require.Equal(t, tt.wantTotalBalance, got.TotalBalance.String())
			require.Equal(t, tt.wantFundsInAddressBalance, got.FundsInAddressBalance.String())
			// No gRPC source for these; they must stay zero.
			require.Zero(t, got.CoinObjectCount)
			require.Nil(t, got.LockedBalance)
		})
	}
}

// TestBalance_json verifies the big-int fields marshal as JSON strings, the
// shape callers expect. (The whole types.Balance struct is not
// json.Marshal-able because of the LockedBalance map key, so the decimal
// fields are marshaled individually.)
func TestBalance_json(t *testing.T) {
	got := (&Balance{
		CoinType:       proto.String(longSuiType),
		Balance:        proto.Uint64(18446744073709551615),
		AddressBalance: proto.Uint64(42),
	}).ToInternalType()
	total, err := json.Marshal(got.TotalBalance)
	require.NoError(t, err)
	require.Equal(t, `"18446744073709551615"`, string(total))
	funds, err := json.Marshal(got.FundsInAddressBalance)
	require.NoError(t, err)
	require.Equal(t, `"42"`, string(funds))
}

func TestCoinMetadata(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		got, err := (&GetCoinInfoResponse{
			CoinType: proto.String(longUsdcType),
			Metadata: &CoinMetadata{
				Id:          proto.String(longObjectID),
				Decimals:    proto.Uint32(6),
				Name:        proto.String("USD Coin"),
				Symbol:      proto.String("USDC"),
				Description: proto.String("Stablecoin"),
				IconUrl:     proto.String("https://example.com/usdc.png"),
			},
		}).ToInternalType()
		require.NoError(t, err)
		require.Equal(t, &types.SuiCoinMetadata{
			Decimals:    6,
			Description: "Stablecoin",
			IconUrl:     "https://example.com/usdc.png",
			Id:          mustAddress(t, longObjectID),
			Name:        "USD Coin",
			Symbol:      "USDC",
		}, got)
	})

	t.Run("absent id stays zero", func(t *testing.T) {
		got, err := (&GetCoinInfoResponse{
			Metadata: &CoinMetadata{Decimals: proto.Uint32(9), Symbol: proto.String("SUI")},
		}).ToInternalType()
		require.NoError(t, err)
		require.Equal(t, sui_types.ObjectID{}, got.Id)
		require.Equal(t, uint8(9), got.Decimals)
	})

	t.Run("no metadata is an error", func(t *testing.T) {
		_, err := (&GetCoinInfoResponse{CoinType: proto.String(shortSuiType)}).ToInternalType()
		require.Error(t, err)
	})

	t.Run("invalid id is an error", func(t *testing.T) {
		_, err := (&GetCoinInfoResponse{
			Metadata: &CoinMetadata{Id: proto.String("0xnothex")},
		}).ToInternalType()
		require.Error(t, err)
	})
}
