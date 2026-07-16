package suiclient

import (
	"net/http"

	"google.golang.org/grpc"
)

type Backend int

const (
	// BackendJSONRPC is the default backend (legacy JSON-RPC, client package).
	BackendJSONRPC Backend = iota
	// BackendGRPC serves the sui.rpc.v2 gRPC API via the clientv2 package.
	BackendGRPC
)

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

type config struct {
	backend         *Backend
	httpClient      *http.Client
	grpcDialOptions []grpc.DialOption
	grpcConn        grpc.ClientConnInterface
	authToken       string
	insecure        bool
}

type Option func(*config)

// WithBackend selects the backend explicitly instead of the default
// BackendJSONRPC.
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

// WithInsecure makes the gRPC backend dial plaintext instead of the TLS
// default (e.g. an internal bridge on host:port). No effect on JSON-RPC or on
// a WithGRPCConn connection; transport credentials passed via
// WithGRPCDialOptions still win.
func WithInsecure() Option {
	return func(c *config) {
		c.insecure = true
	}
}

// WithAuthToken sends token as an "x-token" header on every request, on both
// backends. Cannot be combined with WithGRPCConn: attach credentials when
// dialing the injected connection instead.
func WithAuthToken(token string) Option {
	return func(c *config) {
		c.authToken = token
	}
}
