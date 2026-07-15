package adapt

import (
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// Effects converts proto TransactionEffects into the JSON-RPC effects shape,
// including SIP-58 accumulator events derived from ACCUMULATOR_WRITE entries
// in changed_objects. Returns nil when fx is nil (effects not requested).
func Effects(fx *pb.TransactionEffects) *types.SuiTransactionBlockEffects {
	if fx == nil {
		return nil
	}
	v1 := &types.SuiTransactionBlockEffectsV1{
		Status:            executionStatus(fx.GetStatus()),
		ExecutedEpoch:     types.NewSafeSuiBigInt(fx.GetEpoch()),
		GasUsed:           gasCostSummary(fx.GetGasUsed()),
		TransactionDigest: parseDigest(fx.GetTransactionDigest()),
	}
	if fx.EventsDigest != nil {
		eventsDigest := parseDigest(fx.GetEventsDigest())
		v1.EventsDigest = &eventsDigest
	}
	if gasObject := fx.GetGasObject(); gasObject != nil {
		v1.GasObject = ownedObjectRef(gasObject)
	}
	for _, dep := range fx.GetDependencies() {
		v1.Dependencies = append(v1.Dependencies, parseDigest(dep))
	}
	for _, changed := range fx.GetChangedObjects() {
		mapChangedObject(v1, changed)
	}
	return &types.SuiTransactionBlockEffects{V1: v1}
}

// mapChangedObject sorts one changed_objects entry into the JSON-RPC
// created/mutated/unwrapped/deleted/wrapped buckets (or accumulatorEvents),
// mirroring how Sui derives ObjectChange categories from effects V2.
func mapChangedObject(v1 *types.SuiTransactionBlockEffectsV1, changed *pb.ChangedObject) {
	written := changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE ||
		changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE
	existed := changed.GetInputState() == pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS

	switch {
	case changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE:
		v1.AccumulatorEvents = append(v1.AccumulatorEvents, accumulatorEvent(changed))
	case changed.GetIdOperation() == pb.ChangedObject_CREATED && written:
		v1.Created = append(v1.Created, ownedObjectRef(changed))
	case changed.GetIdOperation() == pb.ChangedObject_DELETED:
		if existed {
			v1.Deleted = append(v1.Deleted, outputObjectRef(changed))
		} else {
			v1.UnwrappedThenDeleted = append(v1.UnwrappedThenDeleted, outputObjectRef(changed))
		}
	case changed.GetIdOperation() == pb.ChangedObject_NONE && written:
		if existed {
			v1.Mutated = append(v1.Mutated, ownedObjectRef(changed))
		} else {
			v1.Unwrapped = append(v1.Unwrapped, ownedObjectRef(changed))
		}
	case changed.GetIdOperation() == pb.ChangedObject_NONE && existed &&
		changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST:
		v1.Wrapped = append(v1.Wrapped, outputObjectRef(changed))
	}
}

// accumulatorEvent converts an ACCUMULATOR_WRITE changed_objects entry into
// the accumulatorEvents shape emitted by JSON-RPC effects.
func accumulatorEvent(changed *pb.ChangedObject) types.AccumulatorEvent {
	write := changed.GetAccumulatorWrite()
	event := types.AccumulatorEvent{
		AccumulatorObj: changed.GetObjectId(),
		Address:        write.GetAddress(),
		Ty:             NormalizeTypeString(write.GetAccumulatorType()),
	}
	switch write.GetOperation() {
	case pb.AccumulatorWrite_MERGE:
		event.Operation = types.AccumulatorOperationMerge
	case pb.AccumulatorWrite_SPLIT:
		event.Operation = types.AccumulatorOperationSplit
	}
	if write.IntegerValue != nil {
		value := write.GetIntegerValue()
		event.Value.Integer = &value
	}
	return event
}

func executionStatus(status *pb.ExecutionStatus) types.ExecutionStatus {
	if status.GetSuccess() {
		return types.ExecutionStatus{Status: types.ExecutionStatusSuccess}
	}
	return types.ExecutionStatus{
		Status: types.ExecutionStatusFailure,
		Error:  status.GetError().GetDescription(),
	}
}

func gasCostSummary(summary *pb.GasCostSummary) types.GasCostSummary {
	return types.GasCostSummary{
		ComputationCost:         types.NewSafeSuiBigInt(summary.GetComputationCost()),
		StorageCost:             types.NewSafeSuiBigInt(summary.GetStorageCost()),
		StorageRebate:           types.NewSafeSuiBigInt(summary.GetStorageRebate()),
		NonRefundableStorageFee: types.NewSafeSuiBigInt(summary.GetNonRefundableStorageFee()),
	}
}

// ownedObjectRef builds the ref+owner pair from a changed object's output
// state (the post-transaction version/digest/owner).
func ownedObjectRef(changed *pb.ChangedObject) types.OwnedObjectRef {
	return types.OwnedObjectRef{
		Owner:     lib.TagJson[sui_types.Owner]{Data: owner(changed.GetOutputOwner())},
		Reference: outputObjectRef(changed),
	}
}

// outputObjectRef builds an object ref from a changed object's output
// version/digest. For deleted and wrapped objects the output version is the
// transaction's lamport version, matching JSON-RPC.
func outputObjectRef(changed *pb.ChangedObject) types.SuiObjectRef {
	return types.SuiObjectRef{
		ObjectId: changed.GetObjectId(),
		Version:  changed.GetOutputVersion(),
		Digest:   parseDigest(changed.GetOutputDigest()),
	}
}

// owner converts a proto Owner into the sui_types.Owner enum. A
// CONSENSUS_ADDRESS owner is surfaced as AddressOwner, the closest v1 variant.
func owner(protoOwner *pb.Owner) sui_types.Owner {
	var out sui_types.Owner
	switch protoOwner.GetKind() {
	case pb.Owner_ADDRESS, pb.Owner_CONSENSUS_ADDRESS:
		if addr, err := parseAddress(protoOwner.GetAddress()); err == nil {
			out.AddressOwner = &addr
		}
	case pb.Owner_OBJECT:
		if addr, err := parseAddress(protoOwner.GetAddress()); err == nil {
			out.ObjectOwner = &addr
		}
	case pb.Owner_SHARED:
		out.Shared = &struct {
			InitialSharedVersion sui_types.SequenceNumber `json:"initial_shared_version"`
		}{InitialSharedVersion: protoOwner.GetVersion()}
	case pb.Owner_IMMUTABLE:
		out.Immutable = &lib.EmptyEnum{}
	}
	return out
}
