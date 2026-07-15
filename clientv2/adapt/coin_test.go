package adapt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestCoin(t *testing.T) {
	objDigestStr, objDigest := testDigest(0x51)
	prevTxStr, prevTx := testDigest(0x52)

	got, err := Coin(&pb.Object{
		ObjectId:            proto.String(longObjectID),
		Version:             proto.Uint64(9007199254740993), // above 2^53: JSON numbers would lose precision
		Digest:              proto.String(objDigestStr),
		ObjectType:          proto.String("0x2::coin::Coin<" + longUsdcType + ">"),
		Balance:             proto.Uint64(5_000_000),
		PreviousTransaction: proto.String(prevTxStr),
	})
	require.NoError(t, err)
	require.Equal(t, types.Coin{
		CoinType:            shortUsdcType,
		CoinObjectId:        mustAddress(t, longObjectID),
		Version:             types.NewSafeSuiBigInt[uint64](9007199254740993),
		Digest:              objDigest,
		Balance:             types.NewSafeSuiBigInt[uint64](5_000_000),
		PreviousTransaction: prevTx,
	}, got)
	require.Nil(t, got.LockedUntilEpoch)

	// SafeSuiBigInt fields must marshal as JSON strings (v1 wire shape).
	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(data), `"version":"9007199254740993"`)
	require.Contains(t, string(data), `"balance":"5000000"`)
}

func TestCoin_errors(t *testing.T) {
	tests := []struct {
		name string
		in   *pb.Object
	}{
		{
			name: "invalid object id",
			in: &pb.Object{
				ObjectId:   proto.String("0xzz"),
				ObjectType: proto.String("0x2::coin::Coin<" + shortSuiType + ">"),
			},
		},
		{
			name: "object type without generic",
			in: &pb.Object{
				ObjectId:   proto.String(longObjectID),
				ObjectType: proto.String("0x2::coin::Coin"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Coin(tt.in)
			require.Error(t, err)
		})
	}
}

func TestCoinTypeFromObjectType(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "long form inner type normalized",
			in:   "0x2::coin::Coin<" + longSuiType + ">",
			want: shortSuiType,
		},
		{
			name: "long form outer and inner",
			in:   longSuiPackage + "::coin::Coin<" + longUsdcType + ">",
			want: shortUsdcType,
		},
		{
			name: "nested generic inner type uses last closing bracket",
			in:   "0x2::coin::Coin<0x00000000000000000000000000000000000000000000000000000000000000ab::lp::LP<" + longSuiType + ", " + longUsdcType + ">>",
			want: "0xab::lp::LP<" + shortSuiType + ", " + shortUsdcType + ">",
		},
		{
			name:    "no generic brackets",
			in:      "0x2::coin::Coin",
			wantErr: true,
		},
		{
			name:    "package type",
			in:      "package",
			wantErr: true,
		},
		{
			name:    "closing bracket before opening",
			in:      "0x2>oops<",
			wantErr: true,
		},
		{
			name:    "empty string",
			in:      "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CoinTypeFromObjectType(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCoinReadMaskPaths(t *testing.T) {
	require.Equal(t, []string{
		"object_id", "version", "digest", "object_type", "balance", "previous_transaction",
	}, CoinReadMaskPaths)
}

func TestCoinMetadata(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		got, err := CoinMetadata(&pb.GetCoinInfoResponse{
			CoinType: proto.String(longUsdcType),
			Metadata: &pb.CoinMetadata{
				Id:          proto.String(longObjectID),
				Decimals:    proto.Uint32(6),
				Name:        proto.String("USD Coin"),
				Symbol:      proto.String("USDC"),
				Description: proto.String("Stablecoin"),
				IconUrl:     proto.String("https://example.com/usdc.png"),
			},
		})
		require.NoError(t, err)
		require.Equal(t, &types.SuiCoinMetadata{
			Decimals:    6,
			Description: "Stablecoin",
			IconUrl:     "https://example.com/usdc.png",
			Id:          mustAddress(t, longObjectID),
			Name:        "USD Coin",
			Symbol:      "USDC",
		}, got)
	})

	t.Run("absent id stays zero", func(t *testing.T) {
		got, err := CoinMetadata(&pb.GetCoinInfoResponse{
			Metadata: &pb.CoinMetadata{Decimals: proto.Uint32(9), Symbol: proto.String("SUI")},
		})
		require.NoError(t, err)
		require.Equal(t, sui_types.ObjectID{}, got.Id)
		require.Equal(t, uint8(9), got.Decimals)
	})

	t.Run("no metadata is an error", func(t *testing.T) {
		_, err := CoinMetadata(&pb.GetCoinInfoResponse{CoinType: proto.String(shortSuiType)})
		require.Error(t, err)
	})

	t.Run("invalid id is an error", func(t *testing.T) {
		_, err := CoinMetadata(&pb.GetCoinInfoResponse{
			Metadata: &pb.CoinMetadata{Id: proto.String("0xnothex")},
		})
		require.Error(t, err)
	})
}
