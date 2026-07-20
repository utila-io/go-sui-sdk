package rpcv2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestCheckpoint_full(t *testing.T) {
	cpDigestStr, cpDigest := testDigest(0x61)
	prevDigestStr, prevDigest := testDigest(0x62)
	tx1Str, tx1 := testDigest(0x63)
	tx2Str, tx2 := testDigest(0x64)

	got := (&Checkpoint{
		SequenceNumber: proto.Uint64(123456789),
		Digest:         proto.String(cpDigestStr),
		Summary: &CheckpointSummary{
			Epoch:                    proto.Uint64(42),
			TotalNetworkTransactions: proto.Uint64(987654321),
			PreviousDigest:           proto.String(prevDigestStr),
			EpochRollingGasCostSummary: &GasCostSummary{
				ComputationCost:         proto.Uint64(100),
				StorageCost:             proto.Uint64(200),
				StorageRebate:           proto.Uint64(30),
				NonRefundableStorageFee: proto.Uint64(3),
			},
			Timestamp: timestamppb.New(time.UnixMilli(1700000000123)),
		},
		Signature: &ValidatorAggregatedSignature{Signature: []byte{0x01, 0x02, 0x03}},
		Transactions: []*ExecutedTransaction{
			{Digest: proto.String(tx1Str)},
			{Digest: proto.String(tx2Str)},
		},
	}).ToInternalType()

	require.Equal(t, &types.Checkpoint{
		Epoch:                    types.NewSafeSuiBigInt[types.EpochId](42),
		SequenceNumber:           types.NewSafeSuiBigInt[uint64](123456789),
		Digest:                   cpDigest,
		NetworkTotalTransactions: types.NewSafeSuiBigInt[uint64](987654321),
		PreviousDigest:           &prevDigest,
		EpochRollingGasCostSummary: types.GasCostSummary{
			ComputationCost:         types.NewSafeSuiBigInt[uint64](100),
			StorageCost:             types.NewSafeSuiBigInt[uint64](200),
			StorageRebate:           types.NewSafeSuiBigInt[uint64](30),
			NonRefundableStorageFee: types.NewSafeSuiBigInt[uint64](3),
		},
		TimestampMs:        types.NewSafeSuiBigInt[uint64](1700000000123),
		Transactions:       []*sui_types.TransactionDigest{&tx1, &tx2},
		ValidatorSignature: "AQID", // base64 of 0x01 0x02 0x03
	}, got)

	// SafeSuiBigInt fields marshal as JSON strings. gRPC carries them as numeric
	// uint64; the internal types re-encode them as strings for consumers.
	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(data), `"epoch":"42"`)
	require.Contains(t, string(data), `"sequenceNumber":"123456789"`)
	require.Contains(t, string(data), `"timestampMs":"1700000000123"`)
}

// TestCheckpoint_minimal covers absent optional summary fields: no timestamp,
// no previous digest, no signature and no transactions.
func TestCheckpoint_minimal(t *testing.T) {
	cpDigestStr, cpDigest := testDigest(0x65)

	got := (&Checkpoint{
		SequenceNumber: proto.Uint64(1),
		Digest:         proto.String(cpDigestStr),
		Summary: &CheckpointSummary{
			Epoch: proto.Uint64(0),
		},
	}).ToInternalType()

	require.Equal(t, &types.Checkpoint{
		Epoch:          types.NewSafeSuiBigInt[types.EpochId](0),
		SequenceNumber: types.NewSafeSuiBigInt[uint64](1),
		Digest:         cpDigest,
	}, got)
	require.Nil(t, got.PreviousDigest)
	require.Nil(t, got.Transactions)
	require.Empty(t, got.ValidatorSignature)
}
