package rpcv2

import (
	"slices"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// ToInternalType converts an ExecutedTransaction into the internal transaction
// block response shape. Fields not covered by the request's read mask stay
// silently unset. A transaction that cannot be BCS-decoded reports the problem
// via the response's Errors field instead of failing.
func (x *ExecutedTransaction) ToInternalType(options types.SuiTransactionBlockResponseOptions) *types.SuiTransactionBlockResponse {
	if x == nil {
		return nil
	}
	response := &types.SuiTransactionBlockResponse{
		Digest: parseDigest(x.GetDigest()),
	}
	// The transaction BCS is decoded at most once and shared between the
	// showInput and showObjectChanges paths. System transaction kinds are not
	// in sui_types' enum and fail to decode; their real sender is the zero
	// address, which is what the failed decode yields for object changes.
	var data *sui_types.TransactionData
	if txData := x.GetTransaction().GetBcs().GetValue(); len(txData) > 0 {
		var decodeErr error
		if options.ShowInput || options.ShowObjectChanges {
			data, decodeErr = decodeTransactionData(txData)
		}
		if options.ShowRawInput {
			response.RawTransaction = rawSenderSignedData(txData, x.GetSignatures())
		}
		if options.ShowInput {
			if decodeErr != nil {
				response.Errors = append(response.Errors, decodeErr.Error())
			} else {
				response.Transaction = toInternalTransactionBlock(data, x.GetSignatures())
			}
		}
	}
	// Effects arrive on the wire for ShowObjectChanges too, but are only echoed
	// back when explicitly requested.
	var effectsErrs []error
	if options.ShowEffects {
		effects, errs := x.GetEffects().ToInternalType()
		effectsErrs = errs
		if effects != nil {
			response.Effects = &lib.TagJson[types.SuiTransactionBlockEffects]{Data: *effects}
		}
	}
	var changeErrs []error
	if options.ShowObjectChanges {
		var sender sui_types.SuiAddress
		if data != nil {
			sender = data.V1.Sender
		}
		response.ObjectChanges, changeErrs = x.GetEffects().ToInternalObjectChanges(sender)
	}
	events, eventErrs := x.GetEvents().ToInternalType(x.GetDigest())
	response.Events = events
	balanceChanges, balanceErrs := toInternalBalanceChanges(x.GetBalanceChanges())
	response.BalanceChanges = balanceChanges
	for _, err := range slices.Concat(effectsErrs, changeErrs, eventErrs, balanceErrs) {
		response.Errors = append(response.Errors, err.Error())
	}
	if x.Timestamp != nil {
		timestampMs := types.NewSafeSuiBigInt(uint64(x.GetTimestamp().AsTime().UnixMilli()))
		response.TimestampMs = &timestampMs
	}
	if x.Checkpoint != nil {
		checkpoint := types.NewSafeSuiBigInt[types.CheckpointSequenceNumber](x.GetCheckpoint())
		response.Checkpoint = &checkpoint
	}
	return response
}
