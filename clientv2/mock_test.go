package clientv2

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	mock_rpcv2 "github.com/utila-io/go-sui-sdk/clientv2/internal/genmock/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

// fakeStream stands in for a server stream: it replays frames, then err (io.EOF
// when unset). The embedded nil ClientStream is never called.
type fakeStream[R any] struct {
	grpc.ClientStream
	frames []*R
	err    error
	next   int
}

func (s *fakeStream[R]) Recv() (*R, error) {
	if s.next < len(s.frames) {
		frame := s.frames[s.next]
		s.next++
		return frame, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}

type serviceMocks struct {
	ledger *mock_rpcv2.MockLedgerServiceClient
	state  *mock_rpcv2.MockStateServiceClient
	exec   *mock_rpcv2.MockTransactionExecutionServiceClient
}

func newMockClient(t *testing.T) (*Client, serviceMocks) {
	ctrl := gomock.NewController(t)
	mocks := serviceMocks{
		ledger: mock_rpcv2.NewMockLedgerServiceClient(ctrl),
		state:  mock_rpcv2.NewMockStateServiceClient(ctrl),
		exec:   mock_rpcv2.NewMockTransactionExecutionServiceClient(ctrl),
	}
	return &Client{ledger: mocks.ledger, state: mocks.state, exec: mocks.exec}, mocks
}

type protoEq struct{ want proto.Message }

func (m protoEq) Matches(x any) bool {
	msg, ok := x.(proto.Message)
	return ok && proto.Equal(m.want, msg)
}

func (m protoEq) String() string {
	return prototext.MarshalOptions{}.Format(m.want)
}

// protoEqual matches the exact request proto, pinning everything sent on the
// wire (gomock.Eq compares unexported protoimpl state and misfires).
func protoEqual(want proto.Message) gomock.Matcher { return protoEq{want} }

func testAddress(t *testing.T) sui_types.SuiAddress {
	addr, err := sui_types.NewAddressFromHex("0x7113a31aa484dfca371f854ae74918c7463c7b3f1bf4c1fe8ef28835e88fd590")
	require.NoError(t, err)
	return *addr
}

// testDigest returns a distinct 32-byte digest with no zero bytes, so its
// base58 string form roundtrips exactly.
func testDigest(i int) sui_types.TransactionDigest {
	digest := make(lib.Base58, 32)
	for j := range digest {
		digest[j] = byte(1 + (i+j)%255)
	}
	return digest
}
