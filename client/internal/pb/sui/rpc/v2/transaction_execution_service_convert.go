package rpcv2

import "github.com/utila-io/go-sui-sdk/types"

// ToInternalExecutionResults converts the per-command outputs of a simulated
// transaction into the internal dev-inspect results shape.
func (x *SimulateTransactionResponse) ToInternalExecutionResults() []types.ExecutionResultType {
	outputs := x.GetCommandOutputs()
	if len(outputs) == 0 {
		return nil
	}
	results := make([]types.ExecutionResultType, len(outputs))
	for i, output := range outputs {
		results[i] = output.ToInternalType()
	}
	return results
}

// ToInternalType converts one command's outputs. Bytes stay []byte, which
// encodes as base64 in JSON.
func (x *CommandResult) ToInternalType() types.ExecutionResultType {
	var result types.ExecutionResultType
	for _, mutated := range x.GetMutatedByRef() {
		result.MutableReferenceOutputs = append(result.MutableReferenceOutputs,
			types.MutableReferenceOutputType([]any{
				mutated.GetArgument().ToInternalType(),
				mutated.GetValue().GetValue(),
				normalizeTypeString(mutated.GetValue().GetName()),
			}))
	}
	for _, returned := range x.GetReturnValues() {
		result.ReturnValues = append(result.ReturnValues,
			types.ReturnValueType([]any{
				returned.GetValue().GetValue(),
				normalizeTypeString(returned.GetValue().GetName()),
			}))
	}
	return result
}
