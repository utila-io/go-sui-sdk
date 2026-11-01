package rpcv2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	structpb "google.golang.org/protobuf/types/known/structpb"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestEvents(t *testing.T) {
	t.Run("nil events", func(t *testing.T) {
		got, errs := (*TransactionEvents)(nil).ToInternalType("digest")
		require.Nil(t, got)
		require.Empty(t, errs)
	})

	t.Run("two events", func(t *testing.T) {
		txDigestStr, txDigest := testDigest(0x71)
		eventJSON, err := structpb.NewValue(map[string]any{"amount": "100"})
		require.NoError(t, err)

		got, errs := (&TransactionEvents{Events: []*Event{
			{
				PackageId: proto.String(longSuiPackage),
				Module:    proto.String("coin"),
				Sender:    proto.String(longOwnerAddress),
				EventType: proto.String(longSuiPackage + "::coin::DepositEvent<" + longSuiType + ">"),
				Contents:  &Bcs{Value: []byte{0xde, 0xad, 0xbe, 0xef}},
				Json:      eventJSON,
			},
			{
				PackageId: proto.String(longSuiPackage),
				Module:    proto.String("balance"),
				Sender:    proto.String(longOwnerAddress),
				EventType: proto.String(longSuiType),
			},
		}}).ToInternalType(txDigestStr)

		require.Empty(t, errs)
		require.Equal(t, []types.SuiEvent{
			{
				Id: types.EventId{
					TxDigest: txDigest,
					EventSeq: types.NewSafeSuiBigInt[uint64](0),
				},
				PackageId:         mustAddress(t, longSuiPackage),
				TransactionModule: "coin",
				Sender:            mustAddress(t, longOwnerAddress),
				Type:              "0x2::coin::DepositEvent<" + shortSuiType + ">",
				ParsedJson:        map[string]any{"amount": "100"},
				Bcs:               lib.Base58([]byte{0xde, 0xad, 0xbe, 0xef}).String(),
			},
			{
				Id: types.EventId{
					TxDigest: txDigest,
					EventSeq: types.NewSafeSuiBigInt[uint64](1),
				},
				PackageId:         mustAddress(t, longSuiPackage),
				TransactionModule: "balance",
				Sender:            mustAddress(t, longOwnerAddress),
				Type:              shortSuiType,
				ParsedJson:        nil,
				Bcs:               "",
			},
		}, got)
	})

	t.Run("event with invalid sender is reported", func(t *testing.T) {
		got, errs := (&TransactionEvents{Events: []*Event{{
			PackageId: proto.String(longSuiPackage),
			Sender:    proto.String("0xzz"),
			EventType: proto.String(longSuiType),
		}}}).ToInternalType("digest")
		require.Empty(t, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "0xzz")
	})
}
