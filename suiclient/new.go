package suiclient

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/utila-io/go-sui-sdk/client"
	"github.com/utila-io/go-sui-sdk/clientv2"
)

// BackendEnvVar is a last-resort operational override for backend selection,
// consulted only when WithBackend is not passed. Accepted values: "v1" or
// "jsonrpc" for the JSON-RPC backend, "v2" or "grpc" for the gRPC backend.
const BackendEnvVar = "SUI_SDK_BACKEND"

// New returns a SuiClient talking to endpoint. The backend is chosen by
// WithBackend when given, then by the SUI_SDK_BACKEND environment variable,
// and defaults to BackendJSONRPC.
//
// No network I/O happens at construction: the JSON-RPC backend only records
// the endpoint, and the gRPC backend connects lazily on the first call.
func New(endpoint string, opts ...Option) (SuiClient, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	backend := BackendJSONRPC
	if cfg.backend != nil {
		backend = *cfg.backend
	} else if envBackend, ok, err := backendFromEnv(); err != nil {
		return nil, err
	} else if ok {
		backend = envBackend
	}

	switch backend {
	case BackendJSONRPC:
		hc := cfg.httpClient
		if hc == nil {
			hc = defaultHTTPClient()
		}
		c, err := client.DialWithClient(endpoint, hc)
		if err != nil {
			return nil, err
		}
		return &jsonrpcBackend{c: c}, nil
	case BackendGRPC:
		return clientv2.NewClient(endpoint, cfg.grpcDialOptions...)
	default:
		return nil, fmt.Errorf("suiclient: unknown backend %d", backend)
	}
}

// backendFromEnv reads BackendEnvVar; ok is false when the variable is unset
// or empty.
func backendFromEnv() (backend Backend, ok bool, err error) {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(BackendEnvVar))); v {
	case "":
		return 0, false, nil
	case "v1", "jsonrpc":
		return BackendJSONRPC, true, nil
	case "v2", "grpc":
		return BackendGRPC, true, nil
	default:
		return 0, false, fmt.Errorf("suiclient: invalid %s value %q (want v1|jsonrpc|v2|grpc)", BackendEnvVar, v)
	}
}

// defaultHTTPClient mirrors the http.Client that client.Dial uses, so that
// New(endpoint) behaves like client.Dial(endpoint) unless WithHTTPClient is
// passed.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:    3,
			IdleConnTimeout: 30 * time.Second,
		},
		Timeout: 30 * time.Second,
	}
}
