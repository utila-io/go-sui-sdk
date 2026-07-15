// Package suiclient provides a backend-neutral Sui client. The same SuiClient
// interface is served by either the legacy JSON-RPC backend (the client
// package) or the sui.rpc.v2 gRPC backend (the clientv2 package), selected
// with WithBackend. JSON-RPC is the default until Sui's public JSON-RPC
// endpoints are retired.
package suiclient

import (
	"context"
	"io"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// CoinPage is a page of coins with an opaque pagination cursor.
//
// Unlike types.CoinPage, the cursor is a string: the JSON-RPC getCoins cursor
// is a base64 string (not an ObjectID), and the gRPC backend's page token is
// opaque bytes surfaced as base64.
type CoinPage = types.Page[types.Coin, string]

// SuiClient is the surface implemented by both the JSON-RPC and gRPC
// backends. It intentionally contains only methods every backend fully
// supports; backend-specific extras remain on the concrete client types.
type SuiClient interface {
	// Coins & balances

	// GetBalance returns the combined balance (coin objects + SIP-58 address
	// balance) for the given coin type. An empty coinType defaults to
	// 0x2::sui::SUI.
	GetBalance(ctx context.Context, owner sui_types.SuiAddress, coinType string) (*types.Balance, error)
	GetAllBalances(ctx context.Context, owner sui_types.SuiAddress) ([]types.Balance, error)
	// GetCoins returns a page of coin objects owned by owner. A nil coinType
	// defaults to 0x2::sui::SUI. The cursor is an opaque string from a
	// previous page's NextCursor.
	//
	// Backend divergence: JSON-RPC additionally synthesizes a pseudo-coin
	// representing the owner's SIP-58 address balance (it has no on-chain
	// object and cannot be fetched or used as an object ref); the gRPC
	// backend returns real coin objects only. Use GetBalance's
	// FundsInAddressBalance for the address-balance portion.
	GetCoins(ctx context.Context, owner sui_types.SuiAddress, coinType *string, cursor *string, limit uint) (*CoinPage, error)
	GetCoinMetadata(ctx context.Context, coinType string) (*types.SuiCoinMetadata, error)
	GetReferenceGasPrice(ctx context.Context) (*types.SafeSuiBigInt[uint64], error)

	// Objects

	GetObject(ctx context.Context, objID sui_types.ObjectID, options *types.SuiObjectDataOptions) (*types.SuiObjectResponse, error)
	MultiGetObjects(ctx context.Context, objIDs []sui_types.ObjectID, options *types.SuiObjectDataOptions) ([]types.SuiObjectResponse, error)

	// Transactions

	GetTransactionBlock(ctx context.Context, digest sui_types.TransactionDigest, options types.SuiTransactionBlockResponseOptions) (*types.SuiTransactionBlockResponse, error)
	MultiGetTransactionBlocks(ctx context.Context, digests []sui_types.TransactionDigest, options types.SuiTransactionBlockResponseOptions) ([]*types.SuiTransactionBlockResponse, error)
	// ExecuteTransactionBlock submits a signed transaction. txBytes is the BCS
	// serialization of TransactionData; signatures are sui_types.Signature
	// values (or base64 strings of serialized signatures).
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
	// GetCheckpointTransactions returns every transaction in the checkpoint,
	// with the response shaped per options. It replaces JSON-RPC
	// queryTransactionBlocks with a Checkpoint filter and paginates
	// internally.
	GetCheckpointTransactions(ctx context.Context, seqNum uint64, options types.SuiTransactionBlockResponseOptions) ([]*types.SuiTransactionBlockResponse, error)

	// Closer releases backend resources (no-op for JSON-RPC; closes the
	// gRPC connection for the gRPC backend).
	io.Closer
}
