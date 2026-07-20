package rpcv2

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/fardream/go-bcs/bcs"
	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/sui_types"
)

func TestRawSenderSignedData(t *testing.T) {
	t.Run("no signatures", func(t *testing.T) {
		got := rawSenderSignedData([]byte{0xaa, 0xbb, 0xcc}, nil)
		require.Equal(t, []byte{
			0x01,             // vector<SenderSignedTransaction> of length 1
			0x00, 0x00, 0x00, // intent: TransactionData, V0, Sui
			0xaa, 0xbb, 0xcc, // TransactionData bytes
			0x00, // empty signature vector
		}, got)
	})

	t.Run("two signatures with a multi-byte length prefix", func(t *testing.T) {
		longSig := bytes.Repeat([]byte{0x5a}, 130)
		got := rawSenderSignedData([]byte{0xaa, 0xbb, 0xcc}, []*UserSignature{
			{Bcs: &Bcs{Value: []byte{0x01, 0x02, 0x03}}},
			{Bcs: &Bcs{Value: longSig}},
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
		// i.e. exactly what the rawTransaction field decodes as.
		txData := testTransactionDataBytes(t)
		raw := rawSenderSignedData(txData, []*UserSignature{
			{Bcs: &Bcs{Value: bytes.Repeat([]byte{0x11}, 97)}},
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

func TestDevInspectTransactionData(t *testing.T) {
	sender := mustAddress(t, longOwnerAddress)
	kindBytes := []byte{0xaa, 0xbb, 0xcc}
	gasPrice := uint64(1000)

	got := DevInspectTransactionData(sender, kindBytes, gasPrice)

	// Layout: V1 tag, kind, sender, empty gas payment vector, gas owner,
	// gas price (u64 LE), gas budget (u64 LE), expiration None.
	want := []byte{0x00}
	want = append(want, kindBytes...)
	want = append(want, sender[:]...)
	want = append(want, 0x00)
	want = append(want, sender[:]...)
	want = binary.LittleEndian.AppendUint64(want, gasPrice)
	want = binary.LittleEndian.AppendUint64(want, 50_000_000_000)
	want = append(want, 0x00)
	require.Equal(t, want, got)
	require.Len(t, got, 1+len(kindBytes)+32+1+32+8+8+1)
}
