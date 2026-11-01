package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
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
			name: "nested struct shortens addresses",
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
			want: "0x2::coin::Coin<0x2::sui::SUI>",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, typeTagString(c.in))
		})
	}
}
