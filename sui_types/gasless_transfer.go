package sui_types

import (
	"errors"
	"fmt"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
)

// Gasless stablecoin transfers (Sui SIP-58 address balances): the PTB writes no owned
// objects — every coin is destroyed into an address balance via send_funds — so it can
// broadcast with gasBudget=0 and no gas payment. The shapes are unverified on-chain
// pending a testnet DryRun (computationCost=0).

// BalanceSendFunds appends balance::send_funds<T>, crediting balance to recipient's address balance.
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

// CoinSendFunds appends coin::send_funds<T>, destroying coin and crediting it to recipient's address balance.
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

// redeemFunds appends redeem_funds<T> on module "balance" (->Balance<T>) or "coin" (->Coin<T>),
// reserving withdrawalAmount from the sender's address balance.
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
// recipient. With no coins it pays from the sender's address balance alone; otherwise it
// consumes the supplied coins, redeems any shortfall from the address balance, and routes
// the change back to the sender's balance so no owned object survives. The caller
// guarantees coins + withdrawalAmount >= totalAmount. (The balance-vs-coins choice is the
// caller's; see the backend's PickCoins.)
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
	if len(coins) == 0 {
		return p.gaslessTransferFromBalance(recipient, totalAmount, withdrawalAmount, coinType)
	}
	return p.gaslessTransferFromCoins(sender, recipient, coins, totalAmount, withdrawalAmount, coinType)
}

// gaslessTransferFromBalance pays the whole amount from the sender's settled address
// balance: redeem a Balance<T> and credit it to the recipient (no coins involved).
func (p *ProgrammableTransactionBuilder) gaslessTransferFromBalance(
	recipient SuiAddress,
	totalAmount uint64,
	withdrawalAmount uint64,
	coinType move_types.TypeTag,
) error {
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

// gaslessTransferFromCoins consolidates the supplied coins (plus any redeemed shortfall)
// into one coin, splits out totalAmount for the recipient, and returns the remainder (which
// may be zero) to the sender — all via coin::send_funds, so no coin object survives.
func (p *ProgrammableTransactionBuilder) gaslessTransferFromCoins(
	sender SuiAddress,
	recipient SuiAddress,
	coins []*ObjectRef,
	totalAmount uint64,
	withdrawalAmount uint64,
	coinType move_types.TypeTag,
) error {
	base, err := p.mergeGaslessCoins(coins, withdrawalAmount, coinType)
	if err != nil {
		return err
	}
	recipientCoin, err := p.splitCoinAmount(base, totalAmount)
	if err != nil {
		return err
	}
	if _, err := p.CoinSendFunds(recipientCoin, recipient, coinType); err != nil {
		return err
	}
	_, err = p.CoinSendFunds(base, sender, coinType)
	return err
}

// mergeGaslessCoins references every coin object and redeems any shortfall as a Coin<T>,
// returning a single "base" coin holding their combined value (it emits a MergeCoins only
// when there is more than one source).
func (p *ProgrammableTransactionBuilder) mergeGaslessCoins(
	coins []*ObjectRef,
	withdrawalAmount uint64,
	coinType move_types.TypeTag,
) (Argument, error) {
	coinArgs := make([]Argument, 0, len(coins)+1)
	for _, c := range coins {
		coinArg, err := p.Obj(ObjectArg{ImmOrOwnedObject: c})
		if err != nil {
			return Argument{}, err
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
	return base, nil
}

// splitCoinAmount splits amount off source, returning the new coin holding exactly amount;
// the remainder stays in source.
func (p *ProgrammableTransactionBuilder) splitCoinAmount(
	source Argument,
	amount uint64,
) (Argument, error) {
	amtArg, err := p.Pure(amount)
	if err != nil {
		return Argument{}, err
	}
	splitResult := p.Command(
		Command{
			SplitCoins: &SplitCoinsCommand{Argument: source, Arguments: []Argument{amtArg}},
		},
	)
	if splitResult.Result == nil {
		return Argument{}, errors.New("self.command should always give a Argument::Result")
	}
	return Argument{
		NestedResult: &NestedResultArgument{Result1: *splitResult.Result, Result2: 0},
	}, nil
}
