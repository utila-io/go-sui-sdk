package rpcv2

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/fardream/go-bcs/bcs"
	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

func TestTransactionBlock(t *testing.T) {
	signatures := []*UserSignature{
		{Bcs: &Bcs{Value: []byte{0x00, 0x01, 0x02}}},
		{Bcs: &Bcs{Value: []byte{0x03, 0x04}}},
	}

	t.Run("programmable transaction", func(t *testing.T) {
		data, err := decodeTransactionData(testTransactionDataBytes(t))
		require.NoError(t, err)
		block := toInternalTransactionBlock(data, signatures)

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

		data, err := decodeTransactionData(txData)
		require.NoError(t, err)
		block := toInternalTransactionBlock(data, nil)
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

		data, err := decodeTransactionData(txData)
		require.NoError(t, err)
		block := toInternalTransactionBlock(data, nil)
		kind := block.Data.Data.V1.Transaction.Data
		require.NotNil(t, kind.ConsensusCommitPrologue)
		require.EqualValues(t, 12, kind.ConsensusCommitPrologue.Epoch)
		require.EqualValues(t, 34, kind.ConsensusCommitPrologue.Round)
		require.EqualValues(t, 1700000000001, kind.ConsensusCommitPrologue.CommitTimestampMs)
	})

	t.Run("garbage bytes fail to decode", func(t *testing.T) {
		_, err := decodeTransactionData([]byte{0xff, 0xee})
		require.Error(t, err)
	})
}
