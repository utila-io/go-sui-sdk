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
	gotToken chan string
}

func (s *captureLedgerServer) GetServiceInfo(ctx context.Context, _ *pb.GetServiceInfoRequest) (*pb.GetServiceInfoResponse, error) {
	token := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("x-token"); len(v) > 0 {
			token = v[0]
		}
	}
	s.gotToken <- token
	return &pb.GetServiceInfoResponse{CheckpointHeight: proto.Uint64(42)}, nil
}

func TestWithAuthTokenSendsHeader(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	ledger := &captureLedgerServer{gotToken: make(chan string, 1)}
	pb.RegisterLedgerServiceServer(server, ledger)
	go server.Serve(lis)
	defer server.Stop()

	c, err := NewClient(lis.Addr().String(), WithInsecure(), WithAuthToken("sekret"))
	require.NoError(t, err)
	defer c.Close()

	seq, err := c.GetLatestCheckpointSequenceNumber(context.Background())
	require.NoError(t, err)
	require.Equal(t, "42", seq)
	require.Equal(t, "sekret", <-ledger.gotToken)
}
