// Package adapt converts sui.rpc.v2 protobuf messages into the SDK's
// JSON-RPC-shaped structs from the types package, so both backends surface
// identical data. All functions are pure and network-free so they can be unit
// tested with hand-built protos.
package adapt

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

// hexAddressPattern matches a 0x-prefixed hex address anywhere inside a type
// string, including addresses nested in generic type parameters.
var hexAddressPattern = regexp.MustCompile(`0[xX][0-9a-fA-F]+`)

// NormalizeTypeString rewrites every address component of a Move type string
// to the short form used by JSON-RPC v1, e.g.
// "0x0000...0002::sui::SUI" -> "0x2::sui::SUI". gRPC returns full-length
// addresses in type strings while JSON-RPC v1 trims leading zeros, so every
// type string surfaced to callers must pass through here. Plain object IDs and
// owner addresses stay long form (v1 uses full-length there).
func NormalizeTypeString(typeStr string) string {
	return hexAddressPattern.ReplaceAllStringFunc(typeStr, func(addr string) string {
		trimmed := strings.TrimLeft(addr[2:], "0")
		if trimmed == "" {
			trimmed = "0"
		}
		return "0x" + trimmed
	})
}

// parseAddress converts a proto address/object-ID string into a SuiAddress.
func parseAddress(str string) (sui_types.SuiAddress, error) {
	addr, err := sui_types.NewAddressFromHex(str)
	if err != nil {
		return sui_types.SuiAddress{}, fmt.Errorf("invalid address %q: %w", str, err)
	}
	return *addr, nil
}

// parseDigest converts a proto base58 digest string into a sui_types.Digest.
// Invalid base58 input decodes to an empty digest rather than an error: the
// digests handled here come from the node, which only emits canonical base58,
// so the empty-digest case is unreachable in practice and not worth threading
// an error through every adapter for.
func parseDigest(str string) sui_types.Digest {
	digest, _ := lib.NewBase58(str) // invalid chars decode to empty
	return *digest
}
