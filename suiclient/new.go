package suiclient

import (
	"fmt"
	"net/http"
	"time"

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

	switch backend {
	case BackendJSONRPC:
		hc := cfg.httpClient
		if hc == nil {
			hc = defaultHTTPClient()
		}
		if cfg.authToken != "" {
			hc = withAuthHeader(hc, cfg.authToken)
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
			if cfg.authToken != "" {
				return nil, fmt.Errorf("suiclient: WithAuthToken cannot be combined with WithGRPCConn; attach credentials when dialing the connection")
			}
			return clientv2.NewClientWithConn(cfg.grpcConn), nil
		}
		dialOpts := cfg.grpcDialOptions
		if cfg.authToken != "" {
			dialOpts = append(dialOpts, clientv2.WithAuthToken(cfg.authToken))
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

// withAuthHeader returns a shallow copy of hc whose transport adds the
// x-token header; the caller's client is not mutated.
func withAuthHeader(hc *http.Client, token string) *http.Client {
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone := *hc
	clone.Transport = authTransport{next: transport, token: token}
	return &clone
}

type authTransport struct {
	next  http.RoundTripper
	token string
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("x-token", t.token)
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
