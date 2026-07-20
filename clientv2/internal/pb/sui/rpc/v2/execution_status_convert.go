package rpcv2

import "github.com/utila-io/go-sui-sdk/types"

// ToInternalType converts an ExecutionStatus into the internal status shape,
// flattening the failure detail to its description string.
func (x *ExecutionStatus) ToInternalType() types.ExecutionStatus {
	if x.GetSuccess() {
		return types.ExecutionStatus{Status: types.ExecutionStatusSuccess}
	}
	return types.ExecutionStatus{
		Status: types.ExecutionStatusFailure,
		Error:  x.GetError().GetDescription(),
	}
}
