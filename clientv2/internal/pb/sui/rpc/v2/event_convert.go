package rpcv2

import (
	"fmt"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/types"
)

// ToInternalType converts TransactionEvents into the internal event list;
// event sequence numbers are the event's index within the transaction.
// Unparseable events are dropped and reported in the error slice. txDigest is
// the enclosing transaction's digest — TransactionEvents.Digest is the digest
// of the events themselves.
func (x *TransactionEvents) ToInternalType(txDigest string) ([]types.SuiEvent, []error) {
	if x == nil {
		return nil, nil
	}
	var errs []error
	out := make([]types.SuiEvent, 0, len(x.GetEvents()))
	for i, event := range x.GetEvents() {
		packageID, err := parseAddress(event.GetPackageId())
		if err != nil {
			errs = append(errs, fmt.Errorf("event %d: package: %w", i, err))
			continue
		}
		sender, err := parseAddress(event.GetSender())
		if err != nil {
			errs = append(errs, fmt.Errorf("event %d: sender: %w", i, err))
			continue
		}
		out = append(out, types.SuiEvent{
			Id: types.EventId{
				TxDigest: parseDigest(txDigest),
				EventSeq: types.NewSafeSuiBigInt(uint64(i)),
			},
			PackageId:         packageID,
			TransactionModule: event.GetModule(),
			Sender:            sender,
			Type:              normalizeTypeString(event.GetEventType()),
			ParsedJson:        event.GetJson().AsInterface(),
			Bcs:               lib.Base58(event.GetContents().GetValue()).String(),
		})
	}
	return out, errs
}
