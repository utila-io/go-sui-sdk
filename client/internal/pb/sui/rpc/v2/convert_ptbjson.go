package rpcv2

import (
	"encoding/binary"
	"strconv"

	"github.com/utila-io/go-sui-sdk/move_types"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

// Leaf rendering for the parsed input shape: a decoded PTB's inputs, commands
// and arguments become generic maps, since the internal types carry them
// untyped. Nothing here touches a protobuf message.

// callArgJSON renders one PTB input as a call argument.
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

// pureJSON renders a pure input: typed when inferred, otherwise a null
// valueType with the raw bytes as a number array.
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

// commandJSON renders one PTB command: externally tagged, structs for
// MoveCall, positional arrays for the tuple variants.
func commandJSON(command sui_types.Command) map[string]interface{} {
	switch {
	case command.MoveCall != nil:
		call := map[string]interface{}{
			"package":   command.MoveCall.Package.String(),
			"module":    string(command.MoveCall.Module),
			"function":  string(command.MoveCall.Function),
			"arguments": argumentsJSON(command.MoveCall.Arguments),
		}
		// type_arguments (snake_case here) is omitted when empty.
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

// argumentJSON renders a PTB argument: "GasCoin" as a bare string, the other
// variants externally tagged.
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
// how MoveCall type arguments are printed.
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
		out := normalizeTypeString(tag.Struct.Address.String()) +
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
