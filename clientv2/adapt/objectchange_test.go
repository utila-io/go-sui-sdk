package adapt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/types"
)

// changedObject builds a ChangedObject with the given states; mutate the
// returned message for the per-case details.
func changedObject(id string, input pb.ChangedObject_InputObjectState, output pb.ChangedObject_OutputObjectState, op pb.ChangedObject_IdOperation) *pb.ChangedObject {
	return &pb.ChangedObject{
		ObjectId:    proto.String(id),
		InputState:  input.Enum(),
		OutputState: output.Enum(),
		IdOperation: op.Enum(),
	}
}

func addressOwner(addr string) *pb.Owner {
	return &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String(addr)}
}

func TestObjectChanges(t *testing.T) {
	txData := testTransactionDataBytes(t)
	sender := mustAddress(t, longOwnerAddress)
	outDigestStr, outDigest := testDigest(0x51)
	inDigestStr, _ := testDigest(0x52)

	t.Run("nil effects yield nothing", func(t *testing.T) {
		got, errs := ObjectChanges(txData, nil)
		require.Empty(t, errs)
		require.Nil(t, got)
	})

	t.Run("gas coin mutation is included", func(t *testing.T) {
		gasCoin := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_NONE)
		gasCoin.InputVersion = proto.Uint64(41)
		gasCoin.InputDigest = proto.String(inDigestStr)
		gasCoin.InputOwner = addressOwner(longOwnerAddress)
		gasCoin.OutputVersion = proto.Uint64(42)
		gasCoin.OutputDigest = proto.String(outDigestStr)
		gasCoin.OutputOwner = addressOwner(longOwnerAddress)
		gasCoin.ObjectType = proto.String(longSuiPackage + "::coin::Coin<" + longSuiType + ">")

		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{gasCoin},
		})
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

		// The constructed value must equal what decoding the v1 JSON-RPC
		// rendering of the same change produces.
		var fromV1 lib.TagJson[types.ObjectChange]
		require.NoError(t, json.Unmarshal([]byte(`{
			"type": "mutated",
			"sender": "`+longOwnerAddress+`",
			"owner": {"AddressOwner": "`+longOwnerAddress+`"},
			"objectType": "0x2::coin::Coin<0x2::sui::SUI>",
			"objectId": "`+longObjectID+`",
			"version": "42",
			"previousVersion": "41",
			"digest": "`+outDigestStr+`"
		}`), &fromV1))
		require.Equal(t, fromV1, got[0])
	})

	t.Run("owner change still renders as mutated", func(t *testing.T) {
		// v1 never emits the legacy "transferred" variant.
		transferred := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_NONE)
		transferred.InputVersion = proto.Uint64(7)
		transferred.InputOwner = addressOwner(longOwnerAddress)
		transferred.OutputVersion = proto.Uint64(8)
		transferred.OutputDigest = proto.String(outDigestStr)
		transferred.OutputOwner = addressOwner(testRecipientAddress)
		transferred.ObjectType = proto.String(longUsdcType)

		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{transferred},
		})
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
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_CREATED)
		created.OutputVersion = proto.Uint64(9)
		created.OutputDigest = proto.String(outDigestStr)
		created.OutputOwner = &pb.Owner{Kind: pb.Owner_SHARED.Enum(), Version: proto.Uint64(9)}
		// gRPC prints generic parameters without the space v1 uses.
		created.ObjectType = proto.String(longSuiPackage + "::dynamic_field::Field<u64," + longUsdcType + ">")

		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{created},
		})
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
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_CREATED)
		frozen.OutputVersion = proto.Uint64(1)
		frozen.OutputDigest = proto.String(outDigestStr)
		frozen.OutputOwner = &pb.Owner{Kind: pb.Owner_IMMUTABLE.Enum()}
		frozen.ObjectType = proto.String(longUsdcType)

		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{frozen},
		})
		require.Empty(t, errs)
		require.Len(t, got, 1)
		encoded, err := json.Marshal(got[0].Data.Created.Owner)
		require.NoError(t, err)
		require.Equal(t, `"Immutable"`, string(encoded))
	})

	t.Run("package publish", func(t *testing.T) {
		published := changedObject(longSuiPackage,
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE,
			pb.ChangedObject_CREATED)
		published.OutputVersion = proto.Uint64(1)
		published.OutputDigest = proto.String(outDigestStr)
		published.ObjectType = proto.String("package")

		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{published},
		})
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
			pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE,
			pb.ChangedObject_NONE)
		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{upgrade},
		})
		require.Empty(t, errs)
		require.Empty(t, got)
	})

	t.Run("non-writes are excluded", func(t *testing.T) {
		deleted := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_DELETED)
		deleted.InputVersion = proto.Uint64(3)
		deleted.InputOwner = addressOwner(longOwnerAddress)
		deleted.ObjectType = proto.String(longUsdcType)
		wrapped := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_NONE)
		unwrapped := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_NONE)
		createdThenWrapped := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_CREATED)
		accumulator := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_ACCUMULATOR_WRITE,
			pb.ChangedObject_NONE)
		accumulator.AccumulatorWrite = &pb.AccumulatorWrite{
			Address:         proto.String(longOwnerAddress),
			AccumulatorType: proto.String(longSuiType),
			Operation:       pb.AccumulatorWrite_MERGE.Enum(),
			IntegerValue:    proto.Uint64(100),
		}

		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{deleted, wrapped, unwrapped, createdThenWrapped, accumulator},
		})
		require.Empty(t, errs)
		require.Empty(t, got)
	})

	t.Run("system transaction sender is the zero address", func(t *testing.T) {
		// System transaction kinds are unknown to sui_types and fail to
		// BCS-decode; their sender is always 0x0.
		clock := changedObject(longObjectID,
			pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_NONE)
		clock.InputVersion = proto.Uint64(1)
		clock.OutputVersion = proto.Uint64(2)
		clock.OutputDigest = proto.String(outDigestStr)
		clock.OutputOwner = &pb.Owner{Kind: pb.Owner_SHARED.Enum(), Version: proto.Uint64(1)}
		clock.ObjectType = proto.String(longSuiPackage + "::clock::Clock")

		got, errs := ObjectChanges([]byte{0x00, 0x09, 0xff}, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{clock},
		})
		require.Empty(t, errs)
		require.Len(t, got, 1)
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000",
			got[0].Data.Mutated.Sender.String())
	})

	t.Run("unparseable entries are reported and skipped", func(t *testing.T) {
		bad := changedObject("not-an-id-zz",
			pb.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST,
			pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
			pb.ChangedObject_CREATED)
		bad.OutputOwner = addressOwner(longOwnerAddress)
		got, errs := ObjectChanges(txData, &pb.TransactionEffects{
			ChangedObjects: []*pb.ChangedObject{bad},
		})
		require.Empty(t, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "not-an-id-zz")
	})
}

func TestResponseObjectChanges(t *testing.T) {
	txDigestStr, _ := testDigest(0x72)
	outDigestStr, _ := testDigest(0x51)
	gasCoin := changedObject(longObjectID,
		pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS,
		pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
		pb.ChangedObject_NONE)
	gasCoin.InputVersion = proto.Uint64(41)
	gasCoin.OutputVersion = proto.Uint64(42)
	gasCoin.OutputDigest = proto.String(outDigestStr)
	gasCoin.OutputOwner = addressOwner(longOwnerAddress)
	gasCoin.ObjectType = proto.String(longSuiPackage + "::coin::Coin<" + longSuiType + ">")
	tx := func(t *testing.T) *pb.ExecutedTransaction {
		return &pb.ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: testTransactionDataBytes(t)}},
			Effects: &pb.TransactionEffects{
				Status:            &pb.ExecutionStatus{Success: proto.Bool(true)},
				TransactionDigest: proto.String(txDigestStr),
				ChangedObjects:    []*pb.ChangedObject{gasCoin},
			},
		}
	}

	t.Run("populated only when requested", func(t *testing.T) {
		got := Response(tx(t), types.SuiTransactionBlockResponseOptions{ShowEffects: true})
		require.Nil(t, got.ObjectChanges)
		require.NotNil(t, got.Effects)
	})

	t.Run("show object changes without effects", func(t *testing.T) {
		got := Response(tx(t), types.SuiTransactionBlockResponseOptions{ShowObjectChanges: true})
		require.Nil(t, got.Effects, "effects fetched for the derivation are not echoed back")
		require.Len(t, got.ObjectChanges, 1)
		require.NotNil(t, got.ObjectChanges[0].Data.Mutated)
		require.Empty(t, got.Errors)
	})
}
