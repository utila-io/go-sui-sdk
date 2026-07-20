package suiclient

import (
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc"

	"github.com/utila-io/go-sui-sdk/client"
	"github.com/utila-io/go-sui-sdk/clientv2"
)

// New returns a SuiClient for endpoint. The backend is selected with
// WithBackend and defaults to BackendJSONRPC. No network I/O at construction.
func New(endpoint string, opts ...Option) (SuiClient, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	backend := BackendJSONRPC
	if cfg.backend != nil {
		backend = *cfg.backend
	}

	for key := range cfg.headers {
		if err := clientv2.ValidateHeaderKey(key); err != nil {
			return nil, fmt.Errorf("suiclient: %w", err)
		}
	}

	switch backend {
	case BackendJSONRPC:
		hc := cfg.httpClient
		if hc == nil {
			hc = defaultHTTPClient()
		}
		if len(cfg.headers) > 0 {
			hc = withHeaders(hc, cfg.headers)
		}
		c, err := client.DialWithClient(endpoint, hc)
		if err != nil {
			return nil, err
		}
		return &jsonrpcBackend{c: c}, nil
	case BackendGRPC:
		if cfg.grpcConn != nil {
			if len(cfg.grpcDialOptions) > 0 {
				return nil, fmt.Errorf("suiclient: WithGRPCConn and WithGRPCDialOptions are mutually exclusive")
			}
			if len(cfg.headers) > 0 {
				return nil, fmt.Errorf("suiclient: WithHeader cannot be combined with WithGRPCConn; headers can't be attached to an injected connection, attach them when dialing it")
			}
			return clientv2.NewClientWithConn(cfg.grpcConn), nil
		}
		dialOpts := cfg.grpcDialOptions
		if cfg.insecure {
			// Prepended so caller transport credentials in dialOpts still win.
			dialOpts = append([]grpc.DialOption{clientv2.WithInsecure()}, dialOpts...)
		}
		if len(cfg.headers) > 0 {
			dialOpts = append(dialOpts, clientv2.WithHeaders(cfg.headers))
		}
		// Not returned directly: that would wrap a typed-nil *clientv2.Client
		// in a non-nil SuiClient interface on error.
		c, err := clientv2.NewClient(endpoint, dialOpts...)
		if err != nil {
			return nil, err
		}
		return c, nil
	default:
		return nil, fmt.Errorf("suiclient: unknown backend %d", backend)
	}
}

// withHeaders returns a shallow copy of hc whose transport sets each header;
// the caller's client is not mutated.
func withHeaders(hc *http.Client, headers map[string]string) *http.Client {
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone := *hc
	clone.Transport = headerTransport{next: transport, headers: headers}
	return &clone
}

type headerTransport struct {
	next    http.RoundTripper
	headers map[string]string
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.next.RoundTrip(req)
}

// defaultHTTPClient mirrors client.Dial's http.Client so that New(endpoint)
// behaves like client.Dial(endpoint).
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:    3,
			IdleConnTimeout: 30 * time.Second,
		},
		Timeout: 30 * time.Second,
	}
}
