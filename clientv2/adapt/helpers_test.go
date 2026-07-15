package adapt

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

// Long-form (32-byte, 64 hex char) addresses and object IDs, exactly as the
// gRPC API returns them.
const (
	longSuiPackage = "0x0000000000000000000000000000000000000000000000000000000000000002"
	longSuiType    = "0x0000000000000000000000000000000000000000000000000000000000000002::sui::SUI"
	shortSuiType   = "0x2::sui::SUI"

	longUsdcType  = "0x00000000000000000000000000000000000000000000000000000000c0ffee01::usdc::USDC"
	shortUsdcType = "0xc0ffee01::usdc::USDC"

	longOwnerAddress = "0x7a8442cf08d8f81579e39f0996b21145b2848a3e13b5c9b537f0d6b823e0904b"
	longObjectID     = "0x0ed9afd0d3b41bbcd7458dc65b785b0d5a1e6f0a1b0e9d9f1c94b41d8b7f0f10"
)

// testDigest builds a deterministic 32-byte digest from a filler byte and
// returns both its base58 string (the proto wire form) and the decoded
// sui_types.Digest the adapters are expected to produce.
func testDigest(fill byte) (string, sui_types.Digest) {
	digest := lib.Base58(bytes.Repeat([]byte{fill}, 32))
	return digest.String(), sui_types.Digest(digest)
}

func mustAddress(t *testing.T, str string) sui_types.SuiAddress {
	t.Helper()
	addr, err := sui_types.NewAddressFromHex(str)
	require.NoError(t, err)
	return *addr
}

func ptr[T any](v T) *T { return &v }
