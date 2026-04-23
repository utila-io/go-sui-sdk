package sui_types

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	"github.com/utila-io/go-sui-sdk/move_types"
)

const (
	MainnetGenesisBase58 = "4btiuiMPvEENsttpZC7CZ53DruC3MAgfznDbASZ7DR6S"
	TestnetGenesisBase58 = "69WiPg3DAQiwdxfncX6wYQ2siKwAe6L9BZthQea3JNMD"
)

func ParseCoinTypeTag(coinType string) (move_types.TypeTag, error) {
	parts := strings.Split(coinType, "::")
	if len(parts) != 3 {
		return move_types.TypeTag{}, fmt.Errorf("invalid coin type: %s", coinType)
	}

	addrHex := strings.TrimPrefix(parts[0], "0x")
	if len(addrHex) < 64 {
		addrHex = strings.Repeat("0", 64-len(addrHex)) + addrHex
	}
	addrBytes, err := hex.DecodeString(addrHex)
	if err != nil {
		return move_types.TypeTag{}, fmt.Errorf("parsing coin type address: %w", err)
	}
	if len(addrBytes) != 32 {
		return move_types.TypeTag{}, fmt.Errorf(
			"parsing coin type address: expected 32 bytes, got %d", len(addrBytes),
		)
	}

	var addr move_types.AccountAddress
	copy(addr[:], addrBytes)

	return move_types.TypeTag{
		Struct: &move_types.StructTag{
			Address:    addr,
			Module:     move_types.Identifier(parts[1]),
			Name:       move_types.Identifier(parts[2]),
			TypeParams: []move_types.TypeTag{},
		},
	}, nil
}

func RandomUint32() (uint32, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, fmt.Errorf("generating random nonce: %w", err)
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func Sui2FrameworkID() ObjectID {
	var id ObjectID
	id[len(id)-1] = 0x02
	return id
}

// HexToChainIdentifier resolves a short hex chain ID (e.g. "35834a8a") to the
// full 32-byte genesis checkpoint digest used as ChainIdentifier in BCS.
// Known networks (mainnet, testnet) are resolved via their genesis Base58 digests.
func HexToChainIdentifier(hexStr string) ([]byte, error) {
	if hexStr == "" {
		return nil, fmt.Errorf("chain identifier is empty")
	}
	hexStr = strings.TrimPrefix(hexStr, "0x")

	knownChains := map[string]string{
		"35834a8a": MainnetGenesisBase58,
		"4c78adac": TestnetGenesisBase58,
	}
	if b58, ok := knownChains[hexStr]; ok {
		return Base58ToChainIdentifier(b58)
	}

	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("decoding chain identifier hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf(
			"unknown short chain ID %q; provide the full 32-byte hex or Base58 genesis digest",
			hexStr,
		)
	}
	return b, nil
}

// Base58ToChainIdentifier decodes a Base58-encoded genesis checkpoint digest
// into the 32-byte chain identifier used in ValidDuring expiration.
func Base58ToChainIdentifier(b58 string) ([]byte, error) {
	decoded := base58.Decode(b58)
	if len(decoded) != 32 {
		return nil, fmt.Errorf(
			"Base58 chain identifier decodes to %d bytes, expected 32", len(decoded),
		)
	}
	return decoded, nil
}
