package rpcv2

import (
	"encoding/base64"

	"github.com/utila-io/go-sui-sdk/types"
)

// ToInternalType converts a Checkpoint (with summary, signature and
// transaction digests) into the internal checkpoint shape.
func (x *Checkpoint) ToInternalType() *types.Checkpoint {
	summary := x.GetSummary()
	out := &types.Checkpoint{
		Epoch:                      types.NewSafeSuiBigInt(summary.GetEpoch()),
		SequenceNumber:             types.NewSafeSuiBigInt(x.GetSequenceNumber()),
		Digest:                     parseDigest(x.GetDigest()),
		NetworkTotalTransactions:   types.NewSafeSuiBigInt(summary.GetTotalNetworkTransactions()),
		EpochRollingGasCostSummary: summary.GetEpochRollingGasCostSummary().ToInternalType(),
	}
	if summary.GetTimestamp() != nil {
		out.TimestampMs = types.NewSafeSuiBigInt(uint64(summary.GetTimestamp().AsTime().UnixMilli()))
	}
	if summary.GetPreviousDigest() != "" {
		previousDigest := parseDigest(summary.GetPreviousDigest())
		out.PreviousDigest = &previousDigest
	}
	for _, tx := range x.GetTransactions() {
		digest := parseDigest(tx.GetDigest())
		out.Transactions = append(out.Transactions, &digest)
	}
	if signature := x.GetSignature(); signature != nil {
		out.ValidatorSignature = base64.StdEncoding.EncodeToString(signature.GetSignature())
	}
	return out
}
