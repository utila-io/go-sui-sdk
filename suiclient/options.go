package suiclient

import (
	"strings"

	"google.golang.org/grpc"
)

type config struct {
	grpcDialOptions []grpc.DialOption
	grpcConn        grpc.ClientConnInterface
	headers         map[string]string
	insecure        bool
}

type Option func(*config)

// WithGRPCDialOptions appends grpc.DialOptions used when dialing. Mutually
// exclusive with WithGRPCConn.
func WithGRPCDialOptions(opts ...grpc.DialOption) Option {
	return func(c *config) {
		c.grpcDialOptions = append(c.grpcDialOptions, opts...)
	}
}

// WithGRPCConn makes the client use a caller-provided connection; New's
// endpoint argument is then ignored and may be empty. The caller keeps
// ownership (the client's Close is a no-op) and should apply
// clientv2.MaxRecvMsgSize when dialing. Mutually exclusive with
// WithGRPCDialOptions.
func WithGRPCConn(conn grpc.ClientConnInterface) Option {
	return func(c *config) {
		c.grpcConn = conn
	}
}

// WithInsecure dials plaintext instead of the TLS default (e.g. an internal
// bridge on host:port). No effect on a WithGRPCConn connection; transport
// credentials passed via WithGRPCDialOptions still win.
func WithInsecure() Option {
	return func(c *config) {
		c.insecure = true
	}
}

// WithHeader sends key: value as gRPC metadata on every request. Repeatable;
// keys are case-insensitive and the last value per key wins. Keys that gRPC
// reserves (empty, ":"-prefixed, "grpc-"-prefixed) make New error. Cannot be
// combined with WithGRPCConn: headers can't be attached to an injected
// connection, attach them when dialing it instead.
func WithHeader(key, value string) Option {
	return func(c *config) {
		if c.headers == nil {
			c.headers = make(map[string]string)
		}
		c.headers[strings.ToLower(key)] = value
	}
}
