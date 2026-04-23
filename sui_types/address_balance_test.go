package sui_types

import (
	"testing"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
	"github.com/fardream/go-bcs/bcs"
)

func TestParseCoinTypeTag(t *testing.T) {
	tag, err := ParseCoinTypeTag("0x2::sui::SUI")
	if err != nil {
		t.Fatal(err)
	}
	if tag.Struct == nil {
		t.Fatal("expected Struct tag")
	}
	if tag.Struct.Module != "sui" || tag.Struct.Name != "SUI" {
		t.Fatalf("unexpected tag: %+v", tag.Struct)
	}
}

func TestHexToChainIdentifier(t *testing.T) {
	chain, err := HexToChainIdentifier("4c78adac")
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(chain))
	}
	// First 4 bytes of testnet genesis digest should be 4c78adac
	if chain[0] != 0x4c || chain[1] != 0x78 || chain[2] != 0xad || chain[3] != 0xac {
		t.Fatalf("unexpected chain bytes prefix: %x", chain[:4])
	}
}

func TestTransactionExpirationBCSRoundTrip(t *testing.T) {
	minEpoch := uint64(100)
	maxEpoch := uint64(200)
	nonce := uint32(42)
	chain, _ := HexToChainIdentifier("4c78adac")

	exp := TransactionExpiration{
		ValidDuring: &ValidDuringExpiration{
			MinEpoch: &minEpoch,
			MaxEpoch: &maxEpoch,
			Chain:    chain,
			Nonce:    nonce,
		},
	}

	data, err := bcs.Marshal(exp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TransactionExpiration
	_, err = bcs.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ValidDuring == nil {
		t.Fatal("expected ValidDuring variant")
	}
	if *decoded.ValidDuring.MinEpoch != minEpoch {
		t.Fatalf("min epoch mismatch: got %d", *decoded.ValidDuring.MinEpoch)
	}
	if *decoded.ValidDuring.MaxEpoch != maxEpoch {
		t.Fatalf("max epoch mismatch: got %d", *decoded.ValidDuring.MaxEpoch)
	}
	if decoded.ValidDuring.Nonce != nonce {
		t.Fatalf("nonce mismatch: got %d", decoded.ValidDuring.Nonce)
	}
}

func TestCallArgFundsWithdrawalBCSRoundTrip(t *testing.T) {
	amount := uint64(1_000_000)
	tag, _ := ParseCoinTypeTag("0x2::sui::SUI")

	arg := CallArg{
		FundsWithdrawal: &FundsWithdrawalArg{
			Reservation:  Reservation{MaxAmountU64: &amount},
			TypeArg:      WithdrawalTypeArg{Balance: &tag},
			WithdrawFrom: WithdrawFrom{Sender: &lib.EmptyEnum{}},
		},
	}

	data, err := bcs.Marshal(arg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CallArg
	_, err = bcs.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.FundsWithdrawal == nil {
		t.Fatal("expected FundsWithdrawal variant")
	}
	if *decoded.FundsWithdrawal.Reservation.MaxAmountU64 != amount {
		t.Fatalf("amount mismatch: got %d", *decoded.FundsWithdrawal.Reservation.MaxAmountU64)
	}
}

func TestFullTransactionDataWithSIP58BCSRoundTrip(t *testing.T) {
	amount := uint64(500_000)
	tag, _ := ParseCoinTypeTag("0x2::sui::SUI")
	chain, _ := HexToChainIdentifier("4c78adac")
	nonce := uint32(99)
	minEpoch := uint64(10)
	maxEpoch := uint64(20)

	var sender SuiAddress
	sender[31] = 0x01

	ptb := ProgrammableTransaction{
		Inputs: []CallArg{
			{FundsWithdrawal: &FundsWithdrawalArg{
				Reservation:  Reservation{MaxAmountU64: &amount},
				TypeArg:      WithdrawalTypeArg{Balance: &tag},
				WithdrawFrom: WithdrawFrom{Sender: &lib.EmptyEnum{}},
			}},
		},
		Commands: []Command{
			{MoveCall: &ProgrammableMoveCall{
				Package:       Sui2FrameworkID(),
				Module:        move_types.Identifier("coin"),
				Function:      move_types.Identifier("redeem_funds"),
				TypeArguments: []move_types.TypeTag{tag},
				Arguments:     []Argument{{Input: ptrU16(0)}},
			}},
		},
	}

	txData := NewProgrammableWithExpiration(
		sender, nil, ptb, 3_000_000, 1000,
		TransactionExpiration{
			ValidDuring: &ValidDuringExpiration{
				MinEpoch: &minEpoch,
				MaxEpoch: &maxEpoch,
				Chain:    chain,
				Nonce:    nonce,
			},
		},
	)

	data, err := bcs.Marshal(txData)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TransactionData
	_, err = bcs.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.V1 == nil {
		t.Fatal("expected V1")
	}
	if decoded.V1.Expiration.ValidDuring == nil {
		t.Fatal("expected ValidDuring expiration")
	}
	if decoded.V1.Expiration.ValidDuring.Nonce != nonce {
		t.Fatalf("nonce mismatch: got %d", decoded.V1.Expiration.ValidDuring.Nonce)
	}

	ptbDecoded := decoded.V1.Kind.ProgrammableTransaction
	if ptbDecoded == nil {
		t.Fatal("expected ProgrammableTransaction")
	}
	if len(ptbDecoded.Inputs) != 1 {
		t.Fatalf("expected 1 input, got %d", len(ptbDecoded.Inputs))
	}
	if ptbDecoded.Inputs[0].FundsWithdrawal == nil {
		t.Fatal("expected FundsWithdrawal input")
	}
}

func TestWithdrawalTransfer_WithCoins(t *testing.T) {
	var recipient SuiAddress
	recipient[31] = 0xBB

	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	var coinID1, coinID2 ObjectID
	coinID1[31] = 0xC1
	coinID2[31] = 0xC2
	coins := []*ObjectRef{
		{ObjectId: coinID1},
		{ObjectId: coinID2},
	}

	var sender SuiAddress
	sender[31] = 0xAA

	ptb := NewProgrammableTransactionBuilder()
	err := ptb.WithdrawalTransfer(recipient, coins, 1_500_000, 500_000, coinType, sender)
	if err != nil {
		t.Fatal(err)
	}

	pt := ptb.Finish()

	// Inputs: recipient(0), amount(1), withdrawal(2), coin0(3), coin1(4)
	if len(pt.Inputs) != 5 {
		t.Fatalf("expected 5 inputs, got %d", len(pt.Inputs))
	}
	if pt.Inputs[0].Pure == nil {
		t.Fatal("input[0] must be recipient pure")
	}
	if pt.Inputs[1].Pure == nil {
		t.Fatal("input[1] must be amount pure")
	}
	if pt.Inputs[2].FundsWithdrawal == nil {
		t.Fatal("input[2] must be FundsWithdrawal")
	}
	if pt.Inputs[3].Object == nil {
		t.Fatal("input[3] must be coin object")
	}
	if pt.Inputs[4].Object == nil {
		t.Fatal("input[4] must be coin object")
	}

	// Commands: redeem_funds, MergeCoins, SplitCoins, TransferObjects
	// (with-coins path: no extra transfer for remainder since sourceCoin is an ObjectArg)
	if len(pt.Commands) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(pt.Commands))
	}
	if pt.Commands[0].MoveCall == nil {
		t.Fatal("cmd[0] must be MoveCall (redeem_funds)")
	}
	if string(pt.Commands[0].MoveCall.Function) != "redeem_funds" {
		t.Fatalf("cmd[0] function = %s, want redeem_funds", pt.Commands[0].MoveCall.Function)
	}
	if pt.Commands[1].MergeCoins == nil {
		t.Fatal("cmd[1] must be MergeCoins")
	}
	if pt.Commands[2].SplitCoins == nil {
		t.Fatal("cmd[2] must be SplitCoins")
	}
	if pt.Commands[3].TransferObjects == nil {
		t.Fatal("cmd[3] must be TransferObjects")
	}

	// BCS round-trip (reuse sender from above)
	txData := NewProgrammable(sender, nil, pt, 0, 0)
	data, err := bcs.Marshal(txData)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded TransactionData
	_, err = bcs.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.V1.Kind.ProgrammableTransaction.Inputs) != 5 {
		t.Fatal("decoded inputs count mismatch")
	}
	if decoded.V1.Kind.ProgrammableTransaction.Inputs[2].FundsWithdrawal == nil {
		t.Fatal("decoded input[2] must be FundsWithdrawal")
	}
}

func TestWithdrawalTransfer_WithoutCoins(t *testing.T) {
	var recipient SuiAddress
	recipient[31] = 0xBB

	var sender SuiAddress
	sender[31] = 0xAA

	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	ptb := NewProgrammableTransactionBuilder()
	err := ptb.WithdrawalTransfer(recipient, nil, 2_000_000, 2_000_000, coinType, sender)
	if err != nil {
		t.Fatal(err)
	}

	pt := ptb.Finish()

	// Inputs: recipient(0), amount(1), withdrawal(2), sender(3)
	if len(pt.Inputs) != 4 {
		t.Fatalf("expected 4 inputs, got %d", len(pt.Inputs))
	}
	if pt.Inputs[2].FundsWithdrawal == nil {
		t.Fatal("input[2] must be FundsWithdrawal")
	}
	if pt.Inputs[3].Pure == nil {
		t.Fatal("input[3] must be sender pure")
	}

	// Commands: redeem_funds, SplitCoins, TransferObjects(split→recipient),
	// TransferObjects(remainder→sender)
	if len(pt.Commands) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(pt.Commands))
	}
	if pt.Commands[0].MoveCall == nil || string(pt.Commands[0].MoveCall.Function) != "redeem_funds" {
		t.Fatal("cmd[0] must be redeem_funds")
	}
	if pt.Commands[1].SplitCoins == nil {
		t.Fatal("cmd[1] must be SplitCoins")
	}
	if pt.Commands[2].TransferObjects == nil {
		t.Fatal("cmd[2] must be TransferObjects (split to recipient)")
	}
	if pt.Commands[3].TransferObjects == nil {
		t.Fatal("cmd[3] must be TransferObjects (remainder to sender)")
	}
}

func ptrU16(v uint16) *uint16 { return &v }
