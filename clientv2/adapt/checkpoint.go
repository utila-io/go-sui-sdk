package adapt

import (
	"encoding/base64"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

// CheckpointReadMaskPaths fetches everything needed to mirror the JSON-RPC
// sui_getCheckpoint response, including per-transaction digests.
var CheckpointReadMaskPaths = []string{
	"sequence_number", "digest", "summary", "signature", "transactions.digest",
}

// Checkpoint converts a proto Checkpoint (with summary, signature and
// transaction digests) into the JSON-RPC sui_getCheckpoint shape.
func Checkpoint(checkpoint *pb.Checkpoint) *types.Checkpoint {
	summary := checkpoint.GetSummary()
	out := &types.Checkpoint{
		Epoch:                      types.NewSafeSuiBigInt(summary.GetEpoch()),
		SequenceNumber:             types.NewSafeSuiBigInt(checkpoint.GetSequenceNumber()),
		Digest:                     parseDigest(checkpoint.GetDigest()),
		NetworkTotalTransactions:   types.NewSafeSuiBigInt(summary.GetTotalNetworkTransactions()),
		EpochRollingGasCostSummary: gasCostSummary(summary.GetEpochRollingGasCostSummary()),
	}
	if summary.Timestamp != nil {
		out.TimestampMs = types.NewSafeSuiBigInt(uint64(summary.GetTimestamp().AsTime().UnixMilli()))
	}
	if summary.PreviousDigest != nil {
		previousDigest := parseDigest(summary.GetPreviousDigest())
		out.PreviousDigest = &previousDigest
	}
	for _, tx := range checkpoint.GetTransactions() {
		digest := parseDigest(tx.GetDigest())
		out.Transactions = append(out.Transactions, &digest)
	}
	if signature := checkpoint.GetSignature(); signature != nil {
		out.ValidatorSignature = base64.StdEncoding.EncodeToString(signature.GetSignature())
	}
	return out
}
