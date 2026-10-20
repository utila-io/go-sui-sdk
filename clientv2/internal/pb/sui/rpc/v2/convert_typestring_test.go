package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeTypeString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "long form trimmed to short form",
			in:   longSuiType,
			want: shortSuiType,
		},
		{
			name: "nested generics normalize every address",
			in: "0x0000000000000000000000000000000000000000000000000000000000000002::coin::Coin<" +
				"0x00000000000000000000000000000000000000000000000000000000000000ab::mod::T<" +
				"0x0000000000000000000000000000000000000000000000000000000000000001::x::Y>>",
			want: "0x2::coin::Coin<0xab::mod::T<0x1::x::Y>>",
		},
		{
			name: "multiple generic parameters",
			in:   "0x00000000000000000000000000000000000000000000000000000000000000ab::lp::LP<" + longSuiType + ", " + longUsdcType + ">",
			want: "0xab::lp::LP<" + shortSuiType + ", " + shortUsdcType + ">",
		},
		{
			name: "all-zero address keeps one digit",
			in:   "0x0000000000000000000000000000000000000000000000000000000000000000::a::B",
			want: "0x0::a::B",
		},
		{
			name: "short zero address stays one digit",
			in:   "0x0::a::B",
			want: "0x0::a::B",
		},
		{
			name: "already short passthrough",
			in:   shortSuiType,
			want: shortSuiType,
		},
		{
			name: "non-hex passthrough package",
			in:   "package",
			want: "package",
		},
		{
			name: "non-hex passthrough primitive generic",
			in:   "vector<u8>",
			want: "vector<u8>",
		},
		{
			name: "empty string passthrough",
			in:   "",
			want: "",
		},
		{
			name: "uppercase hex prefix is lowercased and trimmed",
			in:   "0X0002::m::T",
			want: "0x2::m::T",
		},
		{
			name: "hex digit case is preserved after trimming",
			in:   "0x00Ab::m::T",
			want: "0xAb::m::T",
		},
		{
			name: "no leading zeros left untouched",
			in:   "0xdee9::clob_v2::Pool<" + shortSuiType + ", " + shortUsdcType + ">",
			want: "0xdee9::clob_v2::Pool<" + shortSuiType + ", " + shortUsdcType + ">",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeTypeString(tt.in))
		})
	}
}
