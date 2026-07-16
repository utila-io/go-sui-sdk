package suiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/utila-io/go-sui-sdk/clientv2"
)

// unreachableEndpoint points at a port that is essentially guaranteed to be
// closed. New must still succeed for both backends: construction is lazy and
// performs no network I/O.
const unreachableEndpoint = "127.0.0.1:1"

func backendOf(t *testing.T, c SuiClient) Backend {
	t.Helper()
	switch c.(type) {
	case *jsonrpcBackend:
		return BackendJSONRPC
	case *clientv2.Client:
		return BackendGRPC
	default:
		t.Fatalf("unexpected SuiClient concrete type %T", c)
		return 0
	}
}

// newAndClose constructs a client against an unreachable endpoint (must not
// error: that is the construction-laziness assertion) and returns its backend.
func newAndClose(t *testing.T, opts ...Option) Backend {
	t.Helper()
	c, err := New(unreachableEndpoint, opts...)
	require.NoError(t, err)
	backend := backendOf(t, c)
	require.NoError(t, c.Close())
	return backend
}

// callerConn returns a caller-owned gRPC connection for WithGRPCConn cases.
func callerConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(unreachableEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestNewBackendSelection pins both backend selection precedence and
// construction laziness: every case constructs against a closed port via
// newAndClose without error, for either transport (plaintext or TLS).
func TestNewBackendSelection(t *testing.T) {
	cases := []struct {
		name string
		opts func(t *testing.T) []Option
		want Backend
	}{
		{
			name: "default is jsonrpc",
			opts: func(*testing.T) []Option { return nil },
			want: BackendJSONRPC,
		},
		{
			name: "explicit jsonrpc",
			opts: func(*testing.T) []Option { return []Option{WithBackend(BackendJSONRPC)} },
			want: BackendJSONRPC,
		},
		{
			name: "explicit grpc stays lazy over TLS",
			opts: func(*testing.T) []Option { return []Option{WithBackend(BackendGRPC)} },
			want: BackendGRPC,
		},
		{
			name: "grpc with insecure stays lazy over plaintext",
			opts: func(*testing.T) []Option { return []Option{WithBackend(BackendGRPC), WithInsecure()} },
			want: BackendGRPC,
		},
		{
			name: "insecure has no effect on jsonrpc",
			opts: func(*testing.T) []Option { return []Option{WithInsecure()} },
			want: BackendJSONRPC,
		},
		{
			name: "grpc with header stays lazy",
			opts: func(*testing.T) []Option {
				return []Option{WithBackend(BackendGRPC), WithHeader("x-api-key", "k123")}
			},
			want: BackendGRPC,
		},
		{
			name: "grpc conn ignored on jsonrpc",
			opts: func(t *testing.T) []Option {
				return []Option{WithBackend(BackendJSONRPC), WithGRPCConn(callerConn(t))}
			},
			want: BackendJSONRPC,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, newAndClose(t, c.opts(t)...))
		})
	}
}

func TestBackendString(t *testing.T) {
	cases := []struct {
		backend Backend
		want    string
	}{
		{BackendJSONRPC, "jsonrpc"},
		{BackendGRPC, "grpc"},
		{Backend(42), "unknown"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			require.Equal(t, c.want, c.backend.String())
		})
	}
}

// TestNewErrors pins options that cannot be honored: New errors immediately
// (not at first RPC) and returns a nil client.
func TestNewErrors(t *testing.T) {
	cases := []struct {
		name            string
		endpoint        string
		opts            func(t *testing.T) []Option
		wantErrContains string
	}{
		{
			name:            "unknown backend",
			endpoint:        unreachableEndpoint,
			opts:            func(*testing.T) []Option { return []Option{WithBackend(Backend(42))} },
			wantErrContains: "unknown backend",
		},
		{
			name: "grpc conn with dial options",
			opts: func(t *testing.T) []Option {
				return []Option{WithBackend(BackendGRPC), WithGRPCConn(callerConn(t)),
					WithGRPCDialOptions(grpc.WithUserAgent("x"))}
			},
			wantErrContains: "mutually exclusive",
		},
		{
			name: "grpc conn with header",
			opts: func(t *testing.T) []Option {
				return []Option{WithBackend(BackendGRPC), WithGRPCConn(callerConn(t)),
					WithHeader("x-api-key", "k123")}
			},
			wantErrContains: "WithHeader",
		},
	}
	// Invalid header keys error from New on both backends.
	for _, key := range []string{"", ":authority", "grpc-timeout"} {
		for _, backend := range []Backend{BackendJSONRPC, BackendGRPC} {
			cases = append(cases, struct {
				name            string
				endpoint        string
				opts            func(t *testing.T) []Option
				wantErrContains string
			}{
				name:     fmt.Sprintf("invalid header key %q on %s", key, backend),
				endpoint: unreachableEndpoint,
				opts: func(*testing.T) []Option {
					return []Option{WithBackend(backend), WithHeader(key, "v")}
				},
			})
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, err := New(c.endpoint, c.opts(t)...)
			require.Error(t, err)
			require.Nil(t, client)
			if c.wantErrContains != "" {
				require.ErrorContains(t, err, c.wantErrContains)
			}
		})
	}
}

func TestNewWithGRPCConn(t *testing.T) {
	conn := callerConn(t)
	c, err := New("", WithBackend(BackendGRPC), WithGRPCConn(conn))
	require.NoError(t, err)
	require.Equal(t, BackendGRPC, backendOf(t, c))

	// The connection is caller-owned: Close on the client must not shut it down.
	require.NoError(t, c.Close())
	require.NotEqual(t, connectivity.Shutdown, conn.GetState())
}

// TestNewWithHeaderJSONRPC pins WithHeader on the JSON-RPC backend: headers
// ride every request, and the last write per key wins, case-insensitively.
func TestNewWithHeaderJSONRPC(t *testing.T) {
	cases := []struct {
		name    string
		headers [][2]string
		want    map[string][]string
	}{
		{
			name:    "headers sent on requests",
			headers: [][2]string{{"Authorization", "Bearer sekret"}, {"x-api-key", "k123"}},
			want: map[string][]string{
				"Authorization": {"Bearer sekret"},
				"x-api-key":     {"k123"},
			},
		},
		{
			name:    "last write per key wins case-insensitively",
			headers: [][2]string{{"X-Token", "old"}, {"x-token", "new"}},
			want:    map[string][]string{"X-Token": {"new"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"123"}`)
			}))
			defer srv.Close()

			opts := make([]Option, 0, len(c.headers))
			for _, h := range c.headers {
				opts = append(opts, WithHeader(h[0], h[1]))
			}
			client, err := New(srv.URL, opts...)
			require.NoError(t, err)
			defer client.Close()

			seq, err := client.GetLatestCheckpointSequenceNumber(context.Background())
			require.NoError(t, err)
			require.Equal(t, "123", seq)
			for key, values := range c.want {
				require.Equal(t, values, got.Values(key), "header %s", key)
			}
		})
	}
}
