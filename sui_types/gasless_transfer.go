package sui_types

import (
	"errors"
	"fmt"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
)

// Gasless stablecoin transfers (Sui SIP-58 / Address Balances).
//
// Sui's rule is "no objects written as outputs", not "no coin inputs": coins may be
// consumed if all are destroyed and credited to an address balance via send_funds. The
// PTB broadcasts with gasPrice/gasBudget=0 and empty gasPayment, so the protocol pays.
// Docs: https://docs.sui.io/develop/transaction-payment/gasless-stablecoin-transfers
//
// ASSUMPTION: the send_funds/redeem_funds signatures (incl. zero-value change to self)
// are unverified on-chain; the handler PR must add a testnet DryRun (gasUsed=0).

// BalanceSendFunds appends balance::send_funds<T>(balance, recipient), crediting the
// Balance<T> to recipient's address balance (terminal command of the address-balance case).
func (p *ProgrammableTransactionBuilder) BalanceSendFunds(
	balance Argument,
	recipient SuiAddress,
	coinType move_types.TypeTag,
) (Argument, error) {
	recArg, err := p.Pure(recipient)
	if err != nil {
		return Argument{}, err
	}
	return p.Command(
		Command{
			MoveCall: &ProgrammableMoveCall{
				Package:       Sui2FrameworkID,
				Module:        "balance",
				Function:      "send_funds",
				TypeArguments: []move_types.TypeTag{coinType},
				Arguments:     []Argument{balance, recArg},
			},
		},
	), nil
}

// CoinSendFunds appends coin::send_funds<T>(coin, recipient), crediting (and destroying)
// the Coin<T> to recipient's address balance. Used for the recipient amount and the change.
func (p *ProgrammableTransactionBuilder) CoinSendFunds(
	coin Argument,
	recipient SuiAddress,
	coinType move_types.TypeTag,
) (Argument, error) {
	recArg, err := p.Pure(recipient)
	if err != nil {
		return Argument{}, err
	}
	return p.Command(
		Command{
			MoveCall: &ProgrammableMoveCall{
				Package:       Sui2FrameworkID,
				Module:        "coin",
				Function:      "send_funds",
				TypeArguments: []move_types.TypeTag{coinType},
				Arguments:     []Argument{coin, recArg},
			},
		},
	), nil
}

// redeemFunds appends redeem_funds<T> on module "balance" (→Balance<T>) or "coin"
// (→Coin<T>), drawing withdrawalAmount from the sender's address balance.
// ASSUMPTION: balance::redeem_funds<T> (Case A) is doc-listed but unverified here.
func (p *ProgrammableTransactionBuilder) redeemFunds(
	module move_types.Identifier,
	withdrawalAmount uint64,
	coinType move_types.TypeTag,
) Argument {
	withdrawalArg := CreateFundsWithdrawalArgument(
		p, FundsWithdrawalArg{
			Reservation:  Reservation{MaxAmountU64: &withdrawalAmount},
			TypeArg:      WithdrawalTypeArg{Balance: &coinType},
			WithdrawFrom: WithdrawFrom{Sender: &lib.EmptyEnum{}},
		},
	)
	return p.Command(
		Command{
			MoveCall: &ProgrammableMoveCall{
				Package:       Sui2FrameworkID,
				Module:        module,
				Function:      "redeem_funds",
				TypeArguments: []move_types.TypeTag{coinType},
				Arguments:     []Argument{withdrawalArg},
			},
		},
	)
}

// GaslessTransfer builds an output-object-free PTB moving totalAmount of coinType to
// recipient. It mirrors WithdrawalTransfer but never emits TransferObjects: every coin
// is destroyed and its value routed to an address balance. The shape is picked from
// (len(coins), withdrawalAmount), matching the backend's PickCoins:
//
//   - Case A — address balance only: balance::redeem_funds → balance::send_funds.
//   - Case B — coins only: [MergeCoins] → SplitCoins → coin::send_funds x2 (recipient + change).
//   - Case C — coins + shortfall: coin::redeem_funds(shortfall) as a merge source, then Case B.
//
// The change (possibly zero) returns to sender's address balance, so nothing survives as
// an owned object. The helper trusts that coins+withdrawalAmount cover totalAmount.
//
// OPEN (UTILA-10259): multi-coin Case B and Case C use MergeCoins, whose gasless
// eligibility is unconfirmed by Sui — don't wire them until confirmed; A and single-coin
// B are the confirmed shapes.
func (p *ProgrammableTransactionBuilder) GaslessTransfer(
	sender SuiAddress,
	recipient SuiAddress,
	coins []*ObjectRef,
	totalAmount uint64,
	withdrawalAmount uint64,
	coinType move_types.TypeTag,
) error {
	if totalAmount == 0 {
		return fmt.Errorf("totalAmount must be non-zero")
	}

	// Case A: whole amount from the address balance — redeem Balance<T>, send to recipient.
	if len(coins) == 0 {
		if withdrawalAmount != totalAmount {
			return fmt.Errorf(
				"address-balance-only gasless transfer requires withdrawalAmount (%d) == totalAmount (%d)",
				withdrawalAmount,
				totalAmount,
			)
		}
		balance := p.redeemFunds("balance", withdrawalAmount, coinType)
		_, err := p.BalanceSendFunds(balance, recipient, coinType)
		return err
	}

	// Cases B & C: collect coin objects (Case C also redeems the shortfall as a mergeable
	// Coin<T>), merge, split out the amount, and route split + remainder to address balances.
	coinArgs := make([]Argument, 0, len(coins)+1)
	for _, c := range coins {
		coinArg, err := p.Obj(ObjectArg{ImmOrOwnedObject: c})
		if err != nil {
			return err
		}
		coinArgs = append(coinArgs, coinArg)
	}
	if withdrawalAmount > 0 {
		coinArgs = append(coinArgs, p.redeemFunds("coin", withdrawalAmount, coinType))
	}

	base := coinArgs[0]
	if len(coinArgs) > 1 {
		p.Command(
			Command{
				MergeCoins: &MergeCoinsCommand{Argument: base, Arguments: coinArgs[1:]},
			},
		)
	}

	amtArg, err := p.Pure(totalAmount)
	if err != nil {
		return err
	}
	splitResult := p.Command(
		Command{
			SplitCoins: &SplitCoinsCommand{Argument: base, Arguments: []Argument{amtArg}},
		},
	)
	if splitResult.Result == nil {
		return errors.New("self.command should always give a Argument::Result")
	}
	splitCoin := Argument{
		NestedResult: &NestedResultArgument{Result1: *splitResult.Result, Result2: 0},
	}

	if _, err := p.CoinSendFunds(splitCoin, recipient, coinType); err != nil {
		return err
	}
	// Return the merged remainder (possibly zero) to sender's address balance so no owned
	// coin survives. ASSUMPTION: coin::send_funds accepts a zero-value coin — unverified.
	if _, err := p.CoinSendFunds(base, sender, coinType); err != nil {
		return err
	}
	return nil
}
