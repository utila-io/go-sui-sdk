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
