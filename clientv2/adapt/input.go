package adapt

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/fardream/go-bcs/bcs"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// senderSignedDataPrefix is the fixed head of a single-transaction BCS
// SenderSignedData: vector length 1, then the TransactionData signing intent
// (scope=TransactionData, version=V0, app=Sui).
var senderSignedDataPrefix = []byte{1, 0, 0, 0}

// RawSenderSignedData wraps bare BCS TransactionData bytes and the user
// signatures into the BCS SenderSignedData envelope JSON-RPC returns under
// showRawInput: prefix, TransactionData, signatures as a vector of byte vectors.
func RawSenderSignedData(txData []byte, signatures []*pb.UserSignature) []byte {
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

// DecodeTransactionData BCS-decodes bare TransactionData bytes. System
// transaction kinds are not in sui_types' enum and fail to decode.
func DecodeTransactionData(txData []byte) (*sui_types.TransactionData, error) {
	var data sui_types.TransactionData
	if _, err := bcs.Unmarshal(txData, &data); err != nil {
		return nil, fmt.Errorf("decode TransactionData: %w", err)
	}
	if data.V1 == nil {
		return nil, errors.New("decode TransactionData: unknown version")
	}
	return &data, nil
}

// TransactionBlock renders decoded TransactionData into the parsed JSON-RPC
// showInput shape. Pure input value types are only inferred from built-in
// command usage (see pureValueTypes); unresolved pures render as raw bytes
// with a null valueType, like JSON-RPC's own unresolvable pures.
func TransactionBlock(data *sui_types.TransactionData, signatures []*pb.UserSignature) *types.SuiTransactionBlock {
	gasData := types.SuiGasData{
		Payment: make([]types.SuiObjectRef, 0, len(data.V1.GasData.Payment)),
		Owner:   data.V1.GasData.Owner.String(),
		Price:   types.NewSafeSuiBigInt(data.V1.GasData.Price),
		Budget:  types.NewSafeSuiBigInt(data.V1.GasData.Budget),
	}
	for _, ref := range data.V1.GasData.Payment {
		if ref == nil {
			continue
		}
		gasData.Payment = append(gasData.Payment, types.SuiObjectRef{
			ObjectId: ref.ObjectId.String(),
			Version:  ref.Version,
			Digest:   ref.Digest,
		})
	}
	block := &types.SuiTransactionBlock{
		Data: lib.TagJson[types.SuiTransactionBlockData]{Data: types.SuiTransactionBlockData{
			V1: &types.SuiTransactionBlockDataV1{
				Transaction: transactionBlockKind(data.V1.Kind),
				Sender:      data.V1.Sender,
				GasData:     gasData,
			},
		}},
		TxSignatures: make([]string, 0, len(signatures)),
	}
	for _, signature := range signatures {
		block.TxSignatures = append(block.TxSignatures,
			base64.StdEncoding.EncodeToString(signature.GetBcs().GetValue()))
	}
	return block
}

// transactionBlockKind maps a decoded TransactionKind to the JSON-RPC kind.
// Genesis is not mapped (only ever appears in checkpoint 0), leaving it zero.
func transactionBlockKind(kind sui_types.TransactionKind) types.SuiTransactionBlockKind {
	out := types.TransactionBlockKind{}
	switch {
	case kind.ProgrammableTransaction != nil:
		out.ProgrammableTransaction = programmableTransactionBlock(kind.ProgrammableTransaction)
	case kind.ChangeEpoch != nil:
		out.ChangeEpoch = &types.SuiChangeEpoch{
			Epoch:                 types.NewSafeSuiBigInt(kind.ChangeEpoch.Epoch),
			StorageCharge:         kind.ChangeEpoch.StorageCharge,
			ComputationCharge:     kind.ChangeEpoch.ComputationCharge,
			StorageRebate:         kind.ChangeEpoch.StorageRebate,
			EpochStartTimestampMs: kind.ChangeEpoch.EpochStartTimestampMs,
		}
	case kind.ConsensusCommitPrologue != nil:
		out.ConsensusCommitPrologue = &types.SuiConsensusCommitPrologue{
			Epoch:             kind.ConsensusCommitPrologue.Epoch,
			Round:             kind.ConsensusCommitPrologue.Round,
			CommitTimestampMs: kind.ConsensusCommitPrologue.CommitTimestampMs,
		}
	}
	return types.SuiTransactionBlockKind{Data: out}
}

// programmableTransactionBlock renders a PTB's inputs and commands into the
// generic maps JSON-RPC serializes them as.
func programmableTransactionBlock(pt *sui_types.ProgrammableTransaction) *types.SuiProgrammableTransactionBlock {
	valueTypes := pureValueTypes(pt)
	block := &types.SuiProgrammableTransactionBlock{
		Inputs:   make([]interface{}, 0, len(pt.Inputs)),
		Commands: make([]interface{}, 0, len(pt.Commands)),
	}
	for i, input := range pt.Inputs {
		block.Inputs = append(block.Inputs, callArgJSON(input, valueTypes[i]))
	}
	for _, command := range pt.Commands {
		block.Commands = append(block.Commands, commandJSON(command))
	}
	return block
}

// pureValueTypes infers pure input value types from built-in command usage:
// TransferObjects recipients are addresses, SplitCoins amounts are u64.
// MoveCall types would need on-chain function signatures and are not resolved.
func pureValueTypes(pt *sui_types.ProgrammableTransaction) map[int]string {
	valueTypes := make(map[int]string)
	for _, command := range pt.Commands {
		switch {
		case command.TransferObjects != nil:
			if input := command.TransferObjects.Argument.Input; input != nil {
				valueTypes[int(*input)] = "address"
			}
		case command.SplitCoins != nil:
			for _, amount := range command.SplitCoins.Arguments {
				if amount.Input != nil {
					valueTypes[int(*amount.Input)] = "u64"
				}
			}
		}
	}
	return valueTypes
}

// callArgJSON renders one PTB input like JSON-RPC's SuiCallArg.
func callArgJSON(arg sui_types.CallArg, valueType string) map[string]interface{} {
	switch {
	case arg.Pure != nil:
		return pureJSON(*arg.Pure, valueType)
	case arg.Object != nil && arg.Object.ImmOrOwnedObject != nil:
		ref := arg.Object.ImmOrOwnedObject
		return map[string]interface{}{
			"type":       "object",
			"objectType": "immOrOwnedObject",
			"objectId":   ref.ObjectId.String(),
			"version":    strconv.FormatUint(ref.Version, 10),
			"digest":     ref.Digest.String(),
		}
	case arg.Object != nil && arg.Object.Receiving != nil:
		ref := arg.Object.Receiving
		return map[string]interface{}{
			"type":       "object",
			"objectType": "receiving",
			"objectId":   ref.ObjectId.String(),
			"version":    strconv.FormatUint(ref.Version, 10),
			"digest":     ref.Digest.String(),
		}
	case arg.Object != nil && arg.Object.SharedObject != nil:
		shared := arg.Object.SharedObject
		return map[string]interface{}{
			"type":                 "object",
			"objectType":           "sharedObject",
			"objectId":             shared.Id.String(),
			"initialSharedVersion": strconv.FormatUint(shared.InitialSharedVersion, 10),
			"mutable":              shared.Mutable,
		}
	case arg.FundsWithdrawal != nil:
		return map[string]interface{}{"type": "fundsWithdrawal"}
	default:
		return map[string]interface{}{}
	}
}

// pureJSON renders a pure input like JSON-RPC's SuiPureValue: typed when
// inferred, otherwise a null valueType with the raw bytes as a number array.
func pureJSON(pureBytes []byte, valueType string) map[string]interface{} {
	out := map[string]interface{}{"type": "pure"}
	switch {
	case valueType == "u64" && len(pureBytes) == 8:
		out["valueType"] = "u64"
		out["value"] = strconv.FormatUint(binary.LittleEndian.Uint64(pureBytes), 10)
	case valueType == "address" && len(pureBytes) == len(sui_types.SuiAddress{}):
		var addr sui_types.SuiAddress
		copy(addr[:], pureBytes)
		out["valueType"] = "address"
		out["value"] = addr.String()
	default:
		values := make([]int, len(pureBytes))
		for i, b := range pureBytes {
			values[i] = int(b)
		}
		out["valueType"] = nil
		out["value"] = values
	}
	return out
}

// commandJSON renders one PTB command like JSON-RPC's SuiCommand: externally
// tagged, structs for MoveCall, positional arrays for the tuple variants.
func commandJSON(command sui_types.Command) map[string]interface{} {
	switch {
	case command.MoveCall != nil:
		call := map[string]interface{}{
			"package":   command.MoveCall.Package.String(),
			"module":    string(command.MoveCall.Module),
			"function":  string(command.MoveCall.Function),
			"arguments": argumentsJSON(command.MoveCall.Arguments),
		}
		// JSON-RPC omits type_arguments (snake_case there) when empty.
		if len(command.MoveCall.TypeArguments) > 0 {
			typeArgs := make([]string, len(command.MoveCall.TypeArguments))
			for i, tag := range command.MoveCall.TypeArguments {
				typeArgs[i] = typeTagString(tag)
			}
			call["type_arguments"] = typeArgs
		}
		return map[string]interface{}{"MoveCall": call}
	case command.TransferObjects != nil:
		return map[string]interface{}{"TransferObjects": []interface{}{
			argumentsJSON(command.TransferObjects.Arguments),
			argumentJSON(command.TransferObjects.Argument),
		}}
	case command.SplitCoins != nil:
		return map[string]interface{}{"SplitCoins": []interface{}{
			argumentJSON(command.SplitCoins.Argument),
			argumentsJSON(command.SplitCoins.Arguments),
		}}
	case command.MergeCoins != nil:
		return map[string]interface{}{"MergeCoins": []interface{}{
			argumentJSON(command.MergeCoins.Argument),
			argumentsJSON(command.MergeCoins.Arguments),
		}}
	case command.Publish != nil:
		return map[string]interface{}{"Publish": objectIDStrings(command.Publish.Objects)}
	case command.MakeMoveVec != nil:
		var typeTag interface{}
		if command.MakeMoveVec.TypeTag != nil {
			typeTag = typeTagString(*command.MakeMoveVec.TypeTag)
		}
		return map[string]interface{}{"MakeMoveVec": []interface{}{
			typeTag,
			argumentsJSON(command.MakeMoveVec.Arguments),
		}}
	case command.Upgrade != nil:
		return map[string]interface{}{"Upgrade": []interface{}{
			objectIDStrings(command.Upgrade.Objects),
			command.Upgrade.ObjectID.String(),
			argumentJSON(command.Upgrade.Argument),
		}}
	default:
		return map[string]interface{}{}
	}
}

func objectIDStrings(ids []sui_types.ObjectID) []interface{} {
	out := make([]interface{}, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func argumentsJSON(args []sui_types.Argument) []interface{} {
	out := make([]interface{}, len(args))
	for i, arg := range args {
		out[i] = argumentJSON(arg)
	}
	return out
}

// argumentJSON renders a PTB argument like JSON-RPC's SuiArgument: "GasCoin"
// as a bare string, the other variants externally tagged.
func argumentJSON(arg sui_types.Argument) interface{} {
	switch {
	case arg.GasCoin != nil:
		return "GasCoin"
	case arg.Input != nil:
		return map[string]interface{}{"Input": *arg.Input}
	case arg.Result != nil:
		return map[string]interface{}{"Result": *arg.Result}
	case arg.NestedResult != nil:
		return map[string]interface{}{"NestedResult": []interface{}{
			arg.NestedResult.Result1,
			arg.NestedResult.Result2,
		}}
	default:
		return nil
	}
}

// typeTagString renders a Move type tag with short-form addresses, matching
// how JSON-RPC prints MoveCall type arguments.
func typeTagString(tag move_types.TypeTag) string {
	switch {
	case tag.Bool != nil:
		return "bool"
	case tag.U8 != nil:
		return "u8"
	case tag.U16 != nil:
		return "u16"
	case tag.U32 != nil:
		return "u32"
	case tag.U64 != nil:
		return "u64"
	case tag.U128 != nil:
		return "u128"
	case tag.U256 != nil:
		return "u256"
	case tag.Address != nil:
		return "address"
	case tag.Signer != nil:
		return "signer"
	case tag.Vector != nil:
		return "vector<" + typeTagString(*tag.Vector) + ">"
	case tag.Struct != nil:
		out := NormalizeTypeString(tag.Struct.Address.String()) +
			"::" + string(tag.Struct.Module) + "::" + string(tag.Struct.Name)
		if len(tag.Struct.TypeParams) > 0 {
			out += "<"
			for i, param := range tag.Struct.TypeParams {
				if i > 0 {
					out += ", "
				}
				out += typeTagString(param)
			}
			out += ">"
		}
		return out
	default:
		return ""
	}
}
