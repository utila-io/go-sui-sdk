package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCommandArgument(t *testing.T) {
	cases := []struct {
		name string
		in   *Argument
		want any
	}{
		{
			name: "gas coin",
			in:   &Argument{Kind: Argument_GAS.Enum()},
			want: "GasCoin",
		},
		{
			name: "input",
			in:   &Argument{Kind: Argument_INPUT.Enum(), Input: proto.Uint32(2)},
			want: map[string]any{"Input": uint32(2)},
		},
		{
			name: "result",
			in:   &Argument{Kind: Argument_RESULT.Enum(), Result: proto.Uint32(3)},
			want: map[string]any{"Result": uint32(3)},
		},
		{
			name: "nested result",
			in:   &Argument{Kind: Argument_RESULT.Enum(), Result: proto.Uint32(3), Subresult: proto.Uint32(1)},
			want: map[string]any{"NestedResult": []any{uint32(3), uint32(1)}},
		},
		{
			name: "nil argument",
			in:   nil,
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.in.ToInternalType())
		})
	}
}
