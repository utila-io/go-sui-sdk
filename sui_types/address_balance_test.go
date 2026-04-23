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
	if chain[28] != 0x4c || chain[29] != 0x78 || chain[30] != 0xad || chain[31] != 0xac {
		t.Fatalf("unexpected chain bytes: %x", chain)
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

func ptrU16(v uint16) *uint16 { return &v }
