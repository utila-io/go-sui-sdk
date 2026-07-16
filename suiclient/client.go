// Package suiclient provides a backend-neutral Sui client: the same SuiClient
// interface served by JSON-RPC (the default) or the sui.rpc.v2 gRPC backend.
package suiclient

import (
	"context"
	"io"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// CoinPage is a page of coins. Unlike types.CoinPage, the cursor is a string:
// both backends' wire cursors are opaque base64, not an ObjectID.
type CoinPage = types.Page[types.Coin, string]

// SuiClient is the surface implemented by both the JSON-RPC and gRPC
// backends; it contains only methods every backend fully supports.
type SuiClient interface {
	// Coins & balances

	// GetBalance returns the combined balance (coin objects + SIP-58 address
	// balance). An empty coinType defaults to 0x2::sui::SUI.
	GetBalance(ctx context.Context, owner sui_types.SuiAddress, coinType string) (*types.Balance, error)
	GetAllBalances(ctx context.Context, owner sui_types.SuiAddress) ([]types.Balance, error)
	// GetCoins returns a page of coin objects; a nil coinType defaults to
	// 0x2::sui::SUI. Divergence: JSON-RPC synthesizes a pseudo-coin for the
	// owner's SIP-58 address balance (no on-chain object, unusable as an
	// object ref); gRPC returns real coin objects only — use GetBalance's
	// FundsInAddressBalance for that portion.
	GetCoins(ctx context.Context, owner sui_types.SuiAddress, coinType *string, cursor *string, limit uint) (*CoinPage, error)
	GetCoinMetadata(ctx context.Context, coinType string) (*types.SuiCoinMetadata, error)
	GetReferenceGasPrice(ctx context.Context) (*types.SafeSuiBigInt[uint64], error)

	// Objects

	GetObject(ctx context.Context, objID sui_types.ObjectID, options *types.SuiObjectDataOptions) (*types.SuiObjectResponse, error)
	MultiGetObjects(ctx context.Context, objIDs []sui_types.ObjectID, options *types.SuiObjectDataOptions) ([]types.SuiObjectResponse, error)

	// Transactions

	GetTransactionBlock(ctx context.Context, digest sui_types.TransactionDigest, options types.SuiTransactionBlockResponseOptions) (*types.SuiTransactionBlockResponse, error)
	MultiGetTransactionBlocks(ctx context.Context, digests []sui_types.TransactionDigest, options types.SuiTransactionBlockResponseOptions) ([]*types.SuiTransactionBlockResponse, error)
	// ExecuteTransactionBlock submits a signed transaction: BCS
	// TransactionData bytes plus sui_types.Signature values (or base64
	// strings of serialized signatures).
	ExecuteTransactionBlock(ctx context.Context, txBytes lib.Base64Data, signatures []any, options *types.SuiTransactionBlockResponseOptions, requestType types.ExecuteTransactionRequestType) (*types.SuiTransactionBlockResponse, error)
	DryRunTransaction(ctx context.Context, txBytes lib.Base64Data) (*types.DryRunTransactionBlockResponse, error)
	// DevInspectTransactionBlock simulates a bare TransactionKind (BCS bytes)
	// without requiring gas payment or signatures.
	DevInspectTransactionBlock(ctx context.Context, sender sui_types.SuiAddress, txKindBytes lib.Base64Data, gasPrice *types.SafeSuiBigInt[uint64], epoch *uint64) (*types.DevInspectResults, error)

	// Checkpoints

	GetLatestCheckpointSequenceNumber(ctx context.Context) (string, error)
	GetCheckpoint(ctx context.Context, seqNum uint64) (*types.Checkpoint, error)
	// GetCheckpoints returns up to limit sequential checkpoints starting at
	// startSeqNum (inclusive), in ascending order.
	GetCheckpoints(ctx context.Context, startSeqNum uint64, limit int) ([]*types.Checkpoint, error)
	GetCheckpointTransactions(ctx context.Context, seqNum uint64, options types.SuiTransactionBlockResponseOptions) ([]*types.SuiTransactionBlockResponse, error)

	// Closer releases backend resources (no-op for JSON-RPC; closes the
	// gRPC connection for the gRPC backend).
	io.Closer
}
