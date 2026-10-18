package rpcv2

import (
	"regexp"
	"strings"
)

// hexAddressPattern matches a 0x-prefixed hex address anywhere in a type
// string, including inside generic type parameters.
var hexAddressPattern = regexp.MustCompile(`0[xX][0-9a-fA-F]+`)

// shortTypeAddresses mirrors SUI_ADDRESSES in sui-types sui_serde.rs: the
// addresses JSON-RPC renders short in type strings.
var shortTypeAddresses = map[string]bool{
	"0": true, "1": true, "2": true, "3": true, "5": true, "6": true, "dee9": true,
}

// normalizeTypeString rewrites every address in a Move type string the way Sui
// JSON-RPC renders struct tags (to_sui_struct_tag_string): system addresses
// short ("0x0000...02::sui::SUI" -> "0x2::sui::SUI"), every other address as 64
// lowercase hex digits ("0x6864a...::cetus::CETUS" -> "0x06864a...::cetus::CETUS").
// Every type string surfaced to callers must pass through here. Plain object
// IDs and owner addresses stay long form.
func normalizeTypeString(typeStr string) string {
	return hexAddressPattern.ReplaceAllStringFunc(typeStr, func(addr string) string {
		digits := strings.ToLower(addr[2:])
		trimmed := strings.TrimLeft(digits, "0")
		if trimmed == "" {
			trimmed = "0"
		}
		if shortTypeAddresses[trimmed] {
			return "0x" + trimmed
		}
		if len(digits) >= 64 {
			return "0x" + digits
		}
		return "0x" + strings.Repeat("0", 64-len(digits)) + digits
	})
}
