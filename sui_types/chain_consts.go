package sui_types

const (
	// CoinTypeModuleDelimiter separates address, module, and name in a coin type string.
	CoinTypeModuleDelimiter = "::"

	// MovePackageAddressHexLength is the hex character length of a canonical package address in coin types.
	MovePackageAddressHexLength = 64

	MainnetGenesisBase58 = "4btiuiMPvEENsttpZC7CZ53DruC3MAgfznDbASZ7DR6S"
	TestnetGenesisBase58 = "69WiPg3DAQiwdxfncX6wYQ2siKwAe6L9BZthQea3JNMD"

	MainnetChainShortHex = "35834a8a"
	TestnetChainShortHex = "4c78adac"
)

const (
	AccountAddressSize        = 32
	ChainIdentifierDigestSize = 32
)

// genesisCheckpointBase58ByShortHex maps the first four bytes of a known network's
// genesis checkpoint digest (hex, no 0x) to the full Base58 genesis digest.
var genesisCheckpointBase58ByShortHex = map[string]string{
	MainnetChainShortHex: MainnetGenesisBase58,
	TestnetChainShortHex: TestnetGenesisBase58,
}

// Sui2FrameworkID is the Sui framework package id (0x000…0002) used in programmable transactions.
var Sui2FrameworkID = ObjectID{
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
}
