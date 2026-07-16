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

// ResponseReadMaskPaths maps response options to ExecutedTransaction read mask
// paths. digest, checkpoint and timestamp are always fetched (JSON-RPC always
// returns them). ShowObjectChanges is derived from the effects and the
// transaction BCS (its per-entry sender), so it fetches both.
func ResponseReadMaskPaths(options types.SuiTransactionBlockResponseOptions) []string {
	paths := []string{"digest", "checkpoint", "timestamp"}
	if options.ShowInput || options.ShowRawInput {
		paths = append(paths, "transaction.bcs", "signatures")
	} else if options.ShowObjectChanges {
		paths = append(paths, "transaction.bcs")
	}
	if options.ShowEffects || options.ShowObjectChanges {
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
// sui_getTransactionBlock response shape. Fields not covered by the request's
// read mask stay silently unset. A transaction that cannot be BCS-decoded
// reports the problem via the response's Errors field instead of failing.
func Response(tx *pb.ExecutedTransaction, options types.SuiTransactionBlockResponseOptions) *types.SuiTransactionBlockResponse {
	if tx == nil {
		return nil
	}
	response := &types.SuiTransactionBlockResponse{
		Digest: parseDigest(tx.GetDigest()),
	}
	// The transaction BCS is decoded at most once and shared between the
	// showInput and showObjectChanges paths. System transaction kinds are not
	// in sui_types' enum and fail to decode; their real sender is the zero
	// address, which is what the failed decode yields for object changes.
	var data *sui_types.TransactionData
	if txData := tx.GetTransaction().GetBcs().GetValue(); len(txData) > 0 {
		var decodeErr error
		if options.ShowInput || options.ShowObjectChanges {
			data, decodeErr = DecodeTransactionData(txData)
		}
		if options.ShowRawInput {
			response.RawTransaction = RawSenderSignedData(txData, tx.GetSignatures())
		}
		if options.ShowInput {
			if decodeErr != nil {
				response.Errors = append(response.Errors, decodeErr.Error())
			} else {
				response.Transaction = TransactionBlock(data, tx.GetSignatures())
			}
		}
	}
	// Effects arrive on the wire for ShowObjectChanges too; like JSON-RPC,
	// they are only echoed back when explicitly requested.
	var effectsErrs []error
	if options.ShowEffects {
		effects, errs := Effects(tx.GetEffects())
		effectsErrs = errs
		if effects != nil {
			response.Effects = &lib.TagJson[types.SuiTransactionBlockEffects]{Data: *effects}
		}
	}
	var changeErrs []error
	if options.ShowObjectChanges {
		var sender sui_types.SuiAddress
		if data != nil {
			sender = data.V1.Sender
		}
		response.ObjectChanges, changeErrs = ObjectChanges(sender, tx.GetEffects())
	}
	events, eventErrs := Events(tx.GetDigest(), tx.GetEvents())
	response.Events = events
	balanceChanges, balanceErrs := BalanceChanges(tx.GetBalanceChanges())
	response.BalanceChanges = balanceChanges
	for _, err := range slices.Concat(effectsErrs, changeErrs, eventErrs, balanceErrs) {
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

// Events converts proto TransactionEvents into the JSON-RPC event list; event
// sequence numbers are the event's index within the transaction. Unparseable
// events are dropped and reported in the error slice.
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
// Changes with an unparseable owner are dropped and reported in the error slice.
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
// JSON-RPC dev-inspect results shape. Bytes are []byte (base64 in JSON).
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
// from the value forms accepted by ExecuteTransactionBlock.
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
