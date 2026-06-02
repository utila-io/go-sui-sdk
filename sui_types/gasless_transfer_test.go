package sui_types

import (
	"testing"

	"github.com/fardream/go-bcs/bcs"
)

func testAddrs(t *testing.T) (sender, recipient SuiAddress) {
	t.Helper()
	sender[31] = 0xAA
	recipient[31] = 0xBB
	return sender, recipient
}

func testCoins(ids ...byte) []*ObjectRef {
	coins := make([]*ObjectRef, 0, len(ids))
	for _, id := range ids {
		var objID ObjectID
		objID[31] = id
		coins = append(coins, &ObjectRef{ObjectId: objID})
	}
	return coins
}

// allowedGaslessMoveCalls are the only Move calls a gasless PTB may emit.
var allowedGaslessMoveCalls = map[string]bool{
	"balance::redeem_funds": true,
	"balance::send_funds":   true,
	"coin::redeem_funds":    true,
	"coin::send_funds":      true,
}

// assertOnlyGaslessCommands fails on any command other than an allowed send_funds/redeem_funds
// MoveCall, MergeCoins, or SplitCoins (e.g. a TransferObjects that would leak an owned object).
func assertOnlyGaslessCommands(t *testing.T, pt ProgrammableTransaction) {
	t.Helper()
	for i, cmd := range pt.Commands {
		switch {
		case cmd.MoveCall != nil:
			key := string(cmd.MoveCall.Module) + "::" + string(cmd.MoveCall.Function)
			if !allowedGaslessMoveCalls[key] {
				t.Fatalf("cmd[%d] is MoveCall %s; not in the gasless allow-set", i, key)
			}
		case cmd.MergeCoins != nil, cmd.SplitCoins != nil:
			// allowed: consolidate coin inputs / split out the transfer amount
		default:
			t.Fatalf(
				"cmd[%d] = %+v; gasless PTBs may only use balance/coin redeem_funds/send_funds, MergeCoins, SplitCoins",
				i, cmd,
			)
		}
	}
}

func moveCallFn(t *testing.T, cmd Command) (module, function string) {
	t.Helper()
	if cmd.MoveCall == nil {
		t.Fatalf("expected MoveCall command, got %+v", cmd)
	}
	return string(cmd.MoveCall.Module), string(cmd.MoveCall.Function)
}

// Address balance only — redeem + send, one FundsWithdrawal input, no merge/split.
func TestGaslessTransfer_AddressBalanceOnly(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, nil, 2_000_000, 2_000_000, coinType); err != nil {
		t.Fatal(err)
	}
	pt := ptb.Finish()

	if len(pt.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(pt.Commands))
	}
	if m, f := moveCallFn(t, pt.Commands[0]); m != "balance" || f != "redeem_funds" {
		t.Fatalf("cmd[0] = %s::%s, want balance::redeem_funds", m, f)
	}
	if m, f := moveCallFn(t, pt.Commands[1]); m != "balance" || f != "send_funds" {
		t.Fatalf("cmd[1] = %s::%s, want balance::send_funds", m, f)
	}
	assertOnlyGaslessCommands(t, pt)

	var withdrawals int
	for _, in := range pt.Inputs {
		if in.FundsWithdrawal != nil {
			withdrawals++
		}
	}
	if withdrawals != 1 {
		t.Fatalf("expected exactly 1 FundsWithdrawal input, got %d", withdrawals)
	}
}

// Multiple coins, no withdrawal — merge, split, then two coin::send_funds (recipient + change).
func TestGaslessTransfer_MultipleCoins_Merge(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, testCoins(0xC1, 0xC2), 1_500_000, 0, coinType); err != nil {
		t.Fatal(err)
	}
	pt := ptb.Finish()

	if len(pt.Commands) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(pt.Commands))
	}
	if pt.Commands[0].MergeCoins == nil {
		t.Fatal("cmd[0] must be MergeCoins")
	}
	if pt.Commands[1].SplitCoins == nil {
		t.Fatal("cmd[1] must be SplitCoins")
	}
	if m, f := moveCallFn(t, pt.Commands[2]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[2] = %s::%s, want coin::send_funds", m, f)
	}
	if m, f := moveCallFn(t, pt.Commands[3]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[3] = %s::%s, want coin::send_funds", m, f)
	}
	assertOnlyGaslessCommands(t, pt)

	for _, in := range pt.Inputs {
		if in.FundsWithdrawal != nil {
			t.Fatal("Case B must not have a FundsWithdrawal input")
		}
	}
}

// Single coin, no withdrawal — no MergeCoins needed, so SplitCoins + two send_funds.
func TestGaslessTransfer_SingleCoin_NoMerge(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, testCoins(0xC1), 1_000_000, 0, coinType); err != nil {
		t.Fatal(err)
	}
	pt := ptb.Finish()

	if len(pt.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(pt.Commands))
	}
	if pt.Commands[0].SplitCoins == nil {
		t.Fatal("cmd[0] must be SplitCoins (no merge for a single coin)")
	}
	if m, f := moveCallFn(t, pt.Commands[1]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[1] = %s::%s, want coin::send_funds", m, f)
	}
	if m, f := moveCallFn(t, pt.Commands[2]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[2] = %s::%s, want coin::send_funds", m, f)
	}
	assertOnlyGaslessCommands(t, pt)
}

// Coins plus an address-balance shortfall — redeem the shortfall as a coin, merge, split, send.
func TestGaslessTransfer_CoinsPlusWithdrawal(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, testCoins(0xC1), 2_000_000, 500_000, coinType); err != nil {
		t.Fatal(err)
	}
	pt := ptb.Finish()

	if len(pt.Commands) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(pt.Commands))
	}
	if m, f := moveCallFn(t, pt.Commands[0]); m != "coin" || f != "redeem_funds" {
		t.Fatalf("cmd[0] = %s::%s, want coin::redeem_funds", m, f)
	}
	if pt.Commands[1].MergeCoins == nil {
		t.Fatal("cmd[1] must be MergeCoins")
	}
	if pt.Commands[2].SplitCoins == nil {
		t.Fatal("cmd[2] must be SplitCoins")
	}
	if m, f := moveCallFn(t, pt.Commands[3]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[3] = %s::%s, want coin::send_funds", m, f)
	}
	if m, f := moveCallFn(t, pt.Commands[4]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[4] = %s::%s, want coin::send_funds", m, f)
	}
	assertOnlyGaslessCommands(t, pt)

	var withdrawals int
	for _, in := range pt.Inputs {
		if in.FundsWithdrawal != nil {
			withdrawals++
		}
	}
	if withdrawals != 1 {
		t.Fatalf("expected exactly 1 FundsWithdrawal input (the shortfall), got %d", withdrawals)
	}

	// BCS round-trip to confirm the PTB serializes/deserializes cleanly.
	txData := NewProgrammable(sender, nil, pt, 0, 0)
	data, err := bcs.Marshal(txData)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded TransactionData
	if _, err := bcs.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := len(decoded.V1.Kind.ProgrammableTransaction.Commands); got != 5 {
		t.Fatalf("decoded command count = %d, want 5", got)
	}
}

func TestGaslessTransfer_zeroAmountRejected(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")
	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, nil, 0, 0, coinType); err == nil {
		t.Fatal("expected error for totalAmount == 0")
	}
}

func TestGaslessTransfer_addressBalanceMismatchRejected(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")
	ptb := NewProgrammableTransactionBuilder()
	// No coins and withdrawalAmount < totalAmount: nothing covers the rest, so reject.
	if err := ptb.GaslessTransfer(sender, recipient, nil, 2_000_000, 1_000_000, coinType); err == nil {
		t.Fatal("expected error when withdrawalAmount != totalAmount with no coins")
	}
}
