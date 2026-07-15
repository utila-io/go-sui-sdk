// Package clientv2 is the sui.rpc.v2 gRPC backend of the suiclient.SuiClient
// interface. It mirrors the JSON-RPC response shapes from the types package;
// all proto-to-types conversion lives in the adapt subpackage.
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

// maxRecvMsgSize raises the default 4 MiB gRPC receive limit; checkpoints
// fetched with full transaction contents can exceed it. Callers can override
// it via dialOpts.
const maxRecvMsgSize = 64 * 1024 * 1024

// Client is a Sui client backed by the sui.rpc.v2 gRPC API.
type Client struct {
	conn   *grpc.ClientConn
	ledger pb.LedgerServiceClient
	state  pb.StateServiceClient
	exec   pb.TransactionExecutionServiceClient
}

// NewClient creates a client for the given gRPC endpoint. The endpoint may be
// "https://host:443", "host:443", "http://host:9000" or "host:9000"; TLS is
// used for the https scheme or port 443, plaintext otherwise. No network
// traffic happens at construction (the connection is lazy). Caller-provided
// dialOpts are appended last and can override the defaults, including the
// transport credentials.
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
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxRecvMsgSize)),
	}, dialOpts...)

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", target, err)
	}
	return &Client{
		conn:   conn,
		ledger: pb.NewLedgerServiceClient(conn),
		state:  pb.NewStateServiceClient(conn),
		exec:   pb.NewTransactionExecutionServiceClient(conn),
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// parseEndpoint strips an optional http/https scheme and decides whether to
// use TLS: https or port 443 mean TLS, http or any other explicit port mean
// plaintext. A bare host without a port defaults to :443 with TLS.
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
