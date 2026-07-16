package clientv2

import (
	"context"

	"google.golang.org/grpc"
)

// WithAuthToken returns a DialOption that sends token as an "x-token" header
// on every RPC (the auth scheme used by Sui gRPC node providers).
func WithAuthToken(token string) grpc.DialOption {
	return grpc.WithPerRPCCredentials(xTokenCredentials(token))
}

type xTokenCredentials string

func (c xTokenCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"x-token": string(c)}, nil
}

// RequireTransportSecurity is false so the token also works behind
// TLS-terminating proxies and on local plaintext nodes.
func (c xTokenCredentials) RequireTransportSecurity() bool { return false }
