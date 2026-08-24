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

// CheckpointOption tunes a checkpoint read.
type CheckpointOption func(*CheckpointReadOptions)

type CheckpointReadOptions struct {
	// Mask limits which checkpoint fields the backend fetches, as proto field
	// paths. Fields left out arrive zeroed, with no way to tell that from a
	// genuine zero, so narrow it only to fields the caller does not read. Empty
	// fetches everything a Checkpoint can hold.
	//
	// Only the gRPC backend can fetch less; JSON-RPC returns the whole
	// checkpoint regardless, so a masked read is a floor, not a guarantee.
	Mask []string
}

// WithMask limits a checkpoint read to the given proto field paths, e.g.
// "sequence_number", "digest", "summary.previous_digest", "transactions.digest".
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
