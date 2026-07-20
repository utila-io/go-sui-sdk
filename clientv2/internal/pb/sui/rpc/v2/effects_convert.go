package rpcv2

import (
	"fmt"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// Marker digests for deleted ([99; 32]) and wrapped ([88; 32]) objects,
// base58-encoded (Sui's digests.rs). gRPC leaves output_digest unset for
// these, so the markers are substituted to match the internal effects shape.
const (
	objectDigestDeleted = "7gyGAp71YXQRoxmFBaHxofQXAipvgHyBKPyxmdSJxyvz"
	objectDigestWrapped = "6ws1bVyu3F8wGy1fPHhrc2v8UyWiGbRAAuek8SwikKPD"
)

// ToInternalType converts TransactionEffects into the internal effects shape;
// a nil receiver (effects not requested) yields nil. Unparseable entries are
// dropped and reported in the error slice; everything else is still converted.
func (x *TransactionEffects) ToInternalType() (*types.SuiTransactionBlockEffects, []error) {
	if x == nil {
		return nil, nil
	}
	var errs []error
	out := &types.SuiTransactionBlockEffectsV1{
		Status:            x.GetStatus().ToInternalType(),
		ExecutedEpoch:     types.NewSafeSuiBigInt(x.GetEpoch()),
		GasUsed:           x.GetGasUsed().ToInternalType(),
		TransactionDigest: parseDigest(x.GetTransactionDigest()),
	}
	if x.EventsDigest != nil {
		eventsDigest := parseDigest(x.GetEventsDigest())
		out.EventsDigest = &eventsDigest
	}
	if gasObject := x.GetGasObject(); gasObject != nil {
		ref, err := gasObject.toInternalOwnedObjectRef()
		if err != nil {
			errs = append(errs, fmt.Errorf("effects: gas object: %w", err))
		} else {
			out.GasObject = ref
		}
	}
	for _, dep := range x.GetDependencies() {
		out.Dependencies = append(out.Dependencies, parseDigest(dep))
	}
	for _, changed := range x.GetChangedObjects() {
		if err := changed.applyToInternalEffects(out, x.GetLamportVersion()); err != nil {
			errs = append(errs, fmt.Errorf("effects: changed object %s: %w", changed.GetObjectId(), err))
		}
	}
	return &types.SuiTransactionBlockEffects{V1: out}, errs
}

// applyToInternalEffects sorts one changed_objects entry into the internal
// created/mutated/unwrapped/deleted/wrapped buckets (or accumulatorEvents).
func (x *ChangedObject) applyToInternalEffects(out *types.SuiTransactionBlockEffectsV1, lamportVersion uint64) error {
	written := x.GetOutputState() == ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE ||
		x.GetOutputState() == ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE
	existed := x.GetInputState() == ChangedObject_INPUT_OBJECT_STATE_EXISTS

	switch {
	case x.GetOutputState() == ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE:
		out.AccumulatorEvents = append(out.AccumulatorEvents, x.toInternalAccumulatorEvent())
	case x.GetIdOperation() == ChangedObject_CREATED && written:
		ref, err := x.toInternalOwnedObjectRef()
		if err != nil {
			return err
		}
		out.Created = append(out.Created, ref)
	case x.GetIdOperation() == ChangedObject_DELETED:
		if existed {
			out.Deleted = append(out.Deleted, x.toInternalMarkerObjectRef(lamportVersion, objectDigestDeleted))
		} else {
			out.UnwrappedThenDeleted = append(out.UnwrappedThenDeleted, x.toInternalMarkerObjectRef(lamportVersion, objectDigestDeleted))
		}
	case x.GetIdOperation() == ChangedObject_NONE && written:
		ref, err := x.toInternalOwnedObjectRef()
		if err != nil {
			return err
		}
		if existed {
			out.Mutated = append(out.Mutated, ref)
		} else {
			out.Unwrapped = append(out.Unwrapped, ref)
		}
	case x.GetIdOperation() == ChangedObject_NONE && existed &&
		x.GetOutputState() == ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST:
		out.Wrapped = append(out.Wrapped, x.toInternalMarkerObjectRef(lamportVersion, objectDigestWrapped))
	}
	return nil
}

func (x *ChangedObject) toInternalAccumulatorEvent() types.AccumulatorEvent {
	write := x.GetAccumulatorWrite()
	event := types.AccumulatorEvent{
		AccumulatorObj: x.GetObjectId(),
		Address:        write.GetAddress(),
		Ty:             normalizeTypeString(write.GetAccumulatorType()),
	}
	switch write.GetOperation() {
	case AccumulatorWrite_MERGE:
		event.Operation = types.AccumulatorOperationMerge
	case AccumulatorWrite_SPLIT:
		event.Operation = types.AccumulatorOperationSplit
	}
	// accumulator_write is an optional sub-message: a node can set the state
	// without the payload, so presence has to be checked on write itself
	// before reaching through it for the value.
	if write != nil && write.IntegerValue != nil {
		value := write.GetIntegerValue()
		event.Value.Integer = &value
	}
	return event
}

// toInternalOwnedObjectRef builds the ref+owner pair from a changed object's
// output state (the post-transaction version/digest/owner).
func (x *ChangedObject) toInternalOwnedObjectRef() (types.OwnedObjectRef, error) {
	objectOwner, err := x.GetOutputOwner().ToInternalOwner()
	if err != nil {
		return types.OwnedObjectRef{}, err
	}
	return types.OwnedObjectRef{
		Owner: lib.TagJson[sui_types.Owner]{Data: objectOwner},
		Reference: types.SuiObjectRef{
			ObjectId: x.GetObjectId(),
			Version:  x.GetOutputVersion(),
			Digest:   parseDigest(x.GetOutputDigest()),
		},
	}, nil
}

// toInternalMarkerObjectRef builds the ref for a deleted or wrapped object,
// whose output digest/version are unset on the wire: the marker digest and the
// transaction's lamport version are substituted.
func (x *ChangedObject) toInternalMarkerObjectRef(lamportVersion uint64, markerDigest string) types.SuiObjectRef {
	return types.SuiObjectRef{
		ObjectId: x.GetObjectId(),
		Version:  lamportVersion,
		Digest:   parseDigest(markerDigest),
	}
}
