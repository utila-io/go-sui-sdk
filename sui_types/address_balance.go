package sui_types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	"github.com/utila-io/go-sui-sdk/move_types"
)

// NormalizeCoinAddress pads a package-address hex segment from a coin type string
// (with or without a "0x" prefix) to MovePackageAddressHexLength hex characters.
func NormalizeCoinAddress(addrHex string) string {
	addrHex = strings.TrimPrefix(addrHex, "0x")
	if len(addrHex) < MovePackageAddressHexLength {
		return strings.Repeat("0", MovePackageAddressHexLength-len(addrHex)) + addrHex
	}
	return addrHex
}

func ParseCoinTypeTag(coinType string) (move_types.TypeTag, error) {
	parts := strings.Split(coinType, CoinTypeModuleDelimiter)
	if len(parts) != 3 {
		return move_types.TypeTag{}, &InvalidCoinTypeError{
			CoinType: coinType,
			Msg:      "expected '<address>::<module>::<name>'",
		}
	}

	addrHex := NormalizeCoinAddress(parts[0])
	addrBytes, err := hex.DecodeString(addrHex)
	if err != nil {
		return move_types.TypeTag{}, &InvalidCoinTypeError{
			CoinType: coinType,
			Msg:      fmt.Sprintf("decoding address hex: %v", err),
		}
	}
	if len(addrBytes) != AccountAddressSize {
		return move_types.TypeTag{}, &InvalidCoinTypeError{
			CoinType: coinType,
			Msg: fmt.Sprintf(
				"address decodes to %d bytes, expected %d", len(addrBytes), AccountAddressSize,
			),
		}
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

// HexToChainIdentifier resolves a short hex chain ID (e.g. "35834a8a") to the
// full 32-byte genesis checkpoint digest used as ChainIdentifier in BCS.
// Known networks (mainnet, testnet) are resolved via their genesis Base58 digests.
func HexToChainIdentifier(hexStr string) ([]byte, error) {
	if hexStr == "" {
		return nil, fmt.Errorf("chain identifier is empty")
	}
	hexStr = strings.TrimPrefix(hexStr, "0x")

	if b58, ok := genesisCheckpointBase58ByShortHex[hexStr]; ok {
		return Base58ToChainIdentifier(b58)
	}

	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("decoding chain identifier hex: %w", err)
	}
	if len(b) != ChainIdentifierDigestSize {
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
	if len(decoded) != ChainIdentifierDigestSize {
		return nil, fmt.Errorf(
			"Base58 chain identifier decodes to %d bytes, expected %d",
			len(decoded), ChainIdentifierDigestSize,
		)
	}
	return decoded, nil
}
