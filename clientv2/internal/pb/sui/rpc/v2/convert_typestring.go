package rpcv2

import (
	"regexp"
	"strings"
)

// hexAddressPattern matches a 0x-prefixed hex address anywhere in a type
// string, including inside generic type parameters.
var hexAddressPattern = regexp.MustCompile(`0[xX][0-9a-fA-F]+`)

// normalizeTypeString rewrites every address in a Move type string to the
// short form the internal types use ("0x0000...02::sui::SUI" ->
// "0x2::sui::SUI"); every type string surfaced to callers must pass through
// here. Plain object IDs and owner addresses stay long form, which is what the
// internal shape carries there.
func normalizeTypeString(typeStr string) string {
	return hexAddressPattern.ReplaceAllStringFunc(typeStr, func(addr string) string {
		trimmed := strings.TrimLeft(addr[2:], "0")
		if trimmed == "" {
			trimmed = "0"
		}
		return "0x" + trimmed
	})
}
