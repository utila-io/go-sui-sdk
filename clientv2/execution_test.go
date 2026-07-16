package clientv2

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestExecuteTransactionBlock(t *testing.T) {
	client, mocks := newMockClient(t)

	var ed25519Sig sui_types.Ed25519SuiSignature
	for i := range ed25519Sig.Signature {
		ed25519Sig.Signature[i] = byte(i + 1)
	}
	rawStringSig := []byte{0x01, 0x05, 0x06, 0x07}
	signatures := []any{
		sui_types.Signature{Ed25519SuiSignature: &ed25519Sig},
		base64.StdEncoding.EncodeToString(rawStringSig),
	}
	txBytes := lib.Base64Data([]byte{0xde, 0xad, 0xbe, 0xef})
	digest := testDigest(9).String()

	mocks.exec.EXPECT().
		ExecuteTransaction(gomock.Any(), protoEqual(&pb.ExecuteTransactionRequest{
			Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: []byte{0xde, 0xad, 0xbe, 0xef}}},
			Signatures: []*pb.UserSignature{
				{Bcs: &pb.Bcs{Value: ed25519Sig.Signature[:]}},
				{Bcs: &pb.Bcs{Value: rawStringSig}},
			},
			ReadMask: defaultResponseReadMask,
		})).
		Return(&pb.ExecuteTransactionResponse{
			Transaction: &pb.ExecutedTransaction{Digest: proto.String(digest)},
		}, nil)

	response, err := client.ExecuteTransactionBlock(context.Background(), txBytes, signatures,
		nil, types.TxnRequestTypeWaitForLocalExecution)
	require.NoError(t, err)
	require.Equal(t, digest, response.Digest.String())
}

func TestExecuteTransactionBlockBadSignature(t *testing.T) {
	client, _ := newMockClient(t)
	_, err := client.ExecuteTransactionBlock(context.Background(), lib.Base64Data{0x01},
		[]any{42}, nil, types.TxnRequestTypeWaitForLocalExecution)
	require.ErrorContains(t, err, "signature 0")
}

func TestDevInspectTransactionBlock(t *testing.T) {
	sender := testAddress(t)
	txKindBytes := lib.Base64Data([]byte{0x00, 0x01, 0x02})
	price500 := types.NewSafeSuiBigInt(uint64(500))
	price0 := types.NewSafeSuiBigInt(uint64(0))

	cases := []struct {
		name          string
		gasPrice      *types.SafeSuiBigInt[uint64]
		fetchGasPrice bool
		wantPrice     uint64
	}{
		{name: "nil gas price fetches reference", gasPrice: nil, fetchGasPrice: true, wantPrice: 1000},
		{name: "zero gas price fetches reference", gasPrice: &price0, fetchGasPrice: true, wantPrice: 1000},
		{name: "explicit gas price skips fetch", gasPrice: &price500, wantPrice: 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, mocks := newMockClient(t)
			if c.fetchGasPrice {
				mocks.ledger.EXPECT().
					GetEpoch(gomock.Any(), protoEqual(&pb.GetEpochRequest{
						ReadMask: &fieldmaskpb.FieldMask{Paths: []string{"reference_gas_price"}},
					})).
					Return(&pb.GetEpochResponse{
						Epoch: &pb.Epoch{ReferenceGasPrice: proto.Uint64(1000)},
					}, nil)
			}
			mocks.exec.EXPECT().
				SimulateTransaction(gomock.Any(), protoEqual(&pb.SimulateTransactionRequest{
					Transaction: &pb.Transaction{Bcs: &pb.Bcs{
						Value: adapt.DevInspectTransactionData(sender, txKindBytes.Data(), c.wantPrice),
					}},
					ReadMask: &fieldmaskpb.FieldMask{Paths: []string{
						"transaction.digest", "transaction.checkpoint", "transaction.timestamp",
						"transaction.effects", "transaction.events", "transaction.balance_changes",
						"command_outputs",
					}},
					Checks: pb.SimulateTransactionRequest_DISABLED.Enum(),
				})).
				Return(&pb.SimulateTransactionResponse{
					Transaction: &pb.ExecutedTransaction{Digest: proto.String(testDigest(3).String())},
				}, nil)

			results, err := client.DevInspectTransactionBlock(context.Background(), sender,
				txKindBytes, c.gasPrice, nil)
			require.NoError(t, err)
			require.NotNil(t, results)
		})
	}
}
