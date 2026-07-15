package adapt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

func TestBalance(t *testing.T) {
	tests := []struct {
		name                      string
		in                        *pb.Balance
		wantCoinType              string
		wantTotalBalance          string
		wantFundsInAddressBalance string
	}{
		{
			name: "all fields set",
			in: &pb.Balance{
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
			in: &pb.Balance{
				CoinType: proto.String(longUsdcType),
			},
			wantCoinType:              shortUsdcType,
			wantTotalBalance:          "0",
			wantFundsInAddressBalance: "0",
		},
		{
			name: "max uint64 does not overflow",
			in: &pb.Balance{
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
			in:                        &pb.Balance{},
			wantCoinType:              "",
			wantTotalBalance:          "0",
			wantFundsInAddressBalance: "0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Balance(tt.in)
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
// shape JSON-RPC v1 callers expect. (The whole types.Balance struct is not
// json.Marshal-able because of the LockedBalance map key, so the decimal
// fields are marshaled individually.)
func TestBalance_json(t *testing.T) {
	got := Balance(&pb.Balance{
		CoinType:       proto.String(longSuiType),
		Balance:        proto.Uint64(18446744073709551615),
		AddressBalance: proto.Uint64(42),
	})
	total, err := json.Marshal(got.TotalBalance)
	require.NoError(t, err)
	require.Equal(t, `"18446744073709551615"`, string(total))
	funds, err := json.Marshal(got.FundsInAddressBalance)
	require.NoError(t, err)
	require.Equal(t, `"42"`, string(funds))
}
