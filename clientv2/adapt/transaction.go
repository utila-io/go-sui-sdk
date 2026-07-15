package adapt

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// ResponseReadMaskPaths maps SuiTransactionBlockResponseOptions to
// ExecutedTransaction read mask paths. digest, checkpoint and timestamp are
// always fetched since JSON-RPC always returns them. ShowObjectChanges has no
// gRPC equivalent (object changes require indexing data beyond effects) and is
// ignored; the created/mutated/deleted breakdown is available via ShowEffects.
func ResponseReadMaskPaths(options types.SuiTransactionBlockResponseOptions) []string {
	paths := []string{"digest", "checkpoint", "timestamp"}
	if options.ShowInput {
		paths = append(paths, "transaction.bcs")
	}
	if options.ShowEffects {
		paths = append(paths, "effects")
	}
	if options.ShowEvents {
		paths = append(paths, "events")
	}
	if options.ShowBalanceChanges {
		paths = append(paths, "balance_changes")
	}
	return paths
}

// PrefixPaths rebases read mask paths onto a nested message, e.g.
// "transactions." for GetCheckpoint or "transaction." for SimulateTransaction.
func PrefixPaths(prefix string, paths []string) []string {
	prefixed := make([]string, len(paths))
	for i, path := range paths {
		prefixed[i] = prefix + path
	}
	return prefixed
}

// Response converts an ExecutedTransaction into the JSON-RPC
// sui_getTransactionBlock response shape. Fields absent from the proto (not
// covered by the request's read mask) stay unset, mirroring how JSON-RPC omits
// fields not requested via options. The parsed Transaction field is not
// reconstructed; ShowInput surfaces the raw BCS TransactionData instead.
func Response(tx *pb.ExecutedTransaction) *types.SuiTransactionBlockResponse {
	response := &types.SuiTransactionBlockResponse{
		Digest:         parseDigest(tx.GetDigest()),
		RawTransaction: tx.GetTransaction().GetBcs().GetValue(),
	}
	if effects := Effects(tx.GetEffects()); effects != nil {
		response.Effects = &lib.TagJson[types.SuiTransactionBlockEffects]{Data: *effects}
	}
	response.Events = Events(tx.GetDigest(), tx.GetEvents())
	response.BalanceChanges = BalanceChanges(tx.GetBalanceChanges())
	if tx.Timestamp != nil {
		timestampMs := types.NewSafeSuiBigInt(uint64(tx.GetTimestamp().AsTime().UnixMilli()))
		response.TimestampMs = &timestampMs
	}
	if tx.Checkpoint != nil {
		checkpoint := types.NewSafeSuiBigInt[types.CheckpointSequenceNumber](tx.GetCheckpoint())
		response.Checkpoint = &checkpoint
	}
	return response
}

// Events converts proto TransactionEvents into the JSON-RPC event list.
// Event sequence numbers are the event's index within the transaction.
func Events(txDigest string, events *pb.TransactionEvents) []types.SuiEvent {
	if events == nil {
		return nil
	}
	out := make([]types.SuiEvent, 0, len(events.GetEvents()))
	for i, event := range events.GetEvents() {
		packageID, err := parseAddress(event.GetPackageId())
		if err != nil {
			continue
		}
		sender, err := parseAddress(event.GetSender())
		if err != nil {
			continue
		}
		out = append(out, types.SuiEvent{
			Id: types.EventId{
				TxDigest: parseDigest(txDigest),
				EventSeq: types.NewSafeSuiBigInt(uint64(i)),
			},
			PackageId:         packageID,
			TransactionModule: event.GetModule(),
			Sender:            sender,
			Type:              NormalizeTypeString(event.GetEventType()),
			ParsedJson:        event.GetJson().AsInterface(),
			Bcs:               lib.Base58(event.GetContents().GetValue()).String(),
		})
	}
	return out
}

// BalanceChanges converts proto balance changes into the JSON-RPC shape.
// Owner addresses stay long form; coin types are normalized to short form.
func BalanceChanges(changes []*pb.BalanceChange) []types.BalanceChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]types.BalanceChange, 0, len(changes))
	for _, change := range changes {
		addr, err := parseAddress(change.GetAddress())
		if err != nil {
			continue
		}
		out = append(out, types.BalanceChange{
			Owner: types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &addr},
			},
			CoinType: NormalizeTypeString(change.GetCoinType()),
			Amount:   change.GetAmount(),
		})
	}
	return out
}

// SignatureBytes extracts the raw serialized signature (flag || sig || pubkey)
// from the values accepted by ExecuteTransactionBlock: sui_types.Signature,
// base64 strings, lib.Base64Data or raw bytes.
func SignatureBytes(signature any) ([]byte, error) {
	switch sig := signature.(type) {
	case sui_types.Signature:
		return signatureData(sig)
	case *sui_types.Signature:
		return signatureData(*sig)
	case lib.Base64Data:
		return sig.Data(), nil
	case []byte:
		return sig, nil
	case string:
		return base64.StdEncoding.DecodeString(sig)
	default:
		return nil, fmt.Errorf("unsupported signature type %T", signature)
	}
}

func signatureData(sig sui_types.Signature) ([]byte, error) {
	switch {
	case sig.Ed25519SuiSignature != nil:
		return sig.Ed25519SuiSignature.Signature[:], nil
	case sig.Secp256k1SuiSignature != nil:
		return sig.Secp256k1SuiSignature.Signature, nil
	case sig.Secp256r1SuiSignature != nil:
		return sig.Secp256r1SuiSignature.Signature, nil
	default:
		return nil, errors.New("nil signature")
	}
}

// devInspectGasBudget is the gas budget baked into DevInspect transactions,
// matching the maximum budget JSON-RPC dev-inspect runs with.
const devInspectGasBudget = uint64(50_000_000_000)

// DevInspectTransactionData wraps BCS TransactionKind bytes into a full BCS
// TransactionData V1 by concatenation, without decoding the kind. Layout
// (sui_types.TransactionData/TransactionDataV1/GasData field order):
// V1 enum variant, kind, sender, gas data (empty payment vector, owner=sender,
// price, budget) and a None expiration. The node accepts this with checks
// disabled even though no gas coins are supplied.
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
