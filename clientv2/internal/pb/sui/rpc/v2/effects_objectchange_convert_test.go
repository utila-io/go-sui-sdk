package rpcv2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestObjectChanges(t *testing.T) {
	sender := mustAddress(t, longOwnerAddress)
	outDigestStr, outDigest := testDigest(0x51)
	inDigestStr, _ := testDigest(0x52)

	t.Run("nil effects yield nothing", func(t *testing.T) {
		got, errs := (*TransactionEffects)(nil).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Nil(t, got)
	})

	t.Run("gas coin mutation is included", func(t *testing.T) {
		gasCoin := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			ChangedObject_NONE)
		gasCoin.InputVersion = proto.Uint64(41)
		gasCoin.InputDigest = proto.String(inDigestStr)
		gasCoin.InputOwner = addressOwner(longOwnerAddress)
		gasCoin.OutputVersion = proto.Uint64(42)
		gasCoin.OutputDigest = proto.String(outDigestStr)
		gasCoin.OutputOwner = addressOwner(longOwnerAddress)
		gasCoin.ObjectType = proto.String(longSuiPackage + "::coin::Coin<" + longSuiType + ">")

		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{gasCoin},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Len(t, got, 1)
		mutated := got[0].Data.Mutated
		require.NotNil(t, mutated)
		require.Equal(t, sender, mutated.Sender)
		require.Equal(t, sender, *mutated.Owner.AddressOwner)
		require.Equal(t, "0x2::coin::Coin<0x2::sui::SUI>", mutated.ObjectType)
		require.Equal(t, mustAddress(t, longObjectID), mutated.ObjectId)
		require.EqualValues(t, 42, mutated.Version.Uint64())
		require.EqualValues(t, 41, mutated.PreviousVersion.Uint64())
		require.Equal(t, outDigest, mutated.Digest)

		// Pinned against the JSON a deployed node returns for this same change:
		// the constructed value must equal what decoding that literal produces.
		var fromReference lib.TagJson[types.ObjectChange]
		require.NoError(t, json.Unmarshal([]byte(`{
			"type": "mutated",
			"sender": "`+longOwnerAddress+`",
			"owner": {"AddressOwner": "`+longOwnerAddress+`"},
			"objectType": "0x2::coin::Coin<0x2::sui::SUI>",
			"objectId": "`+longObjectID+`",
			"version": "42",
			"previousVersion": "41",
			"digest": "`+outDigestStr+`"
		}`), &fromReference))
		require.Equal(t, fromReference, got[0])
	})

	t.Run("owner change still renders as mutated", func(t *testing.T) {
		// the legacy "transferred" variant is never emitted.
		transferred := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			ChangedObject_NONE)
		transferred.InputVersion = proto.Uint64(7)
		transferred.InputOwner = addressOwner(longOwnerAddress)
		transferred.OutputVersion = proto.Uint64(8)
		transferred.OutputDigest = proto.String(outDigestStr)
		transferred.OutputOwner = addressOwner(testRecipientAddress)
		transferred.ObjectType = proto.String(longUsdcType)

		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{transferred},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Len(t, got, 1)
		mutated := got[0].Data.Mutated
		require.NotNil(t, mutated)
		require.Equal(t, sender, mutated.Sender)
		require.Equal(t, mustAddress(t, testRecipientAddress), *mutated.Owner.AddressOwner)
		require.Equal(t, shortUsdcType, mutated.ObjectType)
	})

	t.Run("created object", func(t *testing.T) {
		created := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			ChangedObject_CREATED)
		created.OutputVersion = proto.Uint64(9)
		created.OutputDigest = proto.String(outDigestStr)
		created.OutputOwner = &Owner{Kind: Owner_SHARED.Enum(), Version: proto.Uint64(9)}
		// gRPC prints generic parameters without the space the internal shape uses.
		created.ObjectType = proto.String(longSuiPackage + "::dynamic_field::Field<u64," + longUsdcType + ">")

		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{created},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Len(t, got, 1)
		change := got[0].Data.Created
		require.NotNil(t, change)
		require.Equal(t, sender, change.Sender)
		require.EqualValues(t, 9, *change.Owner.Shared.InitialSharedVersion)
		require.Equal(t, "0x2::dynamic_field::Field<u64, "+shortUsdcType+">", change.ObjectType)
		require.EqualValues(t, 9, change.Version.Uint64())
		require.Equal(t, outDigest, change.Digest)
	})

	t.Run("immutable owner renders as the bare Immutable string", func(t *testing.T) {
		frozen := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			ChangedObject_CREATED)
		frozen.OutputVersion = proto.Uint64(1)
		frozen.OutputDigest = proto.String(outDigestStr)
		frozen.OutputOwner = &Owner{Kind: Owner_IMMUTABLE.Enum()}
		frozen.ObjectType = proto.String(longUsdcType)

		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{frozen},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Len(t, got, 1)
		encoded, err := json.Marshal(got[0].Data.Created.Owner)
		require.NoError(t, err)
		require.Equal(t, `"Immutable"`, string(encoded))
	})

	t.Run("package publish", func(t *testing.T) {
		published := changedObject(longSuiPackage,
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE,
			ChangedObject_CREATED)
		published.OutputVersion = proto.Uint64(1)
		published.OutputDigest = proto.String(outDigestStr)
		published.ObjectType = proto.String("package")

		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{published},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Len(t, got, 1)
		change := got[0].Data.Published
		require.NotNil(t, change)
		require.Equal(t, mustAddress(t, longSuiPackage), change.PackageId)
		require.EqualValues(t, 1, change.Version.Uint64())
		require.Equal(t, outDigest, change.Digest)
		require.Empty(t, change.Nodules)
	})

	t.Run("system package upgrade in place is excluded", func(t *testing.T) {
		upgrade := changedObject(longSuiPackage,
			ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE,
			ChangedObject_NONE)
		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{upgrade},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Empty(t, got)
	})

	t.Run("non-writes are excluded", func(t *testing.T) {
		deleted := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_DELETED)
		deleted.InputVersion = proto.Uint64(3)
		deleted.InputOwner = addressOwner(longOwnerAddress)
		deleted.ObjectType = proto.String(longUsdcType)
		wrapped := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_NONE)
		unwrapped := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			ChangedObject_NONE)
		createdThenWrapped := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_CREATED)
		accumulator := changedObject(longObjectID,
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE,
			ChangedObject_NONE)
		accumulator.AccumulatorWrite = &AccumulatorWrite{
			Address:         proto.String(longOwnerAddress),
			AccumulatorType: proto.String(longSuiType),
			Operation:       AccumulatorWrite_MERGE.Enum(),
			IntegerValue:    proto.Uint64(100),
		}

		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{deleted, wrapped, unwrapped, createdThenWrapped, accumulator},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, errs)
		require.Empty(t, got)
	})

	t.Run("unparseable entries are reported and skipped", func(t *testing.T) {
		bad := changedObject("not-an-id-zz",
			ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			ChangedObject_CREATED)
		bad.OutputOwner = addressOwner(longOwnerAddress)
		got, errs := (&TransactionEffects{
			ChangedObjects: []*ChangedObject{bad},
		}).ToInternalObjectChanges(sender)
		require.Empty(t, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "not-an-id-zz")
	})
}
