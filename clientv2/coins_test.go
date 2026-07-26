package clientv2

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
)

var coinReadMask = &fieldmaskpb.FieldMask{Paths: []string{
	"object_id", "version", "digest", "object_type", "balance", "previous_transaction",
}}

func TestGetCoins(t *testing.T) {
	owner := testAddress(t)
	coinObject := &pb.Object{
		ObjectId:            proto.String("0x0000000000000000000000000000000000000000000000000000000000000abc"),
		Version:             proto.Uint64(5),
		Digest:              proto.String(testDigest(1).String()),
		ObjectType:          proto.String("0x2::coin::Coin<0x2::sui::SUI>"),
		Balance:             proto.Uint64(12345),
		PreviousTransaction: proto.String(testDigest(2).String()),
	}
	usdcType := "0xdba34672e30cb065b1f93e3ab55318768fd6fef66c15942c9f7cb846e2f900e7::usdc::USDC"
	cursorIn := base64.StdEncoding.EncodeToString([]byte("token-in"))

	cases := []struct {
		name           string
		coinType       *string
		cursor         *string
		limit          uint
		wantRequest    *pb.ListOwnedObjectsRequest
		response       *pb.ListOwnedObjectsResponse
		wantNextCursor *string
	}{
		{
			name: "default coin type, empty next token",
			wantRequest: &pb.ListOwnedObjectsRequest{
				Owner:      proto.String(owner.String()),
				ObjectType: proto.String("0x2::coin::Coin<0x2::sui::SUI>"),
				ReadMask:   coinReadMask,
			},
			response: &pb.ListOwnedObjectsResponse{Objects: []*pb.Object{coinObject}},
		},
		{
			name:     "custom coin type and limit",
			coinType: &usdcType,
			limit:    7,
			wantRequest: &pb.ListOwnedObjectsRequest{
				Owner:      proto.String(owner.String()),
				ObjectType: proto.String("0x2::coin::Coin<" + usdcType + ">"),
				PageSize:   proto.Uint32(7),
				ReadMask:   coinReadMask,
			},
			response: &pb.ListOwnedObjectsResponse{},
		},
		{
			name:   "cursor roundtrip",
			cursor: &cursorIn,
			wantRequest: &pb.ListOwnedObjectsRequest{
				Owner:      proto.String(owner.String()),
				ObjectType: proto.String("0x2::coin::Coin<0x2::sui::SUI>"),
				PageToken:  []byte("token-in"),
				ReadMask:   coinReadMask,
			},
			response: &pb.ListOwnedObjectsResponse{
				Objects:       []*pb.Object{coinObject},
				NextPageToken: []byte("token-out"),
			},
			wantNextCursor: proto.String(base64.StdEncoding.EncodeToString([]byte("token-out"))),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, mocks := newMockClient(t)
			mocks.state.EXPECT().
				ListOwnedObjects(gomock.Any(), protoEqual(c.wantRequest)).
				Return(c.response, nil)

			page, err := client.GetCoins(context.Background(), owner, c.coinType, c.cursor, c.limit)
			require.NoError(t, err)
			require.Len(t, page.Data, len(c.response.GetObjects()))
			for _, coin := range page.Data {
				require.Equal(t, "0x2::sui::SUI", coin.CoinType)
				require.Equal(t, uint64(12345), coin.Balance.Uint64())
				require.Equal(t, testDigest(1).String(), coin.Digest.String())
			}
			require.Equal(t, c.wantNextCursor != nil, page.HasNextPage)
			require.Equal(t, c.wantNextCursor, page.NextCursor)
		})
	}
}

func TestGetCoinsInvalidCursor(t *testing.T) {
	client, _ := newMockClient(t)
	badCursor := "not-base64!"
	_, err := client.GetCoins(context.Background(), testAddress(t), nil, &badCursor, 0)
	require.ErrorContains(t, err, "invalid cursor")
}
