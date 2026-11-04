package suiclient

import (
	"fmt"

	"google.golang.org/grpc"

	"github.com/utila-io/go-sui-sdk/client"
)

// New returns a sui.rpc.v2 gRPC SuiClient for endpoint. No network I/O at
// construction.
func New(endpoint string, opts ...Option) (SuiClient, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	for key := range cfg.headers {
		if err := client.ValidateHeaderKey(key); err != nil {
			return nil, fmt.Errorf("suiclient: %w", err)
		}
	}

	if cfg.grpcConn != nil {
		if len(cfg.grpcDialOptions) > 0 {
			return nil, fmt.Errorf("suiclient: WithGRPCConn and WithGRPCDialOptions are mutually exclusive")
		}
		if len(cfg.headers) > 0 {
			return nil, fmt.Errorf("suiclient: WithHeader cannot be combined with WithGRPCConn; headers can't be attached to an injected connection, attach them when dialing it")
		}
		return client.NewClientWithConn(cfg.grpcConn), nil
	}
	dialOpts := cfg.grpcDialOptions
	if cfg.insecure {
		// Prepended so caller transport credentials in dialOpts still win.
		dialOpts = append([]grpc.DialOption{client.WithInsecure()}, dialOpts...)
	}
	if len(cfg.headers) > 0 {
		dialOpts = append(dialOpts, client.WithHeaders(cfg.headers))
	}
	// Not returned directly: that would wrap a typed-nil *client.Client
	// in a non-nil SuiClient interface on error.
	c, err := client.NewClient(endpoint, dialOpts...)
	if err != nil {
		return nil, err
	}
	return c, nil
}
