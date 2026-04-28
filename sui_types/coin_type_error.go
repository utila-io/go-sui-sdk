package sui_types

import (
	"errors"
	"fmt"
)

// InvalidCoinTypeError reports a malformed coin type string (not "0x<addr>::<module>::<name>").
type InvalidCoinTypeError struct {
	CoinType string
	Msg      string
}

func (e *InvalidCoinTypeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("invalid coin type %q: %s", e.CoinType, e.Msg)
}

// IsInvalidCoinTypeError reports whether err wraps *InvalidCoinTypeError.
func IsInvalidCoinTypeError(err error) bool {
	var target *InvalidCoinTypeError
	return errors.As(err, &target)
}
