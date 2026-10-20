package types

import (
	"encoding/json"

	"github.com/utila-io/go-sui-sdk/sui_types"
)

// Checkpoint is a checkpoint summary with its transaction digests.
type Checkpoint struct {
	Epoch                      SafeSuiBigInt[EpochId]         `json:"epoch"`
	SequenceNumber             SafeSuiBigInt[uint64]          `json:"sequenceNumber"`
	Digest                     sui_types.CheckpointDigest     `json:"digest"`
	NetworkTotalTransactions   SafeSuiBigInt[uint64]          `json:"networkTotalTransactions"`
	PreviousDigest             *sui_types.CheckpointDigest    `json:"previousDigest,omitempty"`
	EpochRollingGasCostSummary GasCostSummary                 `json:"epochRollingGasCostSummary"`
	TimestampMs                SafeSuiBigInt[uint64]          `json:"timestampMs"`
	Transactions               []*sui_types.TransactionDigest `json:"transactions"`
	CheckpointCommitments      []json.RawMessage              `json:"checkpointCommitments,omitempty"`
	ValidatorSignature         string                         `json:"validatorSignature,omitempty"`
	EndOfEpochData             json.RawMessage                `json:"endOfEpochData,omitempty"`
}

type CheckpointOption func(*CheckpointReadOptions)

type CheckpointReadOptions struct {
	// Omitted fields arrive zeroed, indistinguishable from a genuine zero, so
	// mask out only fields the caller does not read. Empty fetches everything.
	Mask []string
}

// WithMask limits the read to proto field paths, e.g. "digest",
// "summary.previous_digest", "transactions.digest".
func WithMask(paths ...string) CheckpointOption {
	return func(o *CheckpointReadOptions) { o.Mask = paths }
}

func NewCheckpointReadOptions(opts ...CheckpointOption) CheckpointReadOptions {
	var options CheckpointReadOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
