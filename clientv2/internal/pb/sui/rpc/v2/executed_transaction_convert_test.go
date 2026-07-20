package rpcv2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/utila-io/go-sui-sdk/types"
)

func TestResponse(t *testing.T) {
	txDigestStr, txDigest := testDigest(0x72)
	fxDigestStr, _ := testDigest(0x73)

	// Callers convert straight off the response getter, so a node that
	// returned no transaction reaches this as a nil receiver and must yield a
	// nil response rather than panicking.
	t.Run("nil", func(t *testing.T) {
		require.Nil(t, (*ExecutedTransaction)(nil).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowEffects: true}))
	})

	t.Run("minimal", func(t *testing.T) {
		got := (&ExecutedTransaction{Digest: proto.String(txDigestStr)}).ToInternalType(types.SuiTransactionBlockResponseOptions{})
		require.Equal(t, txDigest, got.Digest)
		require.Nil(t, got.Effects)
		require.Nil(t, got.Events)
		require.Nil(t, got.BalanceChanges)
		require.Nil(t, got.TimestampMs)
		require.Nil(t, got.Checkpoint)
		require.Empty(t, got.RawTransaction)
		require.Nil(t, got.Transaction)
	})

	t.Run("full", func(t *testing.T) {
		txData := testTransactionDataBytes(t)
		signature := &UserSignature{Bcs: &Bcs{Value: []byte{0x0a, 0x0b, 0x0c}}}
		got := (&ExecutedTransaction{
			Digest: proto.String(txDigestStr),
			Transaction: &Transaction{
				Bcs: &Bcs{Value: txData},
			},
			Signatures: []*UserSignature{signature},
			Effects: &TransactionEffects{
				Status:            &ExecutionStatus{Success: proto.Bool(true)},
				TransactionDigest: proto.String(fxDigestStr),
			},
			Checkpoint: proto.Uint64(555),
			Timestamp:  timestamppb.New(time.UnixMilli(1700000000456)),
		}).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowInput: true, ShowRawInput: true, ShowEffects: true})
		require.Equal(t, txDigest, got.Digest)
		require.Equal(t, rawSenderSignedData(txData, []*UserSignature{signature}), got.RawTransaction)
		require.NotNil(t, got.Transaction)
		require.Empty(t, got.Errors)
		require.NotNil(t, got.Effects)
		require.Equal(t, types.ExecutionStatusSuccess, got.Effects.Data.V1.Status.Status)
		require.NotNil(t, got.Checkpoint)
		require.EqualValues(t, 555, got.Checkpoint.Uint64())
		require.NotNil(t, got.TimestampMs)
		require.EqualValues(t, 1700000000456, got.TimestampMs.Uint64())
	})

	t.Run("input options off leave transaction fields unset", func(t *testing.T) {
		got := (&ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &Transaction{Bcs: &Bcs{Value: testTransactionDataBytes(t)}},
		}).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowEffects: true})
		require.Nil(t, got.RawTransaction)
		require.Nil(t, got.Transaction)
	})

	t.Run("show raw input only", func(t *testing.T) {
		txData := testTransactionDataBytes(t)
		got := (&ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &Transaction{Bcs: &Bcs{Value: txData}},
		}).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowRawInput: true})
		require.Equal(t, rawSenderSignedData(txData, nil), got.RawTransaction)
		require.Nil(t, got.Transaction)
	})

	t.Run("undecodable transaction reports an error instead of failing", func(t *testing.T) {
		got := (&ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &Transaction{Bcs: &Bcs{Value: []byte{0xff, 0xff}}},
		}).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowInput: true})
		require.Nil(t, got.Transaction)
		require.NotEmpty(t, got.Errors)
	})

	t.Run("unparseable entries are reported via Errors", func(t *testing.T) {
		got := (&ExecutedTransaction{
			Digest: proto.String(txDigestStr),
			Events: &TransactionEvents{Events: []*Event{{
				PackageId: proto.String(longSuiPackage),
				Sender:    proto.String("0xzz"),
				EventType: proto.String(longSuiType),
			}}},
			BalanceChanges: []*BalanceChange{
				{
					Address:  proto.String("not-an-address-zz"),
					CoinType: proto.String(longSuiType),
					Amount:   proto.String("1"),
				},
				{
					Address:  proto.String(longOwnerAddress),
					CoinType: proto.String(longSuiType),
					Amount:   proto.String("-100"),
				},
			},
		}).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowEvents: true, ShowBalanceChanges: true})
		// The parseable balance change is still returned; the dropped event
		// and balance change are reported.
		require.Len(t, got.BalanceChanges, 1)
		require.Empty(t, got.Events)
		require.Len(t, got.Errors, 2)
	})
}

func TestResponseObjectChanges(t *testing.T) {
	txDigestStr, _ := testDigest(0x72)
	outDigestStr, _ := testDigest(0x51)
	gasCoin := changedObject(longObjectID,
		ChangedObject_INPUT_OBJECT_STATE_EXISTS,
		ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE,
		ChangedObject_NONE)
	gasCoin.InputVersion = proto.Uint64(41)
	gasCoin.OutputVersion = proto.Uint64(42)
	gasCoin.OutputDigest = proto.String(outDigestStr)
	gasCoin.OutputOwner = addressOwner(longOwnerAddress)
	gasCoin.ObjectType = proto.String(longSuiPackage + "::coin::Coin<" + longSuiType + ">")
	tx := func(t *testing.T) *ExecutedTransaction {
		return &ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &Transaction{Bcs: &Bcs{Value: testTransactionDataBytes(t)}},
			Effects: &TransactionEffects{
				Status:            &ExecutionStatus{Success: proto.Bool(true)},
				TransactionDigest: proto.String(txDigestStr),
				ChangedObjects:    []*ChangedObject{gasCoin},
			},
		}
	}

	t.Run("populated only when requested", func(t *testing.T) {
		got := tx(t).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowEffects: true})
		require.Nil(t, got.ObjectChanges)
		require.NotNil(t, got.Effects)
	})

	t.Run("show object changes without effects", func(t *testing.T) {
		got := tx(t).ToInternalType(types.SuiTransactionBlockResponseOptions{ShowObjectChanges: true})
		require.Nil(t, got.Effects, "effects fetched for the derivation are not echoed back")
		require.Len(t, got.ObjectChanges, 1)
		require.NotNil(t, got.ObjectChanges[0].Data.Mutated)
		require.Empty(t, got.Errors)
	})

	t.Run("receiving input decodes and keeps the real sender", func(t *testing.T) {
		// The test transaction's PTB contains an ObjectArg::Receiving input;
		// without the Receiving variant its BCS decode fails and the sender
		// silently falls back to 0x0.
		got := tx(t).ToInternalType(types.SuiTransactionBlockResponseOptions{
			ShowInput:         true,
			ShowObjectChanges: true,
		})
		require.Empty(t, got.Errors)
		require.NotNil(t, got.Transaction, "parsed transaction present")
		require.Len(t, got.ObjectChanges, 1)
		require.Equal(t, longOwnerAddress, got.ObjectChanges[0].Data.Mutated.Sender.String())
	})

	t.Run("undecodable system transaction sender is the zero address", func(t *testing.T) {
		// System transaction kinds are unknown to sui_types and fail to
		// BCS-decode; their sender is always 0x0.
		systemTx := tx(t)
		systemTx.Transaction.Bcs.Value = []byte{0x00, 0x09, 0xff}
		got := systemTx.ToInternalType(types.SuiTransactionBlockResponseOptions{ShowObjectChanges: true})
		require.Empty(t, got.Errors)
		require.Len(t, got.ObjectChanges, 1)
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000",
			got.ObjectChanges[0].Data.Mutated.Sender.String())
	})
}
