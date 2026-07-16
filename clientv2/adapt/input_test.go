package adapt

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/fardream/go-bcs/bcs"
	"github.com/stretchr/testify/require"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

// testRecipientAddress is the transfer recipient in the test transaction.
const testRecipientAddress = "0x1111111111111111111111111111111111111111111111111111111111111111"

// testTransactionData builds a small but representative TransactionData: a
// PTB with pure, shared-object, owned-object and receiving inputs feeding
// SplitCoins, TransferObjects and a MoveCall.
func testTransactionData(t *testing.T) sui_types.TransactionData {
	t.Helper()
	sender := mustAddress(t, longOwnerAddress)
	recipient := mustAddress(t, testRecipientAddress)

	amountBytes, err := bcs.Marshal(uint64(1000))
	require.NoError(t, err)
	recipientBytes, err := bcs.Marshal(recipient)
	require.NoError(t, err)
	unresolvedBytes := []byte{0x01, 0x02, 0x03}

	pt := sui_types.ProgrammableTransaction{
		Inputs: []sui_types.CallArg{
			{Pure: &amountBytes},    // 0: SplitCoins amount, inferred u64
			{Pure: &recipientBytes}, // 1: TransferObjects recipient, inferred address
			{Object: &sui_types.ObjectArg{SharedObject: &struct {
				Id                   sui_types.ObjectID
				InitialSharedVersion sui_types.SequenceNumber
				Mutable              bool
			}{
				Id:                   mustAddress(t, longSuiPackage),
				InitialSharedVersion: 7,
				Mutable:              true,
			}}},
			{Object: &sui_types.ObjectArg{ImmOrOwnedObject: &sui_types.ObjectRef{
				ObjectId: mustAddress(t, longObjectID),
				Version:  42,
				Digest:   lib.Base58(bytes.Repeat([]byte{0x21}, 32)),
			}}},
			{Pure: &unresolvedBytes}, // 4: only used by the MoveCall, not resolved
			{Object: &sui_types.ObjectArg{Receiving: &sui_types.ObjectRef{
				ObjectId: mustAddress(t, testRecipientAddress),
				Version:  43,
				Digest:   lib.Base58(bytes.Repeat([]byte{0x22}, 32)),
			}}},
		},
		Commands: []sui_types.Command{
			{SplitCoins: &sui_types.SplitCoinsCommand{
				Argument:  sui_types.Argument{GasCoin: &lib.EmptyEnum{}},
				Arguments: []sui_types.Argument{{Input: ptr(uint16(0))}},
			}},
			{TransferObjects: &sui_types.TransferObjectsCommand{
				Arguments: []sui_types.Argument{{Result: ptr(uint16(0))}},
				Argument:  sui_types.Argument{Input: ptr(uint16(1))},
			}},
			{MoveCall: &sui_types.ProgrammableMoveCall{
				Package:  mustAddress(t, longSuiPackage),
				Module:   "coin",
				Function: "join",
				TypeArguments: []move_types.TypeTag{{Struct: &move_types.StructTag{
					Address: mustAddress(t, longSuiPackage),
					Module:  "sui",
					Name:    "SUI",
				}}},
				Arguments: []sui_types.Argument{
					{Input: ptr(uint16(2))},
					{Input: ptr(uint16(3))},
					{Input: ptr(uint16(4))},
					{Input: ptr(uint16(5))},
					{NestedResult: &sui_types.NestedResultArgument{Result1: 0, Result2: 0}},
				},
			}},
		},
	}
	return sui_types.TransactionData{V1: &sui_types.TransactionDataV1{
		Kind:   sui_types.TransactionKind{ProgrammableTransaction: &pt},
		Sender: sender,
		GasData: sui_types.GasData{
			Payment: []*sui_types.ObjectRef{{
				ObjectId: mustAddress(t, longObjectID),
				Version:  5,
				Digest:   lib.Base58(bytes.Repeat([]byte{0x42}, 32)),
			}},
			Owner:  sender,
			Price:  1000,
			Budget: 5_000_000,
		},
		Expiration: sui_types.TransactionExpiration{None: &lib.EmptyEnum{}},
	}}
}

func testTransactionDataBytes(t *testing.T) []byte {
	t.Helper()
	txData, err := bcs.Marshal(testTransactionData(t))
	require.NoError(t, err)
	return txData
}

func TestRawSenderSignedData(t *testing.T) {
	t.Run("no signatures", func(t *testing.T) {
		got := RawSenderSignedData([]byte{0xaa, 0xbb, 0xcc}, nil)
		require.Equal(t, []byte{
			0x01,             // vector<SenderSignedTransaction> of length 1
			0x00, 0x00, 0x00, // intent: TransactionData, V0, Sui
			0xaa, 0xbb, 0xcc, // TransactionData bytes
			0x00, // empty signature vector
		}, got)
	})

	t.Run("two signatures with a multi-byte length prefix", func(t *testing.T) {
		longSig := bytes.Repeat([]byte{0x5a}, 130)
		got := RawSenderSignedData([]byte{0xaa, 0xbb, 0xcc}, []*pb.UserSignature{
			{Bcs: &pb.Bcs{Value: []byte{0x01, 0x02, 0x03}}},
			{Bcs: &pb.Bcs{Value: longSig}},
		})
		want := []byte{
			0x01,             // vector<SenderSignedTransaction> of length 1
			0x00, 0x00, 0x00, // intent
			0xaa, 0xbb, 0xcc, // TransactionData bytes
			0x02,                   // two signatures
			0x03, 0x01, 0x02, 0x03, // first signature, length-prefixed
			0x82, 0x01, // 130 as ULEB128
		}
		want = append(want, longSig...)
		require.Equal(t, want, got)
	})

	t.Run("round-trips through the SDK's SenderSignedData decoding", func(t *testing.T) {
		// The synthesized envelope must decode as vector<IntentMessage ++ sigs>,
		// i.e. exactly what JSON-RPC's rawTransaction decodes as.
		txData := testTransactionDataBytes(t)
		raw := RawSenderSignedData(txData, []*pb.UserSignature{
			{Bcs: &pb.Bcs{Value: bytes.Repeat([]byte{0x11}, 97)}},
		})
		require.Equal(t, []byte{1, 0, 0, 0}, raw[:4])
		require.Equal(t, txData, raw[4:4+len(txData)])

		var decoded []struct {
			Intent  [3]byte
			TxData  sui_types.TransactionData
			SigData [][]byte
		}
		_, err := bcs.Unmarshal(raw, &decoded)
		require.NoError(t, err)
		require.Len(t, decoded, 1)
		require.Equal(t, mustAddress(t, longOwnerAddress), decoded[0].TxData.V1.Sender)
		require.Len(t, decoded[0].SigData, 1)
		require.Equal(t, bytes.Repeat([]byte{0x11}, 97), decoded[0].SigData[0])
	})
}

func TestTransactionBlock(t *testing.T) {
	signatures := []*pb.UserSignature{
		{Bcs: &pb.Bcs{Value: []byte{0x00, 0x01, 0x02}}},
		{Bcs: &pb.Bcs{Value: []byte{0x03, 0x04}}},
	}

	t.Run("programmable transaction", func(t *testing.T) {
		data, err := DecodeTransactionData(testTransactionDataBytes(t))
		require.NoError(t, err)
		block := TransactionBlock(data, signatures)

		v1 := block.Data.Data.V1
		require.NotNil(t, v1)
		require.Equal(t, mustAddress(t, longOwnerAddress), v1.Sender)

		require.Equal(t, longOwnerAddress, v1.GasData.Owner)
		require.EqualValues(t, 1000, v1.GasData.Price.Uint64())
		require.EqualValues(t, 5_000_000, v1.GasData.Budget.Uint64())
		require.Len(t, v1.GasData.Payment, 1)
		require.Equal(t, longObjectID, v1.GasData.Payment[0].ObjectId)
		require.EqualValues(t, 5, v1.GasData.Payment[0].Version)
		require.Equal(t, lib.Base58(bytes.Repeat([]byte{0x42}, 32)), v1.GasData.Payment[0].Digest)

		require.Equal(t, []string{
			base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0x02}),
			base64.StdEncoding.EncodeToString([]byte{0x03, 0x04}),
		}, block.TxSignatures)

		ptb := v1.Transaction.Data.ProgrammableTransaction
		require.NotNil(t, ptb)
		require.Equal(t, []interface{}{
			map[string]interface{}{"type": "pure", "valueType": "u64", "value": "1000"},
			map[string]interface{}{"type": "pure", "valueType": "address", "value": testRecipientAddress},
			map[string]interface{}{
				"type":                 "object",
				"objectType":           "sharedObject",
				"objectId":             longSuiPackage,
				"initialSharedVersion": "7",
				"mutable":              true,
			},
			map[string]interface{}{
				"type":       "object",
				"objectType": "immOrOwnedObject",
				"objectId":   longObjectID,
				"version":    "42",
				"digest":     lib.Base58(bytes.Repeat([]byte{0x21}, 32)).String(),
			},
			map[string]interface{}{"type": "pure", "valueType": nil, "value": []int{1, 2, 3}},
			map[string]interface{}{
				"type":       "object",
				"objectType": "receiving",
				"objectId":   testRecipientAddress,
				"version":    "43",
				"digest":     lib.Base58(bytes.Repeat([]byte{0x22}, 32)).String(),
			},
		}, ptb.Inputs)

		require.Equal(t, []interface{}{
			map[string]interface{}{"SplitCoins": []interface{}{
				"GasCoin",
				[]interface{}{map[string]interface{}{"Input": uint16(0)}},
			}},
			map[string]interface{}{"TransferObjects": []interface{}{
				[]interface{}{map[string]interface{}{"Result": uint16(0)}},
				map[string]interface{}{"Input": uint16(1)},
			}},
			map[string]interface{}{"MoveCall": map[string]interface{}{
				"package":        longSuiPackage,
				"module":         "coin",
				"function":       "join",
				"type_arguments": []string{shortSuiType},
				"arguments": []interface{}{
					map[string]interface{}{"Input": uint16(2)},
					map[string]interface{}{"Input": uint16(3)},
					map[string]interface{}{"Input": uint16(4)},
					map[string]interface{}{"Input": uint16(5)},
					map[string]interface{}{"NestedResult": []interface{}{uint16(0), uint16(0)}},
				},
			}},
		}, ptb.Commands)
	})

	t.Run("change epoch system transaction", func(t *testing.T) {
		txData, err := bcs.Marshal(sui_types.TransactionData{V1: &sui_types.TransactionDataV1{
			Kind: sui_types.TransactionKind{ChangeEpoch: &sui_types.ChangeEpoch{
				Epoch:                 33,
				StorageCharge:         100,
				ComputationCharge:     200,
				StorageRebate:         50,
				EpochStartTimestampMs: 1700000000000,
			}},
			Sender:     sui_types.SuiAddress{},
			GasData:    sui_types.GasData{Payment: []*sui_types.ObjectRef{}, Price: 1},
			Expiration: sui_types.TransactionExpiration{None: &lib.EmptyEnum{}},
		}})
		require.NoError(t, err)

		data, err := DecodeTransactionData(txData)
		require.NoError(t, err)
		block := TransactionBlock(data, nil)
		kind := block.Data.Data.V1.Transaction.Data
		require.Nil(t, kind.ProgrammableTransaction)
		require.NotNil(t, kind.ChangeEpoch)
		require.EqualValues(t, 33, kind.ChangeEpoch.Epoch.Uint64())
		require.EqualValues(t, 100, kind.ChangeEpoch.StorageCharge)
		require.EqualValues(t, 200, kind.ChangeEpoch.ComputationCharge)
		require.EqualValues(t, 50, kind.ChangeEpoch.StorageRebate)
		require.EqualValues(t, 1700000000000, kind.ChangeEpoch.EpochStartTimestampMs)
		require.Empty(t, block.TxSignatures)
	})

	t.Run("consensus commit prologue system transaction", func(t *testing.T) {
		txData, err := bcs.Marshal(sui_types.TransactionData{V1: &sui_types.TransactionDataV1{
			Kind: sui_types.TransactionKind{ConsensusCommitPrologue: &sui_types.ConsensusCommitPrologue{
				Epoch:             12,
				Round:             34,
				CommitTimestampMs: 1700000000001,
			}},
			GasData:    sui_types.GasData{Payment: []*sui_types.ObjectRef{}, Price: 1},
			Expiration: sui_types.TransactionExpiration{None: &lib.EmptyEnum{}},
		}})
		require.NoError(t, err)

		data, err := DecodeTransactionData(txData)
		require.NoError(t, err)
		block := TransactionBlock(data, nil)
		kind := block.Data.Data.V1.Transaction.Data
		require.NotNil(t, kind.ConsensusCommitPrologue)
		require.EqualValues(t, 12, kind.ConsensusCommitPrologue.Epoch)
		require.EqualValues(t, 34, kind.ConsensusCommitPrologue.Round)
		require.EqualValues(t, 1700000000001, kind.ConsensusCommitPrologue.CommitTimestampMs)
	})

	t.Run("garbage bytes fail to decode", func(t *testing.T) {
		_, err := DecodeTransactionData([]byte{0xff, 0xee})
		require.Error(t, err)
	})
}

func TestTypeTagString(t *testing.T) {
	u64Tag := move_types.TypeTag{U64: &lib.EmptyEnum{}}
	cases := []struct {
		name string
		in   move_types.TypeTag
		want string
	}{
		{name: "u64", in: u64Tag, want: "u64"},
		{name: "vector of u64", in: move_types.TypeTag{Vector: &u64Tag}, want: "vector<u64>"},
		{name: "address", in: move_types.TypeTag{Address: &lib.EmptyEnum{}}, want: "address"},
		{
			name: "nested struct shortens addresses",
			in: move_types.TypeTag{Struct: &move_types.StructTag{
				Address: mustAddress(t, longSuiPackage),
				Module:  "coin",
				Name:    "Coin",
				TypeParams: []move_types.TypeTag{{Struct: &move_types.StructTag{
					Address: mustAddress(t, longSuiPackage),
					Module:  "sui",
					Name:    "SUI",
				}}},
			}},
			want: "0x2::coin::Coin<0x2::sui::SUI>",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, typeTagString(c.in))
		})
	}
}
