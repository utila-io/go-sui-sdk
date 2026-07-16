package clientv2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		target   string
	}{
		{"grpc://fullnode.mainnet.sui.io:443", "fullnode.mainnet.sui.io:443"},
		{"grpc://fullnode.mainnet.sui.io", "fullnode.mainnet.sui.io:443"},
		{"grpc://node.example:9000/", "node.example:9000"},
		{"fullnode.mainnet.sui.io", "fullnode.mainnet.sui.io:443"},
		{"fullnode.mainnet.sui.io:443", "fullnode.mainnet.sui.io:443"},
		{"my-node.provider.example:9000", "my-node.provider.example:9000"},
		// loopback is not special: TLS default applies there too
		{"127.0.0.1:9000", "127.0.0.1:9000"},
		{"localhost:9000", "localhost:9000"},
		{"localhost", "localhost:443"},
	}
	for _, c := range cases {
		target, creds, err := parseEndpoint(c.endpoint)
		require.NoError(t, err, c.endpoint)
		require.Equal(t, c.target, target, c.endpoint)
		require.Nil(t, creds, c.endpoint)
	}
}

func TestParseEndpointUserinfo(t *testing.T) {
	for _, endpoint := range []string{
		"grpc://user:pass@node.example:9000",
		"user:pass@node.example:9000",
	} {
		target, creds, err := parseEndpoint(endpoint)
		require.NoError(t, err, endpoint)
		require.Equal(t, "node.example:9000", target, endpoint)
		require.NotNil(t, creds, endpoint)
		require.Equal(t, "user", creds.Username(), endpoint)
		pass, ok := creds.Password()
		require.True(t, ok, endpoint)
		require.Equal(t, "pass", pass, endpoint)
	}

	target, creds, err := parseEndpoint("grpc://user@node.example")
	require.NoError(t, err)
	require.Equal(t, "node.example:443", target)
	require.Equal(t, "user", creds.Username())
	_, hasPass := creds.Password()
	require.False(t, hasPass)

	// Construction must fail rather than dial with the credentials dropped.
	c, err := NewClient("grpc://user:pass@node.example:9000")
	require.Nil(t, c)
	require.ErrorContains(t, err, "URL credentials are not yet supported")
}

func TestParseEndpointRejected(t *testing.T) {
	for _, endpoint := range []string{
		"",
		"grpc://",
		"https://fullnode.mainnet.sui.io:443",
		"https://fullnode.mainnet.sui.io",
		"http://localhost:9000",
		"ftp://x",
	} {
		_, _, err := parseEndpoint(endpoint)
		require.Error(t, err, endpoint)
	}

	_, err := NewClient("https://fullnode.mainnet.sui.io:443")
	require.ErrorContains(t, err, "grpc://host:port or host:port")
}
