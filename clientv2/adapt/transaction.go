package adapt

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

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
// ShowInput and ShowRawInput both need the BCS TransactionData plus the user
// signatures: the parsed transaction surfaces them as txSignatures and the raw
// SenderSignedData envelope embeds them.
func ResponseReadMaskPaths(options types.SuiTransactionBlockResponseOptions) []string {
	paths := []string{"digest", "checkpoint", "timestamp"}
	if options.ShowInput || options.ShowRawInput {
		paths = append(paths, "transaction.bcs", "signatures")
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
// fields not requested via options. Like JSON-RPC, ShowRawInput yields the BCS
// SenderSignedData bytes (synthesized from the transaction and its signatures)
// and ShowInput the parsed transaction; a transaction that cannot be decoded
// (e.g. a system transaction kind newer than the SDK's BCS types) reports the
// problem via the response's Errors field instead of failing the call.
func Response(tx *pb.ExecutedTransaction, options types.SuiTransactionBlockResponseOptions) *types.SuiTransactionBlockResponse {
	if tx == nil {
		return nil
	}
	response := &types.SuiTransactionBlockResponse{
		Digest: parseDigest(tx.GetDigest()),
	}
	if txData := tx.GetTransaction().GetBcs().GetValue(); len(txData) > 0 {
		if options.ShowRawInput {
			response.RawTransaction = RawSenderSignedData(txData, tx.GetSignatures())
		}
		if options.ShowInput {
			block, err := TransactionBlock(txData, tx.GetSignatures())
			if err != nil {
				response.Errors = append(response.Errors, err.Error())
			} else {
				response.Transaction = block
			}
		}
	}
	effects, effectsErrs := Effects(tx.GetEffects())
	if effects != nil {
		response.Effects = &lib.TagJson[types.SuiTransactionBlockEffects]{Data: *effects}
	}
	events, eventErrs := Events(tx.GetDigest(), tx.GetEvents())
	response.Events = events
	balanceChanges, balanceErrs := BalanceChanges(tx.GetBalanceChanges())
	response.BalanceChanges = balanceChanges
	for _, err := range slices.Concat(effectsErrs, eventErrs, balanceErrs) {
		response.Errors = append(response.Errors, err.Error())
	}
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
// Events that cannot be parsed are dropped and reported in the returned error
// slice; the rest are still converted.
func Events(txDigest string, events *pb.TransactionEvents) ([]types.SuiEvent, []error) {
	if events == nil {
		return nil, nil
	}
	var errs []error
	out := make([]types.SuiEvent, 0, len(events.GetEvents()))
	for i, event := range events.GetEvents() {
		packageID, err := parseAddress(event.GetPackageId())
		if err != nil {
			errs = append(errs, fmt.Errorf("event %d: package: %w", i, err))
			continue
		}
		sender, err := parseAddress(event.GetSender())
		if err != nil {
			errs = append(errs, fmt.Errorf("event %d: sender: %w", i, err))
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
	return out, errs
}

// BalanceChanges converts proto balance changes into the JSON-RPC shape.
// Owner addresses stay long form; coin types are normalized to short form.
// Changes whose owner address cannot be parsed are dropped and reported in
// the returned error slice; the rest are still converted.
func BalanceChanges(changes []*pb.BalanceChange) ([]types.BalanceChange, []error) {
	if len(changes) == 0 {
		return nil, nil
	}
	var errs []error
	out := make([]types.BalanceChange, 0, len(changes))
	for i, change := range changes {
		addr, err := parseAddress(change.GetAddress())
		if err != nil {
			errs = append(errs, fmt.Errorf("balance change %d: owner: %w", i, err))
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
	return out, errs
}

// ExecutionResults converts SimulateTransaction per-command outputs into the
// JSON-RPC dev-inspect results shape: per command, the mutable reference
// outputs as (argument, bytes, type) triples and the return values as
// (bytes, type) pairs. Bytes are []byte (rendered as base64 by
// encoding/json) and types are normalized to short form.
func ExecutionResults(outputs []*pb.CommandResult) []types.ExecutionResultType {
	if len(outputs) == 0 {
		return nil
	}
	results := make([]types.ExecutionResultType, len(outputs))
	for i, output := range outputs {
		for _, mutated := range output.GetMutatedByRef() {
			results[i].MutableReferenceOutputs = append(results[i].MutableReferenceOutputs,
				types.MutableReferenceOutputType([]any{
					commandArgument(mutated.GetArgument()),
					mutated.GetValue().GetValue(),
					NormalizeTypeString(mutated.GetValue().GetName()),
				}))
		}
		for _, returned := range output.GetReturnValues() {
			results[i].ReturnValues = append(results[i].ReturnValues,
				types.ReturnValueType([]any{
					returned.GetValue().GetValue(),
					NormalizeTypeString(returned.GetValue().GetName()),
				}))
		}
	}
	return results
}

// commandArgument renders a proto Argument in the JSON-RPC SuiArgument shape:
// "GasCoin", {"Input": n}, {"Result": n} or {"NestedResult": [n, m]}.
func commandArgument(arg *pb.Argument) any {
	switch arg.GetKind() {
	case pb.Argument_GAS:
		return "GasCoin"
	case pb.Argument_INPUT:
		return map[string]any{"Input": arg.GetInput()}
	case pb.Argument_RESULT:
		if arg.Subresult != nil {
			return map[string]any{"NestedResult": []any{arg.GetResult(), arg.GetSubresult()}}
		}
		return map[string]any{"Result": arg.GetResult()}
	default:
		return nil
	}
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
