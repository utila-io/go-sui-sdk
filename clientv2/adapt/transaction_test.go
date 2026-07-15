package adapt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestEvents(t *testing.T) {
	t.Run("nil events", func(t *testing.T) {
		got, errs := Events("digest", nil)
		require.Nil(t, got)
		require.Empty(t, errs)
	})

	t.Run("two events", func(t *testing.T) {
		txDigestStr, txDigest := testDigest(0x71)
		eventJSON, err := structpb.NewValue(map[string]any{"amount": "100"})
		require.NoError(t, err)

		got, errs := Events(txDigestStr, &pb.TransactionEvents{Events: []*pb.Event{
			{
				PackageId: proto.String(longSuiPackage),
				Module:    proto.String("coin"),
				Sender:    proto.String(longOwnerAddress),
				EventType: proto.String(longSuiPackage + "::coin::DepositEvent<" + longSuiType + ">"),
				Contents:  &pb.Bcs{Value: []byte{0xde, 0xad, 0xbe, 0xef}},
				Json:      eventJSON,
			},
			{
				PackageId: proto.String(longSuiPackage),
				Module:    proto.String("balance"),
				Sender:    proto.String(longOwnerAddress),
				EventType: proto.String(longSuiType),
			},
		}})

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
		got, errs := Events("digest", &pb.TransactionEvents{Events: []*pb.Event{{
			PackageId: proto.String(longSuiPackage),
			Sender:    proto.String("0xzz"),
			EventType: proto.String(longSuiType),
		}}})
		require.Empty(t, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "0xzz")
	})
}

func TestBalanceChanges(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		got, errs := BalanceChanges(nil)
		require.Nil(t, got)
		require.Empty(t, errs)
		got, errs = BalanceChanges([]*pb.BalanceChange{})
		require.Nil(t, got)
		require.Empty(t, errs)
	})

	t.Run("coin type normalized owner address kept long", func(t *testing.T) {
		ownerAddr := mustAddress(t, longOwnerAddress)
		got, errs := BalanceChanges([]*pb.BalanceChange{{
			Address:  proto.String(longOwnerAddress),
			CoinType: proto.String(longSuiType),
			Amount:   proto.String("-100"),
		}})
		require.Empty(t, errs)
		require.Equal(t, []types.BalanceChange{{
			Owner: types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
			},
			CoinType: shortSuiType,
			Amount:   "-100",
		}}, got)
		// The owner address is the full 32-byte address, not a shortened one.
		require.Equal(t, longOwnerAddress, got[0].Owner.AddressOwner.String())
	})

	t.Run("invalid address is reported", func(t *testing.T) {
		got, errs := BalanceChanges([]*pb.BalanceChange{{
			Address:  proto.String("not-an-address-zz"),
			CoinType: proto.String(longSuiType),
			Amount:   proto.String("1"),
		}})
		require.Empty(t, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "not-an-address-zz")
	})
}

func TestResponse(t *testing.T) {
	txDigestStr, txDigest := testDigest(0x72)
	fxDigestStr, _ := testDigest(0x73)

	t.Run("minimal", func(t *testing.T) {
		got := Response(&pb.ExecutedTransaction{Digest: proto.String(txDigestStr)}, types.SuiTransactionBlockResponseOptions{})
		require.Equal(t, txDigest, got.Digest)
		require.Nil(t, got.Effects)
		require.Nil(t, got.Events)
		require.Nil(t, got.BalanceChanges)
		require.Nil(t, got.TimestampMs)
		require.Nil(t, got.Checkpoint)
		require.Empty(t, got.RawTransaction)
		require.Nil(t, got.Transaction)
	})

	t.Run("full", func(t *testing.T) {
		txData := testTransactionDataBytes(t)
		signature := &pb.UserSignature{Bcs: &pb.Bcs{Value: []byte{0x0a, 0x0b, 0x0c}}}
		got := Response(&pb.ExecutedTransaction{
			Digest: proto.String(txDigestStr),
			Transaction: &pb.Transaction{
				Bcs: &pb.Bcs{Value: txData},
			},
			Signatures: []*pb.UserSignature{signature},
			Effects: &pb.TransactionEffects{
				Status:            &pb.ExecutionStatus{Success: proto.Bool(true)},
				TransactionDigest: proto.String(fxDigestStr),
			},
			Checkpoint: proto.Uint64(555),
			Timestamp:  timestamppb.New(time.UnixMilli(1700000000456)),
		}, types.SuiTransactionBlockResponseOptions{ShowInput: true, ShowRawInput: true})
		require.Equal(t, txDigest, got.Digest)
		require.Equal(t, RawSenderSignedData(txData, []*pb.UserSignature{signature}), got.RawTransaction)
		require.NotNil(t, got.Transaction)
		require.Empty(t, got.Errors)
		require.NotNil(t, got.Effects)
		require.Equal(t, types.ExecutionStatusSuccess, got.Effects.Data.V1.Status.Status)
		require.NotNil(t, got.Checkpoint)
		require.EqualValues(t, 555, got.Checkpoint.Uint64())
		require.NotNil(t, got.TimestampMs)
		require.EqualValues(t, 1700000000456, got.TimestampMs.Uint64())
	})

	t.Run("input options off leave transaction fields unset", func(t *testing.T) {
		got := Response(&pb.ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: testTransactionDataBytes(t)}},
		}, types.SuiTransactionBlockResponseOptions{ShowEffects: true})
		require.Nil(t, got.RawTransaction)
		require.Nil(t, got.Transaction)
	})

	t.Run("show raw input only", func(t *testing.T) {
		txData := testTransactionDataBytes(t)
		got := Response(&pb.ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: txData}},
		}, types.SuiTransactionBlockResponseOptions{ShowRawInput: true})
		require.Equal(t, RawSenderSignedData(txData, nil), got.RawTransaction)
		require.Nil(t, got.Transaction)
	})

	t.Run("undecodable transaction reports an error instead of failing", func(t *testing.T) {
		got := Response(&pb.ExecutedTransaction{
			Digest:      proto.String(txDigestStr),
			Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: []byte{0xff, 0xff}}},
		}, types.SuiTransactionBlockResponseOptions{ShowInput: true})
		require.Nil(t, got.Transaction)
		require.NotEmpty(t, got.Errors)
	})

	t.Run("unparseable entries are reported via Errors", func(t *testing.T) {
		got := Response(&pb.ExecutedTransaction{
			Digest: proto.String(txDigestStr),
			Events: &pb.TransactionEvents{Events: []*pb.Event{{
				PackageId: proto.String(longSuiPackage),
				Sender:    proto.String("0xzz"),
				EventType: proto.String(longSuiType),
			}}},
			BalanceChanges: []*pb.BalanceChange{
				{
					Address:  proto.String("not-an-address-zz"),
					CoinType: proto.String(longSuiType),
					Amount:   proto.String("1"),
				},
				{
					Address:  proto.String(longOwnerAddress),
					CoinType: proto.String(longSuiType),
					Amount:   proto.String("-100"),
				},
			},
		}, types.SuiTransactionBlockResponseOptions{ShowEvents: true, ShowBalanceChanges: true})
		// The parseable balance change is still returned; the dropped event
		// and balance change are reported.
		require.Len(t, got.BalanceChanges, 1)
		require.Empty(t, got.Events)
		require.Len(t, got.Errors, 2)
	})
}

func TestExecutionResults(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		require.Nil(t, ExecutionResults(nil))
		require.Nil(t, ExecutionResults([]*pb.CommandResult{}))
	})

	t.Run("return values and mutated by ref", func(t *testing.T) {
		got := ExecutionResults([]*pb.CommandResult{
			{
				MutatedByRef: []*pb.CommandOutput{{
					Argument: &pb.Argument{Kind: pb.Argument_GAS.Enum()},
					Value: &pb.Bcs{
						Name:  proto.String(longSuiPackage + "::coin::Coin<" + longSuiType + ">"),
						Value: []byte{0x01, 0x02},
					},
				}},
				ReturnValues: []*pb.CommandOutput{{
					Argument: &pb.Argument{
						Kind:      pb.Argument_RESULT.Enum(),
						Result:    proto.Uint32(0),
						Subresult: proto.Uint32(1),
					},
					Value: &pb.Bcs{Name: proto.String("u64"), Value: []byte{0xe8, 0x03}},
				}},
			},
			{}, // command without outputs
		})
		require.Equal(t, []types.ExecutionResultType{
			{
				MutableReferenceOutputs: []types.MutableReferenceOutputType{
					[]any{"GasCoin", []byte{0x01, 0x02}, "0x2::coin::Coin<" + shortSuiType + ">"},
				},
				ReturnValues: []types.ReturnValueType{
					[]any{[]byte{0xe8, 0x03}, "u64"},
				},
			},
			{},
		}, got)
	})
}

func TestCommandArgument(t *testing.T) {
	require.Equal(t, "GasCoin", commandArgument(&pb.Argument{Kind: pb.Argument_GAS.Enum()}))
	require.Equal(t, map[string]any{"Input": uint32(2)},
		commandArgument(&pb.Argument{Kind: pb.Argument_INPUT.Enum(), Input: proto.Uint32(2)}))
	require.Equal(t, map[string]any{"Result": uint32(3)},
		commandArgument(&pb.Argument{Kind: pb.Argument_RESULT.Enum(), Result: proto.Uint32(3)}))
	require.Equal(t, map[string]any{"NestedResult": []any{uint32(3), uint32(1)}},
		commandArgument(&pb.Argument{Kind: pb.Argument_RESULT.Enum(), Result: proto.Uint32(3), Subresult: proto.Uint32(1)}))
	require.Nil(t, commandArgument(nil))
}
