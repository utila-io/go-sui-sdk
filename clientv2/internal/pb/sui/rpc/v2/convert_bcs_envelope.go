package rpcv2

import (
	"encoding/binary"

	"github.com/utila-io/go-sui-sdk/sui_types"
)

// BCS byte assembly that has no protobuf message to hang off: the wire carries
// bare TransactionData, while callers expect the signed envelope around it.

// senderSignedDataPrefix is the fixed head of a single-transaction BCS
// SenderSignedData: vector length 1, then the TransactionData signing intent
// (scope=TransactionData, version=V0, app=Sui).
var senderSignedDataPrefix = []byte{1, 0, 0, 0}

// rawSenderSignedData wraps bare BCS TransactionData bytes and the user
// signatures into the BCS SenderSignedData envelope returned under
// showRawInput: prefix, TransactionData, signatures as a vector of byte vectors.
func rawSenderSignedData(txData []byte, signatures []*UserSignature) []byte {
	out := make([]byte, 0, len(senderSignedDataPrefix)+len(txData)+1+len(signatures)*(2+64))
	out = append(out, senderSignedDataPrefix...)
	out = append(out, txData...)
	out = appendULEB128(out, uint64(len(signatures)))
	for _, signature := range signatures {
		sigBytes := signature.GetBcs().GetValue()
		out = appendULEB128(out, uint64(len(sigBytes)))
		out = append(out, sigBytes...)
	}
	return out
}

// ULEB128 is BCS's length-prefix encoding.
func appendULEB128(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}

// devInspectGasBudget is the gas budget baked into DevInspect transactions:
// the maximum budget dev-inspect runs with.
const devInspectGasBudget = uint64(50_000_000_000)

// DevInspectTransactionData wraps BCS TransactionKind bytes into a full BCS
// TransactionData V1 by concatenation, without decoding the kind: V1 variant,
// kind, sender, gas data (empty payment, owner=sender, price, budget), None
// expiration. The node accepts the empty gas payment with checks disabled.
func DevInspectTransactionData(sender sui_types.SuiAddress, kindBytes []byte, gasPrice uint64) []byte {
	data := make([]byte, 0, len(kindBytes)+2*len(sender)+19)
	data = append(data, 0x00) // TransactionData enum: V1
	data = append(data, kindBytes...)
	data = append(data, sender[:]...)
	data = append(data, 0x00) // GasData.Payment: empty vector
	data = append(data, sender[:]...)
	data = binary.LittleEndian.AppendUint64(data, gasPrice)
	data = binary.LittleEndian.AppendUint64(data, devInspectGasBudget)
	data = append(data, 0x00) // TransactionExpiration enum: None
	return data
}
