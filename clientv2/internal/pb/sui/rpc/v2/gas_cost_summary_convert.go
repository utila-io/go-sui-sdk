package rpcv2

import "github.com/utila-io/go-sui-sdk/types"

// ToInternalType converts a GasCostSummary into the internal shape used by
// both checkpoints and transaction effects.
func (x *GasCostSummary) ToInternalType() types.GasCostSummary {
	return types.GasCostSummary{
		ComputationCost:         types.NewSafeSuiBigInt(x.GetComputationCost()),
		StorageCost:             types.NewSafeSuiBigInt(x.GetStorageCost()),
		StorageRebate:           types.NewSafeSuiBigInt(x.GetStorageRebate()),
		NonRefundableStorageFee: types.NewSafeSuiBigInt(x.GetNonRefundableStorageFee()),
	}
}
