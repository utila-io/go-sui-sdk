package adapt

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// Long-form object IDs used in changed_objects; the adapters must emit them
// verbatim (v1 keeps object IDs and owner addresses long form).
const (
	createdObjectID   = "0x00000000000000000000000000000000000000000000000000000000000000a1"
	mutatedObjectID   = "0x00000000000000000000000000000000000000000000000000000000000000a2"
	deletedObjectID   = "0x00000000000000000000000000000000000000000000000000000000000000a3"
	unwrapDelObjectID = "0x00000000000000000000000000000000000000000000000000000000000000a4"
	unwrappedObjectID = "0x00000000000000000000000000000000000000000000000000000000000000a5"
	wrappedObjectID   = "0x00000000000000000000000000000000000000000000000000000000000000a6"
	gasObjectID       = "0x00000000000000000000000000000000000000000000000000000000000000a7"
	accumulatorObjID  = "0x00000000000000000000000000000000000000000000000000000000000000a8"
)

func TestEffects_nil(t *testing.T) {
	got, errs := Effects(nil)
	require.Nil(t, got)
	require.Empty(t, errs)
}

func TestEffects_statusFailure(t *testing.T) {
	txDigestStr, txDigest := testDigest(0x11)
	got, errs := Effects(&pb.TransactionEffects{
		Status: &pb.ExecutionStatus{
			Success: proto.Bool(false),
			Error: &pb.ExecutionError{
				Description: proto.String("MoveAbort in 0x2::coin, code 1"),
			},
		},
		TransactionDigest: proto.String(txDigestStr),
	})
	require.NotNil(t, got)
	require.Empty(t, errs)
	require.Equal(t, types.ExecutionStatus{
		Status: types.ExecutionStatusFailure,
		Error:  "MoveAbort in 0x2::coin, code 1",
	}, got.V1.Status)
	require.Equal(t, txDigest, got.V1.TransactionDigest)
	require.False(t, got.IsSuccess())
}

func TestEffects_full(t *testing.T) {
	txDigestStr, txDigest := testDigest(0x11)
	eventsDigestStr, eventsDigest := testDigest(0x22)
	dep1Str, dep1 := testDigest(0x33)
	dep2Str, dep2 := testDigest(0x34)
	createdDigStr, createdDig := testDigest(0x41)
	mutatedDigStr, mutatedDig := testDigest(0x42)
	unwrappedDigStr, unwrappedDig := testDigest(0x45)
	gasDigStr, gasDig := testDigest(0x47)
	// Deleted, unwrapped-then-deleted and wrapped objects have neither output
	// digest nor output version on the wire; the JSON-RPC marker digests and
	// the transaction's lamport version are substituted.
	deletedMarker := parseDigest(objectDigestDeleted)
	wrappedMarker := parseDigest(objectDigestWrapped)

	ownerAddr := mustAddress(t, longOwnerAddress)
	objectOwnerAddr := mustAddress(t, longObjectID)

	fx := &pb.TransactionEffects{
		Status:            &pb.ExecutionStatus{Success: proto.Bool(true)},
		Epoch:             proto.Uint64(7),
		LamportVersion:    proto.Uint64(18),
		GasUsed:           &pb.GasCostSummary{ComputationCost: proto.Uint64(1000), StorageCost: proto.Uint64(2000), StorageRebate: proto.Uint64(300), NonRefundableStorageFee: proto.Uint64(30)},
		TransactionDigest: proto.String(txDigestStr),
		EventsDigest:      proto.String(eventsDigestStr),
		Dependencies:      []string{dep1Str, dep2Str},
		GasObject: &pb.ChangedObject{
			ObjectId:      proto.String(gasObjectID),
			InputState:    pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS.Enum(),
			OutputState:   pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE.Enum(),
			IdOperation:   pb.ChangedObject_NONE.Enum(),
			OutputVersion: proto.Uint64(17),
			OutputDigest:  proto.String(gasDigStr),
			OutputOwner:   &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
		},
		ChangedObjects: []*pb.ChangedObject{
			{ // created
				ObjectId:      proto.String(createdObjectID),
				InputState:    pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST.Enum(),
				OutputState:   pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE.Enum(),
				IdOperation:   pb.ChangedObject_CREATED.Enum(),
				OutputVersion: proto.Uint64(11),
				OutputDigest:  proto.String(createdDigStr),
				OutputOwner:   &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
			},
			{ // mutated, object-owned
				ObjectId:      proto.String(mutatedObjectID),
				InputState:    pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS.Enum(),
				OutputState:   pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE.Enum(),
				IdOperation:   pb.ChangedObject_NONE.Enum(),
				OutputVersion: proto.Uint64(12),
				OutputDigest:  proto.String(mutatedDigStr),
				OutputOwner:   &pb.Owner{Kind: pb.Owner_OBJECT.Enum(), Address: proto.String(longObjectID)},
			},
			{ // deleted: no output digest on the wire
				ObjectId:    proto.String(deletedObjectID),
				InputState:  pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS.Enum(),
				OutputState: pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST.Enum(),
				IdOperation: pb.ChangedObject_DELETED.Enum(),
			},
			{ // unwrapped then deleted: no output digest on the wire
				ObjectId:    proto.String(unwrapDelObjectID),
				InputState:  pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST.Enum(),
				OutputState: pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST.Enum(),
				IdOperation: pb.ChangedObject_DELETED.Enum(),
			},
			{ // unwrapped, shared owner
				ObjectId:      proto.String(unwrappedObjectID),
				InputState:    pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST.Enum(),
				OutputState:   pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE.Enum(),
				IdOperation:   pb.ChangedObject_NONE.Enum(),
				OutputVersion: proto.Uint64(15),
				OutputDigest:  proto.String(unwrappedDigStr),
				OutputOwner:   &pb.Owner{Kind: pb.Owner_SHARED.Enum(), Version: proto.Uint64(5)},
			},
			{ // wrapped: no output digest on the wire
				ObjectId:    proto.String(wrappedObjectID),
				InputState:  pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS.Enum(),
				OutputState: pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST.Enum(),
				IdOperation: pb.ChangedObject_NONE.Enum(),
			},
			{ // accumulator write: merge with integer value
				ObjectId:    proto.String(accumulatorObjID),
				OutputState: pb.ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE.Enum(),
				AccumulatorWrite: &pb.AccumulatorWrite{
					Address:         proto.String(longOwnerAddress),
					AccumulatorType: proto.String(longSuiPackage + "::balance::Balance<" + longSuiType + ">"),
					Operation:       pb.AccumulatorWrite_MERGE.Enum(),
					ValueKind:       pb.AccumulatorWrite_INTEGER.Enum(),
					IntegerValue:    proto.Uint64(500),
				},
			},
			{ // accumulator write: split without integer value
				ObjectId:    proto.String(accumulatorObjID),
				OutputState: pb.ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE.Enum(),
				AccumulatorWrite: &pb.AccumulatorWrite{
					Address:         proto.String(longOwnerAddress),
					AccumulatorType: proto.String(longSuiPackage + "::balance::Balance<" + longUsdcType + ">"),
					Operation:       pb.AccumulatorWrite_SPLIT.Enum(),
				},
			},
			{ // unknown states: must not land in any bucket
				ObjectId: proto.String(longObjectID),
			},
		},
	}

	want := &types.SuiTransactionBlockEffectsV1{
		Status:        types.ExecutionStatus{Status: types.ExecutionStatusSuccess},
		ExecutedEpoch: types.NewSafeSuiBigInt[types.EpochId](7),
		GasUsed: types.GasCostSummary{
			ComputationCost:         types.NewSafeSuiBigInt[uint64](1000),
			StorageCost:             types.NewSafeSuiBigInt[uint64](2000),
			StorageRebate:           types.NewSafeSuiBigInt[uint64](300),
			NonRefundableStorageFee: types.NewSafeSuiBigInt[uint64](30),
		},
		TransactionDigest: txDigest,
		EventsDigest:      &eventsDigest,
		Dependencies:      []sui_types.TransactionDigest{dep1, dep2},
		GasObject: types.OwnedObjectRef{
			Owner:     lib.TagJson[sui_types.Owner]{Data: sui_types.Owner{AddressOwner: &ownerAddr}},
			Reference: types.SuiObjectRef{ObjectId: gasObjectID, Version: 17, Digest: gasDig},
		},
		Created: []types.OwnedObjectRef{{
			Owner:     lib.TagJson[sui_types.Owner]{Data: sui_types.Owner{AddressOwner: &ownerAddr}},
			Reference: types.SuiObjectRef{ObjectId: createdObjectID, Version: 11, Digest: createdDig},
		}},
		Mutated: []types.OwnedObjectRef{{
			Owner:     lib.TagJson[sui_types.Owner]{Data: sui_types.Owner{ObjectOwner: &objectOwnerAddr}},
			Reference: types.SuiObjectRef{ObjectId: mutatedObjectID, Version: 12, Digest: mutatedDig},
		}},
		Deleted: []types.SuiObjectRef{
			{ObjectId: deletedObjectID, Version: 18, Digest: deletedMarker},
		},
		UnwrappedThenDeleted: []types.SuiObjectRef{
			{ObjectId: unwrapDelObjectID, Version: 18, Digest: deletedMarker},
		},
		Unwrapped: []types.OwnedObjectRef{{
			Owner: lib.TagJson[sui_types.Owner]{Data: sui_types.Owner{Shared: &struct {
				InitialSharedVersion sui_types.SequenceNumber `json:"initial_shared_version"`
			}{InitialSharedVersion: 5}}},
			Reference: types.SuiObjectRef{ObjectId: unwrappedObjectID, Version: 15, Digest: unwrappedDig},
		}},
		Wrapped: []types.SuiObjectRef{
			{ObjectId: wrappedObjectID, Version: 18, Digest: wrappedMarker},
		},
		AccumulatorEvents: []types.AccumulatorEvent{
			{
				AccumulatorObj: accumulatorObjID,
				Address:        longOwnerAddress,
				Operation:      types.AccumulatorOperationMerge,
				Ty:             "0x2::balance::Balance<" + shortSuiType + ">",
				Value:          types.AccumulatorEventValue{Integer: ptr(uint64(500))},
			},
			{
				AccumulatorObj: accumulatorObjID,
				Address:        longOwnerAddress,
				Operation:      types.AccumulatorOperationSplit,
				Ty:             "0x2::balance::Balance<" + shortUsdcType + ">",
				Value:          types.AccumulatorEventValue{Integer: nil},
			},
		},
	}

	got, errs := Effects(fx)
	require.Empty(t, errs)
	require.Equal(t, &types.SuiTransactionBlockEffects{V1: want}, got)
	require.True(t, got.IsSuccess())

	// Object IDs and owner addresses are surfaced long form, unlike type
	// strings which are shortened.
	require.Equal(t, createdObjectID, got.V1.Created[0].Reference.ObjectId)
	require.Equal(t, accumulatorObjID, got.V1.AccumulatorEvents[0].AccumulatorObj)
	require.Equal(t, longOwnerAddress, got.V1.AccumulatorEvents[0].Address)
}

// TestEffects_gasObjectAbsent covers system transactions that carry no gas
// object: the v1 GasObject field stays the zero value.
func TestEffects_gasObjectAbsent(t *testing.T) {
	txDigestStr, _ := testDigest(0x11)
	got, errs := Effects(&pb.TransactionEffects{
		Status:            &pb.ExecutionStatus{Success: proto.Bool(true)},
		TransactionDigest: proto.String(txDigestStr),
	})
	require.Empty(t, errs)
	require.Equal(t, types.OwnedObjectRef{}, got.V1.GasObject)
	require.Nil(t, got.V1.EventsDigest)
	require.Nil(t, got.V1.Dependencies)
	require.Nil(t, got.V1.AccumulatorEvents)
}

// TestEffects_invalidOwnerReported covers changed objects whose owner address
// cannot be parsed: the entry is dropped from its bucket and reported.
func TestEffects_invalidOwnerReported(t *testing.T) {
	txDigestStr, _ := testDigest(0x11)
	mutatedDigStr, _ := testDigest(0x42)
	got, errs := Effects(&pb.TransactionEffects{
		Status:            &pb.ExecutionStatus{Success: proto.Bool(true)},
		TransactionDigest: proto.String(txDigestStr),
		ChangedObjects: []*pb.ChangedObject{{
			ObjectId:      proto.String(mutatedObjectID),
			InputState:    pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS.Enum(),
			OutputState:   pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE.Enum(),
			IdOperation:   pb.ChangedObject_NONE.Enum(),
			OutputVersion: proto.Uint64(12),
			OutputDigest:  proto.String(mutatedDigStr),
			OutputOwner:   &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String("0xzz")},
		}},
	})
	require.Empty(t, got.V1.Mutated)
	require.Len(t, errs, 1)
	require.ErrorContains(t, errs[0], mutatedObjectID)
	require.ErrorContains(t, errs[0], "0xzz")
}
