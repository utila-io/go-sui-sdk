package suiclient

import (
	"net/http"

	"google.golang.org/grpc"
)

// Backend identifies the transport serving the SuiClient interface.
type Backend int

const (
	// BackendJSONRPC is the default backend (legacy JSON-RPC, client package).
	BackendJSONRPC Backend = iota
	// BackendGRPC serves the sui.rpc.v2 gRPC API via the clientv2 package.
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
	grpcConn        grpc.ClientConnInterface
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

// WithGRPCDialOptions appends grpc.DialOptions used when dialing the gRPC
// backend. No effect on JSON-RPC; mutually exclusive with WithGRPCConn.
func WithGRPCDialOptions(opts ...grpc.DialOption) Option {
	return func(c *config) {
		c.grpcDialOptions = append(c.grpcDialOptions, opts...)
	}
}

// WithGRPCConn makes the gRPC backend use a caller-provided connection; New's
// endpoint argument is then ignored and may be empty. The caller keeps
// ownership (the client's Close is a no-op) and should apply
// clientv2.MaxRecvMsgSize when dialing. Mutually exclusive with
// WithGRPCDialOptions.
func WithGRPCConn(conn grpc.ClientConnInterface) Option {
	return func(c *config) {
		c.grpcConn = conn
	}
}
