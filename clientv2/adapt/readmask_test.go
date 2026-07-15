package adapt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/types"
)

func TestObjectReadMaskPaths(t *testing.T) {
	base := []string{"object_id", "version", "digest"}
	tests := []struct {
		name    string
		options *types.SuiObjectDataOptions
		want    []string
	}{
		{
			name:    "nil options fetch only the base fields",
			options: nil,
			want:    base,
		},
		{
			name:    "empty options fetch only the base fields",
			options: &types.SuiObjectDataOptions{},
			want:    base,
		},
		{
			name:    "show type",
			options: &types.SuiObjectDataOptions{ShowType: true},
			want:    append(append([]string{}, base...), "object_type"),
		},
		{
			name:    "show content",
			options: &types.SuiObjectDataOptions{ShowContent: true},
			want:    append(append([]string{}, base...), "object_type", "json", "has_public_transfer"),
		},
		{
			name:    "show bcs",
			options: &types.SuiObjectDataOptions{ShowBcs: true},
			want:    append(append([]string{}, base...), "object_type", "contents", "has_public_transfer"),
		},
		{
			name:    "show owner",
			options: &types.SuiObjectDataOptions{ShowOwner: true},
			want:    append(append([]string{}, base...), "owner"),
		},
		{
			name:    "show previous transaction",
			options: &types.SuiObjectDataOptions{ShowPreviousTransaction: true},
			want:    append(append([]string{}, base...), "previous_transaction"),
		},
		{
			name:    "show storage rebate",
			options: &types.SuiObjectDataOptions{ShowStorageRebate: true},
			want:    append(append([]string{}, base...), "storage_rebate"),
		},
		{
			name:    "show display",
			options: &types.SuiObjectDataOptions{ShowDisplay: true},
			want:    append(append([]string{}, base...), "display"),
		},
		{
			name: "type plus content adds object_type once",
			options: &types.SuiObjectDataOptions{
				ShowType:    true,
				ShowContent: true,
			},
			want: append(append([]string{}, base...), "object_type", "json", "has_public_transfer"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ObjectReadMaskPaths(tt.options))
		})
	}
}

func TestResponseReadMaskPaths(t *testing.T) {
	always := []string{"digest", "checkpoint", "timestamp"}
	tests := []struct {
		name    string
		options types.SuiTransactionBlockResponseOptions
		want    []string
	}{
		{
			name:    "no options still fetch digest checkpoint timestamp",
			options: types.SuiTransactionBlockResponseOptions{},
			want:    always,
		},
		{
			name:    "show input",
			options: types.SuiTransactionBlockResponseOptions{ShowInput: true},
			want:    append(append([]string{}, always...), "transaction.bcs", "signatures"),
		},
		{
			name:    "show raw input",
			options: types.SuiTransactionBlockResponseOptions{ShowRawInput: true},
			want:    append(append([]string{}, always...), "transaction.bcs", "signatures"),
		},
		{
			name: "show input and raw input request the transaction once",
			options: types.SuiTransactionBlockResponseOptions{
				ShowInput:    true,
				ShowRawInput: true,
			},
			want: append(append([]string{}, always...), "transaction.bcs", "signatures"),
		},
		{
			name:    "show effects",
			options: types.SuiTransactionBlockResponseOptions{ShowEffects: true},
			want:    append(append([]string{}, always...), "effects"),
		},
		{
			name:    "show events",
			options: types.SuiTransactionBlockResponseOptions{ShowEvents: true},
			want:    append(append([]string{}, always...), "events"),
		},
		{
			name:    "show balance changes",
			options: types.SuiTransactionBlockResponseOptions{ShowBalanceChanges: true},
			want:    append(append([]string{}, always...), "balance_changes"),
		},
		{
			name:    "show object changes has no gRPC equivalent and is ignored",
			options: types.SuiTransactionBlockResponseOptions{ShowObjectChanges: true},
			want:    always,
		},
		{
			name: "all options",
			options: types.SuiTransactionBlockResponseOptions{
				ShowInput:          true,
				ShowEffects:        true,
				ShowEvents:         true,
				ShowObjectChanges:  true,
				ShowBalanceChanges: true,
				ShowRawInput:       true,
			},
			want: append(append([]string{}, always...),
				"transaction.bcs", "signatures", "effects", "events", "balance_changes"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResponseReadMaskPaths(tt.options))
		})
	}
}

func TestPrefixPaths(t *testing.T) {
	t.Run("simulate transaction prefixing", func(t *testing.T) {
		paths := ResponseReadMaskPaths(types.SuiTransactionBlockResponseOptions{
			ShowEffects: true,
			ShowEvents:  true,
		})
		require.Equal(t, []string{
			"transaction.digest",
			"transaction.checkpoint",
			"transaction.timestamp",
			"transaction.effects",
			"transaction.events",
		}, PrefixPaths("transaction.", paths))
	})

	t.Run("checkpoint transactions prefixing", func(t *testing.T) {
		require.Equal(t,
			[]string{"transactions.digest", "transactions.effects"},
			PrefixPaths("transactions.", []string{"digest", "effects"}),
		)
	})

	t.Run("empty input", func(t *testing.T) {
		require.Empty(t, PrefixPaths("transaction.", nil))
		require.Empty(t, PrefixPaths("transaction.", []string{}))
	})

	t.Run("input slice is not mutated", func(t *testing.T) {
		in := []string{"digest", "effects"}
		_ = PrefixPaths("transaction.", in)
		require.Equal(t, []string{"digest", "effects"}, in)
	})
}
