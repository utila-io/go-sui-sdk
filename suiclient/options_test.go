package suiclient

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/utila-io/go-sui-sdk/clientv2"
)

// unreachableEndpoint points at a port that is essentially guaranteed to be
// closed. New must still succeed: construction is lazy and performs no
// network I/O.
const unreachableEndpoint = "127.0.0.1:1"

// callerConn returns a caller-owned gRPC connection for WithGRPCConn cases.
func callerConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(unreachableEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return conn
}

// TestNewIsLazy constructs against a closed port without error, for either
// transport (plaintext or TLS).
func TestNewIsLazy(t *testing.T) {
	cases := []struct {
		name string
		opts []Option
	}{
		{name: "default over TLS"},
		{name: "insecure over plaintext", opts: []Option{WithInsecure()}},
		{name: "with header", opts: []Option{WithHeader("x-api-key", "k123")}},
		{name: "with dial options", opts: []Option{WithGRPCDialOptions(grpc.WithUserAgent("x"))}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, err := New(unreachableEndpoint, c.opts...)
			require.NoError(t, err)
			require.IsType(t, &clientv2.Client{}, client)
			require.NoError(t, client.Close())
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
			name: "grpc conn with dial options",
			opts: func(t *testing.T) []Option {
				return []Option{WithGRPCConn(callerConn(t)), WithGRPCDialOptions(grpc.WithUserAgent("x"))}
			},
			wantErrContains: "mutually exclusive",
		},
		{
			name: "grpc conn with header",
			opts: func(t *testing.T) []Option {
				return []Option{WithGRPCConn(callerConn(t)), WithHeader("x-api-key", "k123")}
			},
			wantErrContains: "WithHeader",
		},
	}
	for _, key := range []string{"", ":authority", "grpc-timeout"} {
		cases = append(cases, struct {
			name            string
			endpoint        string
			opts            func(t *testing.T) []Option
			wantErrContains string
		}{
			name:     fmt.Sprintf("invalid header key %q", key),
			endpoint: unreachableEndpoint,
			opts:     func(*testing.T) []Option { return []Option{WithHeader(key, "v")} },
		})
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
	c, err := New("", WithGRPCConn(conn))
	require.NoError(t, err)
	require.IsType(t, &clientv2.Client{}, c)

	// The connection is caller-owned: Close on the client must not shut it down.
	require.NoError(t, c.Close())
	require.NotEqual(t, connectivity.Shutdown, conn.GetState())
}

// TestNewWithHeader pins that headers ride every request as metadata, and the
// last write per key wins, case-insensitively.
func TestNewWithHeader(t *testing.T) {
	cases := []struct {
		name    string
		headers [][2]string
		want    map[string][]string
	}{
		{
			name:    "headers sent on requests",
			headers: [][2]string{{"Authorization", "Bearer sekret"}, {"x-api-key", "k123"}},
			want: map[string][]string{
				"authorization": {"Bearer sekret"},
				"x-api-key":     {"k123"},
			},
		},
		{
			name:    "last write per key wins case-insensitively",
			headers: [][2]string{{"X-Token", "old"}, {"x-token", "new"}},
			want:    map[string][]string{"x-token": {"new"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotMD := make(chan metadata.MD, 1)
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
				md, _ := metadata.FromIncomingContext(stream.Context())
				gotMD <- md
				return status.Error(codes.Unimplemented, "capture only")
			}))
			go func() { _ = server.Serve(lis) }()
			defer server.Stop()

			opts := []Option{WithInsecure()}
			for _, h := range c.headers {
				opts = append(opts, WithHeader(h[0], h[1]))
			}
			client, err := New(lis.Addr().String(), opts...)
			require.NoError(t, err)
			defer func() { require.NoError(t, client.Close()) }()

			_, err = client.GetLatestCheckpointSequenceNumber(context.Background())
			require.Equal(t, codes.Unimplemented, status.Code(err))
			md := <-gotMD
			for key, values := range c.want {
				require.Equal(t, values, md.Get(key), "header %s", key)
			}
		})
	}
}
