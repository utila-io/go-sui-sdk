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

func TestExecuteTransactionBlockNoTransaction(t *testing.T) {
	client, mocks := newMockClient(t)
	mocks.exec.EXPECT().
		ExecuteTransaction(gomock.Any(), gomock.Any()).
		Return(&pb.ExecuteTransactionResponse{}, nil)
	_, err := client.ExecuteTransactionBlock(context.Background(), lib.Base64Data{0x01},
		nil, nil, types.TxnRequestTypeWaitForLocalExecution)
	require.ErrorContains(t, err, "node returned no transaction")
}

func TestDryRunTransaction(t *testing.T) {
	client, mocks := newMockClient(t)
	sender := testAddress(t)
	// valid TransactionData (empty ProgrammableTransaction kind) so the object
	// changes' sender decodes from the echoed transaction BCS
	txData := pb.DevInspectTransactionData(sender, []byte{0x00, 0x00, 0x00}, 1000)
	objectID := "0x1a2b3c4d5e6f00112233445566778899aabbccddeeff00112233445566778899"
	mutated := &pb.ChangedObject{
		ObjectId:      proto.String(objectID),
		InputState:    pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS.Enum(),
		InputVersion:  proto.Uint64(5),
		OutputState:   pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE.Enum(),
		OutputVersion: proto.Uint64(6),
		OutputDigest:  proto.String(testDigest(7).String()),
		OutputOwner:   &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String(sender.String())},
		IdOperation:   pb.ChangedObject_NONE.Enum(),
		ObjectType:    proto.String("0x2::coin::Coin<0x2::sui::SUI>"),
	}

	mocks.exec.EXPECT().
		SimulateTransaction(gomock.Any(), protoEqual(&pb.SimulateTransactionRequest{
			Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: txData}},
			ReadMask: &fieldmaskpb.FieldMask{Paths: []string{
				"transaction.digest", "transaction.checkpoint", "transaction.timestamp",
				"transaction.transaction.bcs", "transaction.effects", "transaction.events",
				"transaction.balance_changes",
			}},
			Checks: pb.SimulateTransactionRequest_ENABLED.Enum(),
		})).
		Return(&pb.SimulateTransactionResponse{
			Transaction: &pb.ExecutedTransaction{
				Digest:      proto.String(testDigest(3).String()),
				Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: txData}},
				Effects:     &pb.TransactionEffects{ChangedObjects: []*pb.ChangedObject{mutated}},
			},
		}, nil)

	response, err := client.DryRunTransaction(context.Background(), lib.Base64Data(txData))
	require.NoError(t, err)
	require.Len(t, response.ObjectChanges, 1)
	change := response.ObjectChanges[0].Data
	require.NotNil(t, change.Mutated)
	require.Equal(t, sender, change.Mutated.Sender)
	require.Equal(t, objectID, change.Mutated.ObjectId.String())
	require.Equal(t, "0x2::coin::Coin<0x2::sui::SUI>", change.Mutated.ObjectType)
	require.Equal(t, uint64(6), change.Mutated.Version.Uint64())
	require.Equal(t, uint64(5), change.Mutated.PreviousVersion.Uint64())
	require.NotNil(t, change.Mutated.Owner.ObjectOwnerInternal)
	require.Equal(t, &sender, change.Mutated.Owner.AddressOwner)
}

func TestDryRunTransactionNoTransaction(t *testing.T) {
	client, mocks := newMockClient(t)
	mocks.exec.EXPECT().
		SimulateTransaction(gomock.Any(), gomock.Any()).
		Return(&pb.SimulateTransactionResponse{}, nil)
	_, err := client.DryRunTransaction(context.Background(), lib.Base64Data{0x01})
	require.ErrorContains(t, err, "node returned no transaction")
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
						Value: pb.DevInspectTransactionData(sender, txKindBytes.Data(), c.wantPrice),
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
