package rpcv2

// ToInternalType renders an Argument in the internal argument shape:
// "GasCoin", {"Input": n}, {"Result": n} or {"NestedResult": [n, m]}. The
// result is an untyped JSON value, matching how arguments are carried in
// command output.
func (x *Argument) ToInternalType() any {
	switch x.GetKind() {
	case Argument_GAS:
		return "GasCoin"
	case Argument_INPUT:
		return map[string]any{"Input": x.GetInput()}
	case Argument_RESULT:
		if x.Subresult != nil {
			return map[string]any{"NestedResult": []any{x.GetResult(), x.GetSubresult()}}
		}
		return map[string]any{"Result": x.GetResult()}
	default:
		return nil
	}
}
