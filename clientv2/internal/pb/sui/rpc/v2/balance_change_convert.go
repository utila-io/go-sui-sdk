package rpcv2

import (
	"fmt"

	"github.com/utila-io/go-sui-sdk/types"
)

// ToInternalType converts one BalanceChange into the internal shape.
func (x *BalanceChange) ToInternalType() (types.BalanceChange, error) {
	addr, err := parseAddress(x.GetAddress())
	if err != nil {
		return types.BalanceChange{}, fmt.Errorf("owner: %w", err)
	}
	return types.BalanceChange{
		Owner: types.ObjectOwner{
			ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &addr},
		},
		CoinType: normalizeTypeString(x.GetCoinType()),
		Amount:   x.GetAmount(),
	}, nil
}

// toInternalBalanceChanges converts a balance change list, keeping the nil
// (rather than empty) result for an empty input. Changes with an unparseable
// owner are dropped and reported in the error slice.
func toInternalBalanceChanges(changes []*BalanceChange) ([]types.BalanceChange, []error) {
	if len(changes) == 0 {
		return nil, nil
	}
	var errs []error
	out := make([]types.BalanceChange, 0, len(changes))
	for i, change := range changes {
		converted, err := change.ToInternalType()
		if err != nil {
			errs = append(errs, fmt.Errorf("balance change %d: %w", i, err))
			continue
		}
		out = append(out, converted)
	}
	return out, errs
}
