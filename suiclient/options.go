package suiclient

import (
	"net/http"

	"google.golang.org/grpc"
)

// Backend identifies the transport serving the SuiClient interface.
type Backend int

const (
	// BackendJSONRPC serves the interface over the legacy JSON-RPC API via the
	// client package. It is the default backend.
	BackendJSONRPC Backend = iota
	// BackendGRPC serves the interface over the sui.rpc.v2 gRPC API via the
	// clientv2 package.
	BackendGRPC
)

// String returns the canonical name of the backend.
func (b Backend) String() string {
	switch b {
	case BackendJSONRPC:
		return "jsonrpc"
	case BackendGRPC:
		return "grpc"
	default:
		return "unknown"
	}
}

// config collects the settings applied by Options before New picks a backend.
type config struct {
	backend         *Backend
	httpClient      *http.Client
	grpcDialOptions []grpc.DialOption
}

// Option configures New.
type Option func(*config)

// WithBackend selects the backend explicitly, taking precedence over the
// SUI_SDK_BACKEND environment variable and the default.
func WithBackend(b Backend) Option {
	return func(c *config) {
		c.backend = &b
	}
}

// WithHTTPClient sets the http.Client used by the JSON-RPC backend. It has no
// effect on the gRPC backend.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) {
		c.httpClient = hc
	}
}

// WithGRPCDialOptions appends grpc.DialOptions used when connecting the gRPC
// backend (e.g. custom credentials or interceptors). It has no effect on the
// JSON-RPC backend.
func WithGRPCDialOptions(opts ...grpc.DialOption) Option {
	return func(c *config) {
		c.grpcDialOptions = append(c.grpcDialOptions, opts...)
	}
}
