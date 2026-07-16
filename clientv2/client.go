// Package clientv2 is the sui.rpc.v2 gRPC backend of suiclient.SuiClient. It
// mirrors JSON-RPC response shapes; proto conversion lives in adapt.
package clientv2

import (
	"errors"
	"fmt"
	"net"
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

// Client is a Sui client backed by the sui.rpc.v2 gRPC API.
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

// NewClient creates a client for the given gRPC endpoint; TLS is used for the
// https scheme or port 443, plaintext otherwise. The connection is lazy.
// dialOpts are appended last and can override the defaults.
func NewClient(endpoint string, dialOpts ...grpc.DialOption) (*Client, error) {
	target, secure, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	creds := insecure.NewCredentials()
	if secure {
		creds = credentials.NewClientTLSFromCert(nil, "")
	}
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(creds),
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

// parseEndpoint strips an optional http/https scheme; https or port 443 mean
// TLS, http or any other explicit port plaintext, a bare host :443 with TLS.
func parseEndpoint(endpoint string) (target string, secure bool, err error) {
	scheme := ""
	if i := strings.Index(endpoint, "://"); i >= 0 {
		scheme = strings.ToLower(endpoint[:i])
		endpoint = endpoint[i+3:]
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	if endpoint == "" {
		return "", false, errors.New("empty gRPC endpoint")
	}
	_, port, splitErr := net.SplitHostPort(endpoint)
	hasPort := splitErr == nil

	switch scheme {
	case "https":
		if !hasPort {
			endpoint += ":443"
		}
		return endpoint, true, nil
	case "http":
		if !hasPort {
			endpoint += ":80"
		}
		return endpoint, false, nil
	case "":
		if !hasPort {
			return endpoint + ":443", true, nil
		}
		return endpoint, port == "443", nil
	default:
		return "", false, fmt.Errorf("unsupported endpoint scheme %q", scheme)
	}
}
