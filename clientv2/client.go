// Package clientv2 is the sui.rpc.v2 gRPC backend of suiclient.SuiClient. It
// mirrors JSON-RPC response shapes; proto conversion lives in adapt.
package clientv2

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

// MaxRecvMsgSize raises the default 4 MiB gRPC receive limit (large checkpoint
// responses exceed it). NewClient applies it by default; callers injecting a
// connection via NewClientWithConn must apply it themselves when dialing.
const MaxRecvMsgSize = 64 * 1024 * 1024

type Client struct {
	// ownedConn is the connection dialed by NewClient; nil when the
	// connection was injected via NewClientWithConn (caller-owned).
	ownedConn *grpc.ClientConn
	ledger    pb.LedgerServiceClient
	state     pb.StateServiceClient
	exec      pb.TransactionExecutionServiceClient
}

// NewClientWithConn creates a client on a caller-provided gRPC connection. The
// caller keeps ownership: Close on the returned client is a no-op. The
// connection should be dialed with MaxRecvMsgSize applied.
func NewClientWithConn(conn grpc.ClientConnInterface) *Client {
	return &Client{
		ledger: pb.NewLedgerServiceClient(conn),
		state:  pb.NewStateServiceClient(conn),
		exec:   pb.NewTransactionExecutionServiceClient(conn),
	}
}

// WithInsecure returns a DialOption that switches the connection to plaintext
// (TLS is otherwise always used), e.g. for an internal bridge. Appended after
// the TLS default, so a later caller-supplied transport credential still wins.
func WithInsecure() grpc.DialOption {
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}

// NewClient creates a client for the given gRPC endpoint, "grpc://host[:port]"
// or "host[:port]" (port defaults to 443). TLS is always used unless
// WithInsecure (or another transport credential) is passed in dialOpts, which
// are appended last and override the defaults. The connection is lazy.
func NewClient(endpoint string, dialOpts ...grpc.DialOption) (*Client, error) {
	target, creds, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if creds != nil {
		// Future URL-credential support (Basic vs x-token mapping, pending the
		// provider's auth scheme) plugs in here; until then failing loudly
		// beats dialing with the credentials silently dropped.
		return nil, fmt.Errorf("gRPC endpoint %s: URL credentials are not yet supported; pass credentials via WithHeaders or gRPC dial options", target)
	}
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxRecvMsgSize)),
	}, dialOpts...)

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", target, err)
	}
	client := NewClientWithConn(conn)
	client.ownedConn = conn
	return client, nil
}

// Close closes the connection when it was dialed by NewClient; no-op for a
// connection injected via NewClientWithConn (caller-owned).
func (c *Client) Close() error {
	if c.ownedConn == nil {
		return nil
	}
	return c.ownedConn.Close()
}

// parseEndpoint accepts "grpc://host[:port]" and "host[:port]", both
// optionally with "user:pass@" userinfo, which is returned separately and
// stripped from the dial target. A missing port defaults to 443. http and
// https are rejected: transport security is not a property of the endpoint
// string (TLS is always the default; plaintext is WithInsecure).
func parseEndpoint(endpoint string) (target string, creds *url.Userinfo, err error) {
	rest := endpoint
	if i := strings.Index(rest, "://"); i >= 0 {
		scheme := strings.ToLower(rest[:i])
		if scheme != "grpc" {
			return "", nil, fmt.Errorf("unsupported gRPC endpoint scheme %q; use grpc://host:port or host:port", scheme)
		}
		rest = rest[i+3:]
	}
	rest = strings.TrimSuffix(rest, "/")
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		if user, pass, ok := strings.Cut(rest[:i], ":"); ok {
			creds = url.UserPassword(user, pass)
		} else {
			creds = url.User(user)
		}
		rest = rest[i+1:]
	}
	if rest == "" {
		return "", nil, errors.New("empty gRPC endpoint")
	}
	if _, _, splitErr := net.SplitHostPort(rest); splitErr != nil {
		rest += ":443"
	}
	return rest, creds, nil
}
