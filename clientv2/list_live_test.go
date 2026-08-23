//go:build live

// Live checks for the checkpoint range scan against a real node, covering what
// mocks cannot: that the streamed checkpoint is identical to the one the unary
// GetCheckpoint returns, multi-round resumption over a span wider than the
// node's per-request limit, and how the node answers ranges outside the data it
// holds -- above the tip, and below its retention watermark.
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
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/utila-io/go-sui-sdk/types"
)

const liveTokenHeader = "x-token"

func liveClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("SUI_GRPC_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SUI_GRPC_ENDPOINT (and SUI_GRPC_TOKEN) to run the live checkpoint-scan checks")
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
	t.Cleanup(func() { require.NoError(t, client.Close()) })
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

// comparableFields renders a checkpoint as a field map with the validator
// signature removed, and returns that signature separately.
//
// The signature is excluded from equality on purpose. It is an aggregated
// validator certificate, and a provider fronting several backend nodes may
// serve two requests from two of them, each having aggregated a different
// (equally valid) subset of validator signatures over the same checkpoint. So
// it is asserted present rather than equal -- the same treatment, for the same
// reason, that the JSON-RPC/gRPC parity suite gives it.
func comparableFields(t *testing.T, checkpoint *types.Checkpoint) (fields map[string]any, signature string) {
	t.Helper()
	encoded, err := json.Marshal(checkpoint)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &fields))
	signature, _ = fields["validatorSignature"].(string)
	delete(fields, "validatorSignature")
	return fields, signature
}

// TestLiveGetCheckpointsMatchesUnary is the behaviour-preserving check for
// serving GetCheckpoints from a stream: every checkpoint it yields must equal
// the one the unary GetCheckpoint returns for the same sequence number. The two
// share a conversion, so a divergence means the streaming read mask or the
// frames themselves carry different data -- a silent regression for consumers.
func TestLiveGetCheckpointsMatchesUnary(t *testing.T) {
	client := liveClient(t)
	ctx := liveScanCtx(t)
	const span = 3
	start := liveStart(t, ctx, client, span)

	streamed, err := client.GetCheckpoints(ctx, start, span)
	require.NoError(t, err)
	require.Len(t, streamed, span)

	var differingSignatures int
	for _, want := range streamed {
		seqNum := want.SequenceNumber.Uint64()
		got, err := client.GetCheckpoint(ctx, seqNum)
		require.NoError(t, err)

		wantFields, wantSignature := comparableFields(t, want)
		gotFields, gotSignature := comparableFields(t, got)
		require.Equal(t, wantFields, gotFields,
			"checkpoint %d differs between the stream and the unary read", seqNum)
		require.NotEmpty(t, wantSignature, "checkpoint %d streamed without a validator signature", seqNum)
		require.NotEmpty(t, gotSignature, "checkpoint %d read without a validator signature", seqNum)
		if wantSignature != gotSignature {
			differingSignatures++
		}
	}
	// Not a failure, but worth surfacing: it means the two calls were served by
	// different backend nodes, so this run genuinely spanned replicas.
	if differingSignatures > 0 {
		t.Logf("%d/%d checkpoints carried a different validator certificate between the two calls; "+
			"the endpoint fronts multiple nodes", differingSignatures, len(streamed))
	}
}

// TestLiveGetCheckpointsPastTip pins what the node does above the chain tip,
// the symmetric case to pruning below the watermark: the range must truncate to
// what exists rather than erroring, since the checkpoints are merely not
// produced yet.
func TestLiveGetCheckpointsPastTip(t *testing.T) {
	client := liveClient(t)
	ctx := liveScanCtx(t)
	tip, err := client.GetLatestCheckpointSequenceNumber(ctx)
	require.NoError(t, err)

	// Straddling the tip: the prefix that exists comes back, the rest does not.
	straddling, err := client.GetCheckpoints(ctx, tip-3, 10)
	require.NoError(t, err)
	require.NotEmpty(t, straddling, "the part of the range below the tip must still be returned")
	require.Less(t, len(straddling), 10, "checkpoints above the tip cannot exist yet")
	requireContiguous(t, straddling, tip-3)

	// Entirely above the tip: empty, and still not an error.
	beyond, err := client.GetCheckpoints(ctx, tip+1000, 5)
	require.NoError(t, err)
	require.Empty(t, beyond)
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
