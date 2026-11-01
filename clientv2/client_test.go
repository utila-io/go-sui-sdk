package clientv2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		target   string
		wantErr  bool
	}{
		{endpoint: "grpc://fullnode.mainnet.sui.io:443", target: "fullnode.mainnet.sui.io:443"},
		{endpoint: "grpc://fullnode.mainnet.sui.io", target: "fullnode.mainnet.sui.io:443"},
		{endpoint: "grpc://node.example:9000/", target: "node.example:9000"},
		{endpoint: "fullnode.mainnet.sui.io", target: "fullnode.mainnet.sui.io:443"},
		{endpoint: "fullnode.mainnet.sui.io:443", target: "fullnode.mainnet.sui.io:443"},
		{endpoint: "my-node.provider.example:9000", target: "my-node.provider.example:9000"},
		// loopback is not special: TLS default applies there too
		{endpoint: "127.0.0.1:9000", target: "127.0.0.1:9000"},
		{endpoint: "localhost:9000", target: "localhost:9000"},
		{endpoint: "localhost", target: "localhost:443"},
		{endpoint: "", wantErr: true},
		{endpoint: "grpc://", wantErr: true},
		{endpoint: "https://fullnode.mainnet.sui.io:443", wantErr: true},
		{endpoint: "https://fullnode.mainnet.sui.io", wantErr: true},
		{endpoint: "http://localhost:9000", wantErr: true},
		{endpoint: "ftp://x", wantErr: true},
		// URL paths and parameters make garbage dial targets
		{endpoint: "fullnode.mainnet.sui.io:443/rpc", wantErr: true},
		{endpoint: "fullnode.mainnet.sui.io/rpc", wantErr: true},
		{endpoint: "grpc://node.example:9000/rpc", wantErr: true},
		{endpoint: "grpc://node.example:9000/rpc/", wantErr: true},
		{endpoint: "node.example:9000?tls=off", wantErr: true},
		{endpoint: "node.example:9000#frag", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.endpoint, func(t *testing.T) {
			target, err := parseEndpoint(c.endpoint)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.target, target)
		})
	}
}

// NewClient surfaces parse failures with actionable messages.
func TestNewClientRejectsBadEndpoint(t *testing.T) {
	cases := []struct {
		endpoint        string
		wantErrContains string
	}{
		{"https://fullnode.mainnet.sui.io:443", "grpc://host:port or host:port"},
		{"grpc://node.example:9000/rpc", "must be host:port"},
	}
	for _, c := range cases {
		t.Run(c.endpoint, func(t *testing.T) {
			_, err := NewClient(c.endpoint)
			require.ErrorContains(t, err, c.wantErrContains)
		})
	}
}
