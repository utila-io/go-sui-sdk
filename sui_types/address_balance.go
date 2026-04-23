package sui_types

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/coming-chat/go-sui/v2/move_types"
)

// ParseCoinTypeTag parses a coin type string like "0x2::sui::SUI" into a move_types.TypeTag.
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

// RandomUint32 returns a cryptographically random uint32, suitable for use as
// a ValidDuring nonce.
func RandomUint32() (uint32, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, fmt.Errorf("generating random nonce: %w", err)
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

// Sui2FrameworkID returns the object ID of the Sui framework package (0x2).
func Sui2FrameworkID() ObjectID {
	var id ObjectID
	id[len(id)-1] = 0x02
	return id
}

// HexToChainIdentifier parses a hex chain identifier string (e.g. "4c78adac")
// into the [32]byte format required by ValidDuringExpiration.
func HexToChainIdentifier(hexStr string) ([32]byte, error) {
	var result [32]byte
	if hexStr == "" {
		return result, fmt.Errorf("chain identifier is empty")
	}
	hexStr = strings.TrimPrefix(hexStr, "0x")
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return result, fmt.Errorf("decoding chain identifier hex: %w", err)
	}
	if len(b) > 32 {
		return result, fmt.Errorf("chain identifier too long: %d bytes", len(b))
	}
	copy(result[32-len(b):], b)
	return result, nil
}
