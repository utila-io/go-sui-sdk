package adapt

import (
	"fmt"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// JSON-RPC marker digests for objects with no output state, from Sui's
// crates/sui-types/src/digests.rs: ObjectDigest::OBJECT_DIGEST_DELETED is
// [99; 32] and ObjectDigest::OBJECT_DIGEST_WRAPPED is [88; 32], base58-encoded.
// gRPC leaves output_digest unset for these objects, so the markers are
// substituted to match the JSON-RPC effects shape.
const (
	objectDigestDeleted = "7gyGAp71YXQRoxmFBaHxofQXAipvgHyBKPyxmdSJxyvz"
	objectDigestWrapped = "6ws1bVyu3F8wGy1fPHhrc2v8UyWiGbRAAuek8SwikKPD"
)

// Effects converts proto TransactionEffects into the JSON-RPC effects shape,
// including SIP-58 accumulator events derived from ACCUMULATOR_WRITE entries
// in changed_objects. Returns nil when fx is nil (effects not requested).
// Entries whose owner cannot be parsed are dropped and reported in the
// returned error slice; everything else is still converted.
func Effects(fx *pb.TransactionEffects) (*types.SuiTransactionBlockEffects, []error) {
	if fx == nil {
		return nil, nil
	}
	var errs []error
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
		ref, err := ownedObjectRef(gasObject)
		if err != nil {
			errs = append(errs, fmt.Errorf("effects: gas object: %w", err))
		} else {
			v1.GasObject = ref
		}
	}
	for _, dep := range fx.GetDependencies() {
		v1.Dependencies = append(v1.Dependencies, parseDigest(dep))
	}
	for _, changed := range fx.GetChangedObjects() {
		if err := mapChangedObject(v1, changed, fx.GetLamportVersion()); err != nil {
			errs = append(errs, fmt.Errorf("effects: changed object %s: %w", changed.GetObjectId(), err))
		}
	}
	return &types.SuiTransactionBlockEffects{V1: v1}, errs
}

// mapChangedObject sorts one changed_objects entry into the JSON-RPC
// created/mutated/unwrapped/deleted/wrapped buckets (or accumulatorEvents),
// mirroring how Sui derives ObjectChange categories from effects V2.
func mapChangedObject(v1 *types.SuiTransactionBlockEffectsV1, changed *pb.ChangedObject, lamportVersion uint64) error {
	written := changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE ||
		changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE
	existed := changed.GetInputState() == pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS

	switch {
	case changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE:
		v1.AccumulatorEvents = append(v1.AccumulatorEvents, accumulatorEvent(changed))
	case changed.GetIdOperation() == pb.ChangedObject_CREATED && written:
		ref, err := ownedObjectRef(changed)
		if err != nil {
			return err
		}
		v1.Created = append(v1.Created, ref)
	case changed.GetIdOperation() == pb.ChangedObject_DELETED:
		if existed {
			v1.Deleted = append(v1.Deleted, markerObjectRef(changed, lamportVersion, objectDigestDeleted))
		} else {
			v1.UnwrappedThenDeleted = append(v1.UnwrappedThenDeleted, markerObjectRef(changed, lamportVersion, objectDigestDeleted))
		}
	case changed.GetIdOperation() == pb.ChangedObject_NONE && written:
		ref, err := ownedObjectRef(changed)
		if err != nil {
			return err
		}
		if existed {
			v1.Mutated = append(v1.Mutated, ref)
		} else {
			v1.Unwrapped = append(v1.Unwrapped, ref)
		}
	case changed.GetIdOperation() == pb.ChangedObject_NONE && existed &&
		changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST:
		v1.Wrapped = append(v1.Wrapped, markerObjectRef(changed, lamportVersion, objectDigestWrapped))
	}
	return nil
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
func ownedObjectRef(changed *pb.ChangedObject) (types.OwnedObjectRef, error) {
	objectOwner, err := owner(changed.GetOutputOwner())
	if err != nil {
		return types.OwnedObjectRef{}, err
	}
	return types.OwnedObjectRef{
		Owner: lib.TagJson[sui_types.Owner]{Data: objectOwner},
		Reference: types.SuiObjectRef{
			ObjectId: changed.GetObjectId(),
			Version:  changed.GetOutputVersion(),
			Digest:   parseDigest(changed.GetOutputDigest()),
		},
	}, nil
}

// markerObjectRef builds the object ref for a deleted or wrapped object.
// Its output_digest and output_version are unset on the wire, so the JSON-RPC
// marker digest and the transaction's lamport version are substituted,
// matching how JSON-RPC renders refs of objects with no output state.
func markerObjectRef(changed *pb.ChangedObject, lamportVersion uint64, markerDigest string) types.SuiObjectRef {
	return types.SuiObjectRef{
		ObjectId: changed.GetObjectId(),
		Version:  lamportVersion,
		Digest:   parseDigest(markerDigest),
	}
}

// owner converts a proto Owner into the sui_types.Owner enum. A
// CONSENSUS_ADDRESS owner is surfaced as AddressOwner, the closest v1 variant.
func owner(protoOwner *pb.Owner) (sui_types.Owner, error) {
	var out sui_types.Owner
	switch protoOwner.GetKind() {
	case pb.Owner_ADDRESS, pb.Owner_CONSENSUS_ADDRESS:
		addr, err := parseAddress(protoOwner.GetAddress())
		if err != nil {
			return out, fmt.Errorf("owner: %w", err)
		}
		out.AddressOwner = &addr
	case pb.Owner_OBJECT:
		addr, err := parseAddress(protoOwner.GetAddress())
		if err != nil {
			return out, fmt.Errorf("owner: %w", err)
		}
		out.ObjectOwner = &addr
	case pb.Owner_SHARED:
		out.Shared = &struct {
			InitialSharedVersion sui_types.SequenceNumber `json:"initial_shared_version"`
		}{InitialSharedVersion: protoOwner.GetVersion()}
	case pb.Owner_IMMUTABLE:
		out.Immutable = &lib.EmptyEnum{}
	}
	return out, nil
}
