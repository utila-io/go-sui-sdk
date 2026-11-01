package rpcv2

import (
	"bytes"
	"testing"

	"github.com/fardream/go-bcs/bcs"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
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
// sui_types.Digest the conversions are expected to produce.
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

// testRecipientAddress is the transfer recipient in the test transaction.
const testRecipientAddress = "0x1111111111111111111111111111111111111111111111111111111111111111"

// testTransactionData builds a small but representative TransactionData: a
// PTB with pure, shared-object, owned-object and receiving inputs feeding
// SplitCoins, TransferObjects and a MoveCall.
func testTransactionData(t *testing.T) sui_types.TransactionData {
	t.Helper()
	sender := mustAddress(t, longOwnerAddress)
	recipient := mustAddress(t, testRecipientAddress)

	amountBytes, err := bcs.Marshal(uint64(1000))
	require.NoError(t, err)
	recipientBytes, err := bcs.Marshal(recipient)
	require.NoError(t, err)
	unresolvedBytes := []byte{0x01, 0x02, 0x03}

	pt := sui_types.ProgrammableTransaction{
		Inputs: []sui_types.CallArg{
			{Pure: &amountBytes},    // 0: SplitCoins amount, inferred u64
			{Pure: &recipientBytes}, // 1: TransferObjects recipient, inferred address
			{Object: &sui_types.ObjectArg{SharedObject: &struct {
				Id                   sui_types.ObjectID
				InitialSharedVersion sui_types.SequenceNumber
				Mutable              bool
			}{
				Id:                   mustAddress(t, longSuiPackage),
				InitialSharedVersion: 7,
				Mutable:              true,
			}}},
			{Object: &sui_types.ObjectArg{ImmOrOwnedObject: &sui_types.ObjectRef{
				ObjectId: mustAddress(t, longObjectID),
				Version:  42,
				Digest:   lib.Base58(bytes.Repeat([]byte{0x21}, 32)),
			}}},
			{Pure: &unresolvedBytes}, // 4: only used by the MoveCall, not resolved
			{Object: &sui_types.ObjectArg{Receiving: &sui_types.ObjectRef{
				ObjectId: mustAddress(t, testRecipientAddress),
				Version:  43,
				Digest:   lib.Base58(bytes.Repeat([]byte{0x22}, 32)),
			}}},
		},
		Commands: []sui_types.Command{
			{SplitCoins: &sui_types.SplitCoinsCommand{
				Argument:  sui_types.Argument{GasCoin: &lib.EmptyEnum{}},
				Arguments: []sui_types.Argument{{Input: ptr(uint16(0))}},
			}},
			{TransferObjects: &sui_types.TransferObjectsCommand{
				Arguments: []sui_types.Argument{{Result: ptr(uint16(0))}},
				Argument:  sui_types.Argument{Input: ptr(uint16(1))},
			}},
			{MoveCall: &sui_types.ProgrammableMoveCall{
				Package:  mustAddress(t, longSuiPackage),
				Module:   "coin",
				Function: "join",
				TypeArguments: []move_types.TypeTag{{Struct: &move_types.StructTag{
					Address: mustAddress(t, longSuiPackage),
					Module:  "sui",
					Name:    "SUI",
				}}},
				Arguments: []sui_types.Argument{
					{Input: ptr(uint16(2))},
					{Input: ptr(uint16(3))},
					{Input: ptr(uint16(4))},
					{Input: ptr(uint16(5))},
					{NestedResult: &sui_types.NestedResultArgument{Result1: 0, Result2: 0}},
				},
			}},
		},
	}
	return sui_types.TransactionData{V1: &sui_types.TransactionDataV1{
		Kind:   sui_types.TransactionKind{ProgrammableTransaction: &pt},
		Sender: sender,
		GasData: sui_types.GasData{
			Payment: []*sui_types.ObjectRef{{
				ObjectId: mustAddress(t, longObjectID),
				Version:  5,
				Digest:   lib.Base58(bytes.Repeat([]byte{0x42}, 32)),
			}},
			Owner:  sender,
			Price:  1000,
			Budget: 5_000_000,
		},
		Expiration: sui_types.TransactionExpiration{None: &lib.EmptyEnum{}},
	}}
}

func testTransactionDataBytes(t *testing.T) []byte {
	t.Helper()
	txData, err := bcs.Marshal(testTransactionData(t))
	require.NoError(t, err)
	return txData
}

// changedObject builds a ChangedObject with the given states; mutate the
// returned message for the per-case details.
func changedObject(id string, input ChangedObject_InputObjectState, output ChangedObject_OutputObjectState, op ChangedObject_IdOperation) *ChangedObject {
	return &ChangedObject{
		ObjectId:    proto.String(id),
		InputState:  input.Enum(),
		OutputState: output.Enum(),
		IdOperation: op.Enum(),
	}
}

func addressOwner(addr string) *Owner {
	return &Owner{Kind: Owner_ADDRESS.Enum(), Address: proto.String(addr)}
}
