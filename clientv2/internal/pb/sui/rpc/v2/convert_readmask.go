package rpcv2

import "github.com/utila-io/go-sui-sdk/types"

// Read masks tell the node which fields to populate. Each one lists exactly
// the paths its conversion needs, so the shape a caller asks for and the
// fields fetched over the wire stay in step.

// CheckpointReadMaskPaths fetches everything needed to fill a
// types.Checkpoint, including per-transaction digests.
var CheckpointReadMaskPaths = []string{
	"sequence_number", "digest", "summary", "signature", "transactions.digest",
}

// CoinReadMaskPaths is the ListOwnedObjects read mask needed to build a
// types.Coin from each returned object.
var CoinReadMaskPaths = []string{
	"object_id", "version", "digest", "object_type", "balance", "previous_transaction",
}

// ObjectReadMaskPaths maps SuiObjectDataOptions to the Object read mask paths
// needed to populate the corresponding SuiObjectData fields. object_id,
// version and digest are always fetched.
func ObjectReadMaskPaths(options *types.SuiObjectDataOptions) []string {
	paths := []string{"object_id", "version", "digest"}
	if options == nil {
		return paths
	}
	if options.ShowType || options.ShowContent || options.ShowBcs {
		paths = append(paths, "object_type")
	}
	if options.ShowContent {
		paths = append(paths, "json", "has_public_transfer")
	}
	if options.ShowBcs {
		paths = append(paths, "contents", "has_public_transfer")
	}
	if options.ShowOwner {
		paths = append(paths, "owner")
	}
	if options.ShowPreviousTransaction {
		paths = append(paths, "previous_transaction")
	}
	if options.ShowStorageRebate {
		paths = append(paths, "storage_rebate")
	}
	if options.ShowDisplay {
		paths = append(paths, "display")
	}
	return paths
}

// ResponseReadMaskPaths maps response options to ExecutedTransaction read mask
// paths. digest, checkpoint and timestamp are requested unconditionally
// because callers get them without asking, so no option gates them.
// ShowObjectChanges is derived from the effects and the transaction BCS (its
// per-entry sender), so it fetches both.
func ResponseReadMaskPaths(options types.SuiTransactionBlockResponseOptions) []string {
	paths := []string{"digest", "checkpoint", "timestamp"}
	if options.ShowInput || options.ShowRawInput {
		paths = append(paths, "transaction.bcs", "signatures")
	} else if options.ShowObjectChanges {
		paths = append(paths, "transaction.bcs")
	}
	if options.ShowEffects || options.ShowObjectChanges {
		paths = append(paths, "effects")
	}
	if options.ShowEvents {
		paths = append(paths, "events")
	}
	if options.ShowBalanceChanges {
		paths = append(paths, "balance_changes")
	}
	return paths
}

// PrefixPaths rebases read mask paths onto a nested message, e.g.
// "transactions." for GetCheckpoint or "transaction." for SimulateTransaction.
func PrefixPaths(prefix string, paths []string) []string {
	prefixed := make([]string, len(paths))
	for i, path := range paths {
		prefixed[i] = prefix + path
	}
	return prefixed
}
