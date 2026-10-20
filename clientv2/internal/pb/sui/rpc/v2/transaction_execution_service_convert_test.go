package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/utila-io/go-sui-sdk/types"
)

func TestExecutionResults(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		require.Nil(t, (&SimulateTransactionResponse{CommandOutputs: nil}).ToInternalExecutionResults())
		require.Nil(t, (&SimulateTransactionResponse{CommandOutputs: []*CommandResult{}}).ToInternalExecutionResults())
	})

	t.Run("return values and mutated by ref", func(t *testing.T) {
		got := (&SimulateTransactionResponse{CommandOutputs: []*CommandResult{
			{
				MutatedByRef: []*CommandOutput{{
					Argument: &Argument{Kind: Argument_GAS.Enum()},
					Value: &Bcs{
						Name:  proto.String(longSuiPackage + "::coin::Coin<" + longSuiType + ">"),
						Value: []byte{0x01, 0x02},
					},
				}},
				ReturnValues: []*CommandOutput{{
					Argument: &Argument{
						Kind:      Argument_RESULT.Enum(),
						Result:    proto.Uint32(0),
						Subresult: proto.Uint32(1),
					},
					Value: &Bcs{Name: proto.String("u64"), Value: []byte{0xe8, 0x03}},
				}},
			},
			{}, // command without outputs
		}}).ToInternalExecutionResults()
		require.Equal(t, []types.ExecutionResultType{
			{
				MutableReferenceOutputs: []types.MutableReferenceOutputType{
					[]any{"GasCoin", []byte{0x01, 0x02}, "0x2::coin::Coin<" + shortSuiType + ">"},
				},
				ReturnValues: []types.ReturnValueType{
					[]any{[]byte{0xe8, 0x03}, "u64"},
				},
			},
			{},
		}, got)
	})
}
