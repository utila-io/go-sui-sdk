package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

func TestTypeTagString(t *testing.T) {
	u64Tag := move_types.TypeTag{U64: &lib.EmptyEnum{}}
	cases := []struct {
		name string
		in   move_types.TypeTag
		want string
	}{
		{name: "u64", in: u64Tag, want: "u64"},
		{name: "vector of u64", in: move_types.TypeTag{Vector: &u64Tag}, want: "vector<u64>"},
		{name: "address", in: move_types.TypeTag{Address: &lib.EmptyEnum{}}, want: "address"},
		{
			name: "nested struct keeps 64-hex addresses",
			in: move_types.TypeTag{Struct: &move_types.StructTag{
				Address: mustAddress(t, longSuiPackage),
				Module:  "coin",
				Name:    "Coin",
				TypeParams: []move_types.TypeTag{{Struct: &move_types.StructTag{
					Address: mustAddress(t, longSuiPackage),
					Module:  "sui",
					Name:    "SUI",
				}}},
			}},
			want: longGasType,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, typeTagString(c.in))
		})
	}
}

func TestCallArgJSONFundsWithdrawal(t *testing.T) {
	maxAmount := uint64(2_000_000)
	arg := sui_types.CallArg{FundsWithdrawal: &sui_types.FundsWithdrawalArg{
		Reservation: sui_types.Reservation{MaxAmountU64: &maxAmount},
		TypeArg: sui_types.WithdrawalTypeArg{Balance: &move_types.TypeTag{Struct: &move_types.StructTag{
			Address: mustAddress(t, longSuiPackage),
			Module:  "sui",
			Name:    "SUI",
		}}},
		WithdrawFrom: sui_types.WithdrawFrom{Sender: &lib.EmptyEnum{}},
	}}
	require.Equal(t, map[string]interface{}{
		"type":         "fundsWithdrawal",
		"reservation":  map[string]interface{}{"maxAmountU64": "2000000"},
		"typeArg":      map[string]interface{}{"balance": longSuiType},
		"withdrawFrom": "sender",
	}, callArgJSON(arg, ""))
}
