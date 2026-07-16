package clientv2

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
)

// ValidateHeaderKey rejects keys gRPC reserves or would fail on at RPC time:
// empty, ":"-prefixed pseudo-headers, and the "grpc-" prefix.
func ValidateHeaderKey(key string) error {
	key = strings.ToLower(key)
	switch {
	case key == "":
		return fmt.Errorf("header key must not be empty")
	case strings.HasPrefix(key, ":"):
		return fmt.Errorf("header key %q: pseudo-headers are reserved", key)
	case strings.HasPrefix(key, "grpc-"):
		return fmt.Errorf("header key %q: the grpc- prefix is reserved", key)
	}
	return nil
}

// WithHeaders returns a DialOption that sends each key/value pair as metadata
// on every RPC. Keys are normalized to lowercase (HTTP/2 rejects uppercase);
// keys failing ValidateHeaderKey panic here, at construction, rather than
// failing the first RPC. suiclient validates first and returns an error
// instead.
func WithHeaders(headers map[string]string) grpc.DialOption {
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		if err := ValidateHeaderKey(key); err != nil {
			panic("clientv2.WithHeaders: " + err.Error())
		}
		normalized[strings.ToLower(key)] = value
	}
	return grpc.WithPerRPCCredentials(headerCredentials(normalized))
}

type headerCredentials map[string]string

func (c headerCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return c, nil
}

// RequireTransportSecurity is false so the headers also work behind
// TLS-terminating proxies and on local plaintext nodes.
func (c headerCredentials) RequireTransportSecurity() bool { return false }
