package suiclient

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/clientv2"
)

// unreachableEndpoint points at a port that is essentially guaranteed to be
// closed. New must still succeed for both backends: construction is lazy and
// performs no network I/O.
const unreachableEndpoint = "127.0.0.1:1"

// backendOf reports which backend a SuiClient was constructed with, based on
// the concrete type New returns.
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

func TestNewDefaultsToJSONRPC(t *testing.T) {
	t.Setenv(BackendEnvVar, "")
	require.Equal(t, BackendJSONRPC, newAndClose(t))
}

func TestNewWithBackend(t *testing.T) {
	t.Setenv(BackendEnvVar, "")
	require.Equal(t, BackendJSONRPC, newAndClose(t, WithBackend(BackendJSONRPC)))
	require.Equal(t, BackendGRPC, newAndClose(t, WithBackend(BackendGRPC)))
}

func TestNewWithBackendOverridesEnv(t *testing.T) {
	t.Setenv(BackendEnvVar, "grpc")
	require.Equal(t, BackendJSONRPC, newAndClose(t, WithBackend(BackendJSONRPC)))

	t.Setenv(BackendEnvVar, "jsonrpc")
	require.Equal(t, BackendGRPC, newAndClose(t, WithBackend(BackendGRPC)))

	// An invalid env value is not even consulted when WithBackend is given.
	t.Setenv(BackendEnvVar, "not-a-backend")
	require.Equal(t, BackendGRPC, newAndClose(t, WithBackend(BackendGRPC)))
}

func TestNewBackendFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		want Backend
	}{
		{"v1", BackendJSONRPC},
		{"jsonrpc", BackendJSONRPC},
		{"V1", BackendJSONRPC},
		{"JSONRPC", BackendJSONRPC},
		{"JsonRpc", BackendJSONRPC},
		{"v2", BackendGRPC},
		{"grpc", BackendGRPC},
		{"V2", BackendGRPC},
		{"GRPC", BackendGRPC},
		{"Grpc", BackendGRPC},
		{"  v2  ", BackendGRPC}, // surrounding whitespace is trimmed
		{"", BackendJSONRPC},    // unset/empty falls back to the default
	}
	for _, tc := range cases {
		t.Run("env="+tc.env, func(t *testing.T) {
			t.Setenv(BackendEnvVar, tc.env)
			require.Equal(t, tc.want, newAndClose(t))
		})
	}
}

func TestNewInvalidEnvBackendErrors(t *testing.T) {
	for _, env := range []string{"v3", "http", "json", "1", "grpc2"} {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv(BackendEnvVar, env)
			c, err := New(unreachableEndpoint)
			require.Error(t, err)
			require.Nil(t, c)
			require.Contains(t, err.Error(), BackendEnvVar)
		})
	}
}

func TestNewUnknownBackendErrors(t *testing.T) {
	c, err := New(unreachableEndpoint, WithBackend(Backend(42)))
	require.Error(t, err)
	require.Nil(t, c)
	require.Contains(t, err.Error(), "unknown backend")
}

func TestBackendString(t *testing.T) {
	require.Equal(t, "jsonrpc", BackendJSONRPC.String())
	require.Equal(t, "grpc", BackendGRPC.String())
	require.Equal(t, "unknown", Backend(42).String())
}

// TestNewIsLazy pins down that New performs no network I/O for either backend:
// constructing against a closed port succeeds, and Close releases cleanly.
func TestNewIsLazy(t *testing.T) {
	t.Setenv(BackendEnvVar, "")
	for _, backend := range []Backend{BackendJSONRPC, BackendGRPC} {
		t.Run(backend.String(), func(t *testing.T) {
			c, err := New(unreachableEndpoint, WithBackend(backend))
			require.NoError(t, err)
			require.NoError(t, c.Close())
		})
	}
}
