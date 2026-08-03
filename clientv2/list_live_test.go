//go:build live

// Live checks for the range scans against a real node, covering what mocks
// cannot: multi-round cursor resumption, whether a scanned range really holds
// exactly the transactions its checkpoints list, how the node answers a range
// below its retention watermark, and whether a resume cursor is portable --
// across a reconnect, and between two simultaneously open connections, which is
// what a load-balanced provider actually does to a paging scan.
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
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/types"
)

const liveTokenHeader = "x-token"

func liveClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("SUI_GRPC_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SUI_GRPC_ENDPOINT (and SUI_GRPC_TOKEN) to run the live range-scan checks")
	}
	var dialOpts []grpc.DialOption
	if token := os.Getenv("SUI_GRPC_TOKEN"); token != "" {
		header := os.Getenv("SUI_GRPC_TOKEN_HEADER")
		if header == "" {
			header = liveTokenHeader
		}
		dialOpts = append(dialOpts, WithHeaders(map[string]string{header: token}))
	}
	if os.Getenv("SUI_GRPC_INSECURE") != "" {
		dialOpts = append(dialOpts, WithInsecure())
	}

	client, err := NewClient(endpoint, dialOpts...)
	require.NoError(t, err)
	// Not asserted: a test may close its client early to prove a point about
	// reconnecting, which makes this second Close a no-op error.
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func liveScanCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// liveStart returns a checkpoint far enough below the tip that a span above it
// is guaranteed to be executed and indexed.
func liveStart(t *testing.T, ctx context.Context, client *Client, span int) uint64 {
	t.Helper()
	tip, err := client.GetLatestCheckpointSequenceNumber(ctx)
	require.NoError(t, err)
	// Headroom: the indexed tip can trail the executed one.
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

func TestLiveGetCheckpoints(t *testing.T) {
	client := liveClient(t)
	ctx := liveScanCtx(t)
	const limit = 5
	start := liveStart(t, ctx, client, limit)

	checkpoints, err := client.GetCheckpoints(ctx, start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit)
	requireContiguous(t, checkpoints, start)
}

// A span wider than the node's per-request item limit forces the scan to
// resume, which is the part mocks cannot verify.
func TestLiveGetCheckpointsResumes(t *testing.T) {
	client := liveClient(t)
	ctx := liveScanCtx(t)
	const limit = 250
	start := liveStart(t, ctx, client, limit)

	checkpoints, err := client.GetCheckpoints(ctx, start, limit)
	require.NoError(t, err)
	require.Len(t, checkpoints, limit, "a resumed scan must still cover the whole range")
	requireContiguous(t, checkpoints, start)
}

// TestLiveGetCheckpointsFromGenesis probes what the node does below its
// retention watermark. GetCheckpoints promises either the checkpoints or a
// "pruned" error; anything else (a raw gRPC status, a silently short range)
// means its classification needs adjusting for this node.
func TestLiveGetCheckpointsFromGenesis(t *testing.T) {
	client := liveClient(t)
	ctx := liveScanCtx(t)
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

// scanRange pages ListTransactions across the whole range, asserting the paging
// contract on the way: HasMore always comes with a cursor, and the cursor always
// advances.
func scanRange(
	t *testing.T, ctx context.Context, client *Client, start, end uint64,
	options types.SuiTransactionBlockResponseOptions,
) []*types.SuiTransactionBlockResponse {
	t.Helper()
	query := types.TransactionRangeQuery{
		StartCheckpoint: &start,
		EndCheckpoint:   &end,
		Limit:           50,
	}
	var (
		transactions []*types.SuiTransactionBlockResponse
		pages        int
	)
	for {
		page, err := client.ListTransactions(ctx, query, options)
		require.NoError(t, err)
		pages++
		require.Less(t, pages, 500, "range [%d, %d) did not terminate", start, end)
		for _, listed := range page.Data {
			require.NotEmpty(t, listed.Cursor, "no resume cursor on %s", listed.Transaction.Digest)
			transactions = append(transactions, listed.Transaction)
		}
		if !page.HasMore {
			t.Logf("checkpoints [%d, %d): %d transactions over %d pages, covered through %s",
				start, end, len(transactions), pages, coveredThrough(page.Checkpoint))
			return transactions
		}
		require.NotEmpty(t, page.NextCursor, "HasMore set with no cursor to resume from")
		require.NotEqual(t, query.Cursor, page.NextCursor, "cursor did not advance")
		query.Cursor = page.NextCursor
	}
}

// coveredThrough renders the fully-covered boundary, which is unset until the
// scan clears its first checkpoint.
func coveredThrough(checkpoint *uint64) string {
	if checkpoint == nil {
		return "none"
	}
	return strconv.FormatUint(*checkpoint, 10)
}

// requireCoversCheckpoints asserts the transactions are exactly the ones the
// checkpoints themselves list, in ascending checkpoint order. This is the
// completeness check: a resume boundary that skipped or repeated a transaction
// shows up here.
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
	ctx := liveScanCtx(t)
	const span = 3
	start := liveStart(t, ctx, client, span)

	checkpoints, err := client.GetCheckpoints(ctx, start, span)
	require.NoError(t, err)
	require.Len(t, checkpoints, span)

	options := types.SuiTransactionBlockResponseOptions{ShowEffects: true, ShowBalanceChanges: true}
	requireCoversCheckpoints(t, scanRange(t, ctx, client, start, start+span, options), checkpoints)

	// The single-checkpoint wrapper must agree with the range scan.
	first, err := client.GetCheckpointTransactions(ctx, start, options)
	require.NoError(t, err)
	requireCoversCheckpoints(t, first, checkpoints[:1])
}

// liveRange picks a range below the tip and returns it alongside the
// checkpoints' own transaction lists, which are the ground truth a scan of that
// range has to reproduce exactly.
func liveRange(
	t *testing.T, ctx context.Context, client *Client, span int,
) (start, end uint64, checkpoints []*types.Checkpoint) {
	t.Helper()
	start = liveStart(t, ctx, client, span)
	checkpoints, err := client.GetCheckpoints(ctx, start, span)
	require.NoError(t, err)
	require.Len(t, checkpoints, span)
	return start, start + uint64(span), checkpoints
}

// nodeFingerprint reports everything a connection can observe about the node
// behind it. Nothing in the API identifies a backend, so two fingerprints that
// match do not prove one node and two that differ do not prove two -- height
// moves on its own. It is logged so a human can judge whether a run plausibly
// spanned replicas at all.
func nodeFingerprint(t *testing.T, ctx context.Context, client *Client) string {
	t.Helper()
	info, err := client.ledger.GetServiceInfo(ctx, &pb.GetServiceInfoRequest{})
	require.NoError(t, err)
	return fmt.Sprintf("server=%q height=%d lowestCheckpoint=%d",
		info.GetServer(), info.GetCheckpointHeight(), info.GetLowestAvailableCheckpoint())
}

// TestLiveCursorCrossesConnections pages one range while alternating between two
// simultaneously open connections, so every cursor is minted on one and redeemed
// on the other. Providers front many backend nodes behind a single hostname and
// may route per request, which is what decides whether a cursor is a ledger
// position or per-node state.
//
// Read a pass as a weak positive, not proof. Nothing in the API identifies the
// backend a connection reached, and gRPC holds one long-lived HTTP/2 connection
// that an L4 balancer routes once, so both connections may well have landed on
// the same node and never exercised the case at all. A single-node endpoint
// passes trivially for the same reason. QuickNode and Alchemy mainnet both
// passed (Aug 2026), which is encouraging and not conclusive. A failure, by
// contrast, is decisive.
//
// It does NOT follow that a cursor can be stored. Neither the proto nor the Sui
// docs promise stability, and sui mainnet-v1.75.2 changed the cursor encoding
// outright, with upstream telling clients to restart pagination on a
// cursor-related error. So a cursor is safe to page with and unsafe to persist
// across a node upgrade; resume a later run from the covered checkpoint instead.
func TestLiveCursorCrossesConnections(t *testing.T) {
	ctx := liveScanCtx(t)
	conns := []*Client{liveClient(t), liveClient(t)}
	for i, conn := range conns {
		t.Logf("connection %d: %s", i, nodeFingerprint(t, ctx, conn))
	}
	start, end, checkpoints := liveRange(t, ctx, conns[0], 6)

	options := types.SuiTransactionBlockResponseOptions{}
	query := types.TransactionRangeQuery{StartCheckpoint: &start, EndCheckpoint: &end, Limit: 5}
	var transactions []*types.SuiTransactionBlockResponse
	for pages := 0; ; pages++ {
		require.Less(t, pages, 200, "interleaved scan did not terminate")
		which := pages % len(conns)
		page, err := conns[which].ListTransactions(ctx, query, options)
		if err != nil {
			t.Skipf("connection %d rejected a cursor minted on the other, so cursors are not portable between connections here: %v",
				which, err)
		}
		for _, listed := range page.Data {
			transactions = append(transactions, listed.Transaction)
		}
		if !page.HasMore {
			t.Logf("%d transactions over %d pages, alternating connection every page; "+
				"evidence only if those connections reached different backends, which nothing here can confirm",
				len(transactions), pages+1)
			break
		}
		require.NotEmpty(t, page.NextCursor, "HasMore set with no cursor to resume from")
		query.Cursor = page.NextCursor
	}
	requireCoversCheckpoints(t, transactions, checkpoints)
}

// TestLiveCursorSurvivesReconnect answers whether a resume cursor may be
// persisted and reused later, which decides whether an indexer can store one
// across restarts rather than resuming from the covered checkpoint. It mints a
// cursor on one connection, closes that connection, and resumes on a freshly
// dialled one; the two halves together must hold exactly the transactions the
// range's checkpoints list.
//
// An outright rejection skips rather than fails: that is the provider answering
// the question -- its cursors are session-scoped, so persisting one is unsafe --
// not a defect in this SDK. Silently skipping or repeating a transaction does
// fail, because that is the failure an indexer cannot detect for itself.
func TestLiveCursorSurvivesReconnect(t *testing.T) {
	ctx := liveScanCtx(t)
	first := liveClient(t)
	start, end, checkpoints := liveRange(t, ctx, first, 6)

	options := types.SuiTransactionBlockResponseOptions{}
	query := types.TransactionRangeQuery{StartCheckpoint: &start, EndCheckpoint: &end, Limit: 5}
	page, err := first.ListTransactions(ctx, query, options)
	require.NoError(t, err)
	require.True(t, page.HasMore, "range [%d, %d) fits in one page, leaving nothing to resume", start, end)
	require.NotEmpty(t, page.NextCursor)

	// Copied the way an indexer would persist it, then the minting connection
	// goes away entirely.
	cursor := append([]byte(nil), page.NextCursor...)
	transactions := make([]*types.SuiTransactionBlockResponse, 0, len(page.Data))
	for _, listed := range page.Data {
		transactions = append(transactions, listed.Transaction)
	}
	require.NoError(t, first.Close())

	second := liveClient(t)
	query.Cursor = cursor
	for pages := 1; ; pages++ {
		require.Less(t, pages, 200, "resumed scan did not terminate")
		resumed, err := second.ListTransactions(ctx, query, options)
		if err != nil {
			t.Skipf("provider rejected a cursor minted on a closed connection, so its cursors are not durable: %v", err)
		}
		for _, listed := range resumed.Data {
			transactions = append(transactions, listed.Transaction)
		}
		if !resumed.HasMore {
			break
		}
		require.NotEmpty(t, resumed.NextCursor, "HasMore set with no cursor to resume from")
		query.Cursor = resumed.NextCursor
	}

	requireCoversCheckpoints(t, transactions, checkpoints)
	t.Logf("cursor survived reconnect: %d transactions across checkpoints [%d, %d) with no gap or repeat at the seam",
		len(transactions), start, end)
}

// A span holding more transactions than the node returns per request forces the
// cursor resume path; completeness proves the boundary neither skipped nor
// repeated a transaction.
func TestLiveListTransactionsResumes(t *testing.T) {
	client := liveClient(t)
	ctx := liveScanCtx(t)
	const span = 60
	start := liveStart(t, ctx, client, span)

	checkpoints, err := client.GetCheckpoints(ctx, start, span)
	require.NoError(t, err)
	require.Len(t, checkpoints, span)
	expected := 0
	for _, checkpoint := range checkpoints {
		expected += len(checkpoint.Transactions)
	}

	transactions := scanRange(t, ctx, client, start, start+span,
		types.SuiTransactionBlockResponseOptions{ShowEffects: true})
	requireCoversCheckpoints(t, transactions, checkpoints)
	require.Len(t, transactions, expected)
}
