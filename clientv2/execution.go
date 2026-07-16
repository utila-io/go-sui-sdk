package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// simulateReadMaskPrefix rebases ExecutedTransaction read mask paths onto the
// SimulateTransactionResponse, whose mask is response-relative (unlike
// ExecuteTransaction, whose mask is ExecutedTransaction-relative).
const simulateReadMaskPrefix = "transaction."

// simulateOptions is what simulate fetches for every simulation; the input is
// what the caller supplied, so it is never echoed back.
var simulateOptions = types.SuiTransactionBlockResponseOptions{
	ShowEffects:        true,
	ShowEvents:         true,
	ShowBalanceChanges: true,
}

// dryRunOptions additionally derives object changes, which v1 dry runs return;
// that pulls transaction.bcs and effects into the read mask.
var dryRunOptions = types.SuiTransactionBlockResponseOptions{
	ShowEffects:        true,
	ShowEvents:         true,
	ShowObjectChanges:  true,
	ShowBalanceChanges: true,
}

// ExecuteTransactionBlock submits a signed transaction. requestType is
// ignored: gRPC execution always waits for effects, which is at least as
// strong as WaitForEffectsCert.
func (c *Client) ExecuteTransactionBlock(
	ctx context.Context,
	txBytes lib.Base64Data,
	signatures []any,
	options *types.SuiTransactionBlockResponseOptions,
	requestType types.ExecuteTransactionRequestType,
) (*types.SuiTransactionBlockResponse, error) {
	userSignatures := make([]*pb.UserSignature, len(signatures))
	for i, signature := range signatures {
		sigBytes, err := adapt.SignatureBytes(signature)
		if err != nil {
			return nil, fmt.Errorf("ExecuteTransactionBlock: signature %d: %w", i, err)
		}
		userSignatures[i] = &pb.UserSignature{Bcs: &pb.Bcs{Value: sigBytes}}
	}
	opts := types.SuiTransactionBlockResponseOptions{}
	if options != nil {
		opts = *options
	}
	resp, err := c.exec.ExecuteTransaction(ctx, &pb.ExecuteTransactionRequest{
		Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: txBytes.Data()}},
		Signatures:  userSignatures,
		ReadMask:    &fieldmaskpb.FieldMask{Paths: adapt.ResponseReadMaskPaths(opts)},
	})
	if err != nil {
		return nil, fmt.Errorf("ExecuteTransactionBlock: %w", err)
	}
	response := adapt.Response(resp.GetTransaction(), opts)
	if response == nil {
		return nil, fmt.Errorf("ExecuteTransactionBlock: node returned no transaction")
	}
	return response, nil
}

// DryRunTransaction simulates a full BCS TransactionData with checks enabled.
// The response's Input field is not reconstructed and stays zero.
func (c *Client) DryRunTransaction(ctx context.Context, txBytes lib.Base64Data) (*types.DryRunTransactionBlockResponse, error) {
	resp, err := c.simulate(ctx, txBytes.Data(), pb.SimulateTransactionRequest_ENABLED, dryRunOptions)
	if err != nil {
		return nil, fmt.Errorf("DryRunTransaction: %w", err)
	}
	response := adapt.Response(resp.GetTransaction(), dryRunOptions)
	if response == nil {
		return nil, fmt.Errorf("DryRunTransaction: node returned no transaction")
	}
	out := &types.DryRunTransactionBlockResponse{
		Events:         response.Events,
		ObjectChanges:  response.ObjectChanges,
		BalanceChanges: response.BalanceChanges,
	}
	if response.Effects != nil {
		out.Effects = *response.Effects
	}
	return out, nil
}

// DevInspectTransactionBlock simulates a bare TransactionKind (BCS bytes) with
// checks disabled. epoch is ignored (gRPC always simulates at the current
// epoch); a nil gasPrice defaults to the reference gas price.
func (c *Client) DevInspectTransactionBlock(
	ctx context.Context,
	sender sui_types.SuiAddress,
	txKindBytes lib.Base64Data,
	gasPrice *types.SafeSuiBigInt[uint64],
	epoch *uint64,
) (*types.DevInspectResults, error) {
	price := uint64(0)
	if gasPrice != nil {
		price = gasPrice.Uint64()
	}
	if price == 0 {
		referencePrice, err := c.GetReferenceGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("DevInspectTransactionBlock: %w", err)
		}
		price = referencePrice.Uint64()
	}
	txData := adapt.DevInspectTransactionData(sender, txKindBytes.Data(), price)
	resp, err := c.simulate(ctx, txData, pb.SimulateTransactionRequest_DISABLED, simulateOptions, "command_outputs")
	if err != nil {
		return nil, fmt.Errorf("DevInspectTransactionBlock: %w", err)
	}
	response := adapt.Response(resp.GetTransaction(), simulateOptions)
	if response == nil {
		return nil, fmt.Errorf("DevInspectTransactionBlock: node returned no transaction")
	}
	out := &types.DevInspectResults{
		Events:  response.Events,
		Results: adapt.ExecutionResults(resp.GetCommandOutputs()),
	}
	if response.Effects != nil {
		out.Effects = *response.Effects
		if response.Effects.Data.V1 != nil && response.Effects.Data.V1.Status.Error != "" {
			execError := response.Effects.Data.V1.Status.Error
			out.Error = &execError
		}
	}
	return out, nil
}

// simulate runs SimulateTransaction on raw BCS TransactionData bytes.
// extraPaths are additional response-relative read mask paths (e.g.
// "command_outputs").
func (c *Client) simulate(
	ctx context.Context,
	txData []byte,
	checks pb.SimulateTransactionRequest_TransactionChecks,
	options types.SuiTransactionBlockResponseOptions,
	extraPaths ...string,
) (*pb.SimulateTransactionResponse, error) {
	paths := adapt.PrefixPaths(simulateReadMaskPrefix, adapt.ResponseReadMaskPaths(options))
	paths = append(paths, extraPaths...)
	return c.exec.SimulateTransaction(ctx, &pb.SimulateTransactionRequest{
		Transaction: &pb.Transaction{Bcs: &pb.Bcs{Value: txData}},
		ReadMask:    &fieldmaskpb.FieldMask{Paths: paths},
		Checks:      checks.Enum(),
	})
}
