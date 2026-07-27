//go:build live

// Live checks for the streaming List* RPCs against a real node, including the
// two behaviours mocks cannot pin down: multi-round cursor resumption and how
// the node answers a request below its retention watermark.
//
// Run with:
//
//	SUI_GRPC_ENDPOINT=grpc://host:port \
//	SUI_GRPC_TOKEN=<token> \
//	go test -tags live ./clientv2/ -run TestLive -v
//
// Env:
//   - SUI_GRPC_ENDPOINT       required; "grpc://host[:port]" or "host[:port]"
//   - SUI_GRPC_TOKEN          sent as a per-RPC header when set
//   - SUI_GRPC_TOKEN_HEADER   header carrying the token (default x-token)
//   - SUI_GRPC_INSECURE       any value dials plaintext instead of TLS
//
// The tests skip when SUI_GRPC_ENDPOINT is unset.
package clientv2

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/utila-io/go-sui-sdk/types"
)

const defaultTokenHeader = "x-token"

func liveClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("SUI_GRPC_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SUI_GRPC_ENDPOINT (and SUI_GRPC_TOKEN) to run the live List* checks")
	}
	var dialOpts []grpc.DialOption
	if token := os.Getenv("SUI_GRPC_TOKEN"); token != "" {
		header := os.Getenv("SUI_GRPC_TOKEN_HEADER")
		if header == "" {
			header = defaultTokenHeader
		}
		dialOpts = append(dialOpts, WithHeaders(map[string]string{header: token}))
	}
	if os.Getenv("SUI_GRPC_INSECURE") != "" {
		dialOpts = append(dialOpts, WithInsecure())
	}

	client, err := NewClient(endpoint, dialOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// liveStart returns a checkpoint far enough below the tip that a span of
// checkpoints above it is guaranteed to be executed and indexed.
func liveStart(t *testing.T, ctx context.Context, client *Client, span int) uint64 {
	t.Helper()
	tip, err := client.GetLatestCheckpointSequenceNumber(ctx)
	require.NoError(t, err)
	// 20 checkpoints of headroom: the indexed tip can trail the executed tip.
	behind := uint64(span) + 20
	require.Greater(t, tip, behind, "chain tip %d too low for a %d-checkpoint span", tip, span)
	t.Logf("chain tip %d", tip)
	return tip - behind
}

func requireContiguous(t *testing.T, checkpoints []*types.Checkpoint, start uint64) {
	t.Helper()
	for i, checkpoint := range checkpoints {
		require.Equal(t, start+uint64(i), checkpoint.SequenceNumber.Uint64(),
			"checkpoint %d out of order or missing", start+uint64(i))
		require.NotEmpty(t, checkpoint.Digest.String())
	}
}

func TestLiveListCheckpoints(t *testing.T) {
	client := liveClient(t)
	ctx := liveCtx(t)
	const limit = 5
	start := liveStart(t, ctx, client, limit)

	checkpoints, err := client.GetCheckpoints(ctx, start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit)
	requireContiguous(t, checkpoints, start)
	for _, checkpoint := range checkpoints {
		t.Logf("checkpoint %d: %s, %d transactions, epoch %d",
			checkpoint.SequenceNumber.Uint64(), checkpoint.Digest,
			len(checkpoint.Transactions), checkpoint.Epoch.Uint64())
	}
}

// A span wider than the node's per-request item limit forces the scan to
// resume, which is the part mocks cannot verify.
func TestLiveListCheckpointsResumes(t *testing.T) {
	client := liveClient(t)
	ctx := liveCtx(t)
	const limit = 250
	start := liveStart(t, ctx, client, limit)

	checkpoints, err := client.GetCheckpoints(ctx, start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit, "a resumed scan must still cover the whole range")
	requireContiguous(t, checkpoints, start)
}

// requireCoversCheckpoints asserts the transactions are exactly the ones the
// checkpoints themselves list, grouped in ascending checkpoint order. This is
// the completeness check: a resume boundary that skipped or repeated a
// transaction shows up here.
func requireCoversCheckpoints(
	t *testing.T,
	transactions []*types.SuiTransactionBlockResponse,
	checkpoints []*types.Checkpoint,
) {
	t.Helper()
	expected := make(map[string]uint64)
	for _, checkpoint := range checkpoints {
		for _, digest := range checkpoint.Transactions {
			expected[digest.String()] = checkpoint.SequenceNumber.Uint64()
		}
	}

	seen := make(map[string]bool, len(transactions))
	var previous uint64
	for _, transaction := range transactions {
		digest := transaction.Digest.String()
		require.False(t, seen[digest], "transaction %s returned twice", digest)
		seen[digest] = true

		wantCheckpoint, ok := expected[digest]
		require.True(t, ok, "transaction %s is not listed by any checkpoint in the range", digest)
		require.NotNil(t, transaction.Checkpoint, "transaction %s has no checkpoint", digest)
		require.Equal(t, wantCheckpoint, transaction.Checkpoint.Uint64(),
			"transaction %s attributed to the wrong checkpoint", digest)
		require.GreaterOrEqual(t, transaction.Checkpoint.Uint64(), previous,
			"transactions are not grouped in ascending checkpoint order")
		previous = transaction.Checkpoint.Uint64()
	}
	require.Len(t, transactions, len(expected), "range is missing transactions the checkpoints list")
}

func TestLiveListTransactions(t *testing.T) {
	client := liveClient(t)
	ctx := liveCtx(t)
	const span = 3
	start := liveStart(t, ctx, client, span)

	checkpoints, err := client.GetCheckpoints(ctx, start, span)
	require.NoError(t, err)
	require.Len(t, checkpoints, span)

	options := types.SuiTransactionBlockResponseOptions{ShowEffects: true, ShowBalanceChanges: true}
	transactions, err := client.ListTransactions(ctx, start, start+span, options)
	require.NoError(t, err)
	t.Logf("checkpoints [%d, %d): %d transactions", start, start+span, len(transactions))
	requireCoversCheckpoints(t, transactions, checkpoints)

	// The single-checkpoint wrapper must agree with the range scan.
	first, err := client.GetCheckpointTransactions(ctx, start, options)
	require.NoError(t, err)
	requireCoversCheckpoints(t, first, checkpoints[:1])
}

// A span holding more transactions than the node returns per request forces the
// cursor resume path; completeness proves the boundary neither skipped nor
// repeated a transaction.
func TestLiveListTransactionsResumes(t *testing.T) {
	client := liveClient(t)
	ctx := liveCtx(t)
	const span = 60
	start := liveStart(t, ctx, client, span)

	checkpoints, err := client.GetCheckpoints(ctx, start, span)
	require.NoError(t, err)
	require.Len(t, checkpoints, span)
	expected := 0
	for _, checkpoint := range checkpoints {
		expected += len(checkpoint.Transactions)
	}

	transactions, err := client.ListTransactions(ctx, start, start+span,
		types.SuiTransactionBlockResponseOptions{ShowEffects: true})
	require.NoError(t, err)
	t.Logf("checkpoints [%d, %d): %d transactions across %d checkpoints",
		start, start+span, len(transactions), span)
	requireCoversCheckpoints(t, transactions, checkpoints)
	require.Equal(t, expected, len(transactions))
}

// TestLiveListCheckpointsFromGenesis probes what the node does below its
// retention watermark. GetCheckpoints promises either the checkpoints or a
// "pruned" error; anything else (a raw gRPC status, a silently short range)
// means its classification needs adjusting for this node.
func TestLiveListCheckpointsFromGenesis(t *testing.T) {
	client := liveClient(t)
	ctx := liveCtx(t)
	const limit = 3

	checkpoints, err := client.GetCheckpoints(ctx, 0, limit)
	if err != nil {
		require.ErrorContains(t, err, "pruned; node retains from",
			"unexpected error shape for a pruned range: %v", err)
		t.Logf("node is pruned: %v", err)
		return
	}
	require.Len(t, checkpoints, limit, "archival node returned a short range without an error")
	requireContiguous(t, checkpoints, 0)
	t.Log("node is archival: genesis checkpoints are still served")
}
