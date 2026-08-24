package types

import (
	"encoding/json"

	"github.com/utila-io/go-sui-sdk/sui_types"
)

// Checkpoint mirrors the JSON-RPC sui_getCheckpoint response shape.
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

// CheckpointPage is a page of checkpoints as returned by sui_getCheckpoints.
type CheckpointPage = Page[*Checkpoint, SafeSuiBigInt[uint64]]

type CheckpointOption func(*CheckpointReadOptions)

type CheckpointReadOptions struct {
	// Omitted fields arrive zeroed, indistinguishable from a genuine zero, so
	// mask out only fields the caller does not read. Empty fetches everything.
	// JSON-RPC cannot fetch less, so a mask is a floor rather than a guarantee.
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
