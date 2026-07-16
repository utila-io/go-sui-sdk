package clientv2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		target   string
		secure   bool
	}{
		{"https://fullnode.mainnet.sui.io:443", "fullnode.mainnet.sui.io:443", true},
		{"https://fullnode.mainnet.sui.io", "fullnode.mainnet.sui.io:443", true},
		{"fullnode.mainnet.sui.io", "fullnode.mainnet.sui.io:443", true},
		{"fullnode.mainnet.sui.io:443", "fullnode.mainnet.sui.io:443", true},
		// providers serve TLS on non-443 ports; bare non-loopback hosts get TLS
		{"my-node.provider.example:9000", "my-node.provider.example:9000", true},
		{"https://my-node.provider.example:9000", "my-node.provider.example:9000", true},
		// plaintext: explicit http, or bare loopback
		{"http://my-node.provider.example:9000", "my-node.provider.example:9000", false},
		{"127.0.0.1:9000", "127.0.0.1:9000", false},
		{"localhost:9000", "localhost:9000", false},
		{"http://localhost", "localhost:80", false},
	}
	for _, c := range cases {
		target, secure, err := parseEndpoint(c.endpoint)
		require.NoError(t, err, c.endpoint)
		require.Equal(t, c.target, target, c.endpoint)
		require.Equal(t, c.secure, secure, c.endpoint)
	}

	_, _, err := parseEndpoint("")
	require.Error(t, err)
	_, _, err = parseEndpoint("ftp://x")
	require.Error(t, err)
}
