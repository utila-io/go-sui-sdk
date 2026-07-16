package clientv2

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

type captureLedgerServer struct {
	pb.UnimplementedLedgerServiceServer
	gotMD chan metadata.MD
}

func (s *captureLedgerServer) GetServiceInfo(ctx context.Context, _ *pb.GetServiceInfoRequest) (*pb.GetServiceInfoResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.gotMD <- md
	return &pb.GetServiceInfoResponse{CheckpointHeight: proto.Uint64(42)}, nil
}

func TestWithHeadersSendsHeaders(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	ledger := &captureLedgerServer{gotMD: make(chan metadata.MD, 1)}
	pb.RegisterLedgerServiceServer(server, ledger)
	go server.Serve(lis)
	defer server.Stop()

	// "Authorization" also pins lowercase normalization: gRPC fails RPCs whose
	// per-RPC credentials carry uppercase keys.
	c, err := NewClient(lis.Addr().String(), WithInsecure(), WithHeaders(map[string]string{
		"Authorization": "Bearer sekret",
		"x-api-key":     "k123",
	}))
	require.NoError(t, err)
	defer c.Close()

	seq, err := c.GetLatestCheckpointSequenceNumber(context.Background())
	require.NoError(t, err)
	require.Equal(t, "42", seq)

	md := <-ledger.gotMD
	require.Equal(t, []string{"Bearer sekret"}, md.Get("authorization"))
	require.Equal(t, []string{"k123"}, md.Get("x-api-key"))
}

func TestWithHeadersPanicsOnInvalidKey(t *testing.T) {
	for _, key := range []string{"", ":authority", "grpc-timeout", "Grpc-Timeout", "x api key"} {
		require.Panics(t, func() { WithHeaders(map[string]string{key: "v"}) }, "key %q", key)
	}
}

func TestValidateHeaderKey(t *testing.T) {
	for _, key := range []string{
		"x-api-key",
		"Authorization", // uppercase normalizes to lowercase, still valid
		"x_key.v1",
		"key0-9",
	} {
		require.NoError(t, ValidateHeaderKey(key), "key %q", key)
	}
	for _, key := range []string{
		"",
		":path",
		"grpc-encoding",
		"GRPC-Encoding",
		"x api key",  // space
		"x-api-key ", // trailing space
		"clé",        // non-ASCII
		"日本語",
		"x@key",
		"key;v",
	} {
		require.Error(t, ValidateHeaderKey(key), "key %q", key)
	}
}
