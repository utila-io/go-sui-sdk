package sui_types

import (
	"bytes"
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

// sendFundsArgs returns the (value, recipient) arguments of a 2-arg send_funds MoveCall.
func sendFundsArgs(t *testing.T, cmd Command) (value, recipient Argument) {
	t.Helper()
	if cmd.MoveCall == nil || len(cmd.MoveCall.Arguments) != 2 {
		t.Fatalf("expected a 2-arg send_funds MoveCall, got %+v", cmd)
	}
	return cmd.MoveCall.Arguments[0], cmd.MoveCall.Arguments[1]
}

// assertPure asserts that arg references a Pure input whose BCS bytes equal want — used to
// verify which address/amount a command was actually wired to (e.g. recipient vs sender).
func assertPure(t *testing.T, pt ProgrammableTransaction, arg Argument, want any) {
	t.Helper()
	if arg.Input == nil {
		t.Fatalf("expected a Pure Input argument, got %+v", arg)
	}
	idx := int(*arg.Input)
	if idx >= len(pt.Inputs) {
		t.Fatalf("input index %d out of range (%d inputs)", idx, len(pt.Inputs))
	}
	if pt.Inputs[idx].Pure == nil {
		t.Fatalf("input %d is not a Pure value: %+v", idx, pt.Inputs[idx])
	}
	wantBytes, err := bcs.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if got := *pt.Inputs[idx].Pure; !bytes.Equal(got, wantBytes) {
		t.Fatalf("pure input[%d] = %x, want %x", idx, got, wantBytes)
	}
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
	_, rec := sendFundsArgs(t, pt.Commands[1])
	assertPure(t, pt, rec, recipient)
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
			t.Fatal("coins-only path must not have a FundsWithdrawal input")
		}
	}
}

// Single coin, no withdrawal — no MergeCoins needed, so SplitCoins + two send_funds.
// Also asserts the value/amount wiring: the recipient gets the split-out totalAmount and
// the change goes back to the sender (a swapped recipient/sender would pass the shape-only
// checks but fail here).
func TestGaslessTransfer_SingleCoin_NoMerge(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	const amount = 1_000_000
	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, testCoins(0xC1), amount, 0, coinType); err != nil {
		t.Fatal(err)
	}
	pt := ptb.Finish()

	if len(pt.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(pt.Commands))
	}
	split := pt.Commands[0].SplitCoins
	if split == nil {
		t.Fatal("cmd[0] must be SplitCoins (no merge for a single coin)")
	}
	if len(split.Arguments) != 1 {
		t.Fatalf("SplitCoins must take exactly one amount arg, got %d", len(split.Arguments))
	}
	assertPure(t, pt, split.Arguments[0], uint64(amount))

	// Recipient send: must consume the split-out coin (SplitCoins is cmd 0, output 0) and target recipient.
	if m, f := moveCallFn(t, pt.Commands[1]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[1] = %s::%s, want coin::send_funds", m, f)
	}
	splitCoin, recArg := sendFundsArgs(t, pt.Commands[1])
	if splitCoin.NestedResult == nil || splitCoin.NestedResult.Result1 != 0 || splitCoin.NestedResult.Result2 != 0 {
		t.Fatalf("recipient send must consume the SplitCoins result (cmd 0, output 0), got %+v", splitCoin)
	}
	assertPure(t, pt, recArg, recipient)

	// Change send: must consume the remainder base coin (an Object input) and target the sender.
	if m, f := moveCallFn(t, pt.Commands[2]); m != "coin" || f != "send_funds" {
		t.Fatalf("cmd[2] = %s::%s, want coin::send_funds", m, f)
	}
	base, changeRec := sendFundsArgs(t, pt.Commands[2])
	if base.Input == nil {
		t.Fatalf("change send must consume the base coin input, got %+v", base)
	}
	assertPure(t, pt, changeRec, sender)

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

// Multiple coins plus a shortfall — the redeemed shortfall coin and every coin object are
// folded into one MergeCoins (locking the coinArgs[1:] wiring), then split and sent.
func TestGaslessTransfer_MultipleCoinsPlusWithdrawal(t *testing.T) {
	sender, recipient := testAddrs(t)
	coinType, _ := ParseCoinTypeTag("0x2::sui::SUI")

	const amount = 2_000_000
	ptb := NewProgrammableTransactionBuilder()
	if err := ptb.GaslessTransfer(sender, recipient, testCoins(0xC1, 0xC2), amount, 500_000, coinType); err != nil {
		t.Fatal(err)
	}
	pt := ptb.Finish()

	if len(pt.Commands) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(pt.Commands))
	}
	if m, f := moveCallFn(t, pt.Commands[0]); m != "coin" || f != "redeem_funds" {
		t.Fatalf("cmd[0] = %s::%s, want coin::redeem_funds", m, f)
	}
	// Merge folds two extra sources into the base coin: the second coin object + the redeemed shortfall.
	merge := pt.Commands[1].MergeCoins
	if merge == nil {
		t.Fatal("cmd[1] must be MergeCoins")
	}
	if len(merge.Arguments) != 2 {
		t.Fatalf("MergeCoins must fold 2 extra sources (coin 0xC2 + redeemed shortfall), got %d", len(merge.Arguments))
	}
	split := pt.Commands[2].SplitCoins
	if split == nil {
		t.Fatal("cmd[2] must be SplitCoins")
	}
	assertPure(t, pt, split.Arguments[0], uint64(amount))

	// Recipient send consumes the split result (SplitCoins is cmd 2); change goes to the sender.
	splitCoin, recArg := sendFundsArgs(t, pt.Commands[3])
	if splitCoin.NestedResult == nil || splitCoin.NestedResult.Result1 != 2 || splitCoin.NestedResult.Result2 != 0 {
		t.Fatalf("recipient send must consume the SplitCoins result (cmd 2, output 0), got %+v", splitCoin)
	}
	assertPure(t, pt, recArg, recipient)
	_, changeRec := sendFundsArgs(t, pt.Commands[4])
	assertPure(t, pt, changeRec, sender)

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
