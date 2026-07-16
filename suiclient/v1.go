package suiclient

import (
	"context"
	"fmt"

	"github.com/utila-io/go-sui-sdk/client"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// jsonrpcBackend adapts *client.Client to SuiClient. Methods whose v1
// signatures diverge from the interface go through CallContext directly.
type jsonrpcBackend struct {
	c *client.Client
}

const (
	// multiGetTransactionsChunkSize caps digests per
	// sui_multiGetTransactionBlocks call (the JSON-RPC multi-get limit).
	multiGetTransactionsChunkSize = 50
	// checkpointsPageLimit is the maximum page size sui_getCheckpoints
	// accepts (the JSON-RPC query result limit).
	checkpointsPageLimit = 100
)

var _ SuiClient = (*jsonrpcBackend)(nil)

// MARK - Coins & balances

func (b *jsonrpcBackend) GetBalance(ctx context.Context, owner sui_types.SuiAddress, coinType string) (*types.Balance, error) {
	return b.c.GetBalance(ctx, owner, coinType)
}

func (b *jsonrpcBackend) GetAllBalances(ctx context.Context, owner sui_types.SuiAddress) ([]types.Balance, error) {
	return b.c.GetAllBalances(ctx, owner)
}

// GetCoins goes through CallContext instead of the typed v1 method:
// types.CoinPage declares its cursor as an ObjectID, but the real wire cursor
// is an opaque base64 string.
func (b *jsonrpcBackend) GetCoins(
	ctx context.Context,
	owner sui_types.SuiAddress,
	coinType *string,
	cursor *string,
	limit uint,
) (*CoinPage, error) {
	var resp CoinPage
	return &resp, b.c.CallContext(ctx, &resp, client.SuiXMethod("getCoins"), owner, coinType, cursor, limit)
}

func (b *jsonrpcBackend) GetCoinMetadata(ctx context.Context, coinType string) (*types.SuiCoinMetadata, error) {
	return b.c.GetCoinMetadata(ctx, coinType)
}

func (b *jsonrpcBackend) GetReferenceGasPrice(ctx context.Context) (*types.SafeSuiBigInt[uint64], error) {
	return b.c.GetReferenceGasPrice(ctx)
}

// MARK - Objects

func (b *jsonrpcBackend) GetObject(
	ctx context.Context,
	objID sui_types.ObjectID,
	options *types.SuiObjectDataOptions,
) (*types.SuiObjectResponse, error) {
	return b.c.GetObject(ctx, objID, options)
}

func (b *jsonrpcBackend) MultiGetObjects(
	ctx context.Context,
	objIDs []sui_types.ObjectID,
	options *types.SuiObjectDataOptions,
) ([]types.SuiObjectResponse, error) {
	return b.c.MultiGetObjects(ctx, objIDs, options)
}

// MARK - Transactions

func (b *jsonrpcBackend) GetTransactionBlock(
	ctx context.Context,
	digest sui_types.TransactionDigest,
	options types.SuiTransactionBlockResponseOptions,
) (*types.SuiTransactionBlockResponse, error) {
	return b.c.GetTransactionBlock(ctx, digest, options)
}

func (b *jsonrpcBackend) MultiGetTransactionBlocks(
	ctx context.Context,
	digests []sui_types.TransactionDigest,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	responses := make([]*types.SuiTransactionBlockResponse, 0, len(digests))
	for start := 0; start < len(digests); start += multiGetTransactionsChunkSize {
		end := min(start+multiGetTransactionsChunkSize, len(digests))
		var chunk []*types.SuiTransactionBlockResponse
		err := b.c.CallContext(ctx, &chunk, client.SuiMethod("multiGetTransactionBlocks"), digests[start:end], options)
		if err != nil {
			return nil, err
		}
		responses = append(responses, chunk...)
	}
	return responses, nil
}

func (b *jsonrpcBackend) ExecuteTransactionBlock(
	ctx context.Context, txBytes lib.Base64Data, signatures []any,
	options *types.SuiTransactionBlockResponseOptions, requestType types.ExecuteTransactionRequestType,
) (*types.SuiTransactionBlockResponse, error) {
	return b.c.ExecuteTransactionBlock(ctx, txBytes, signatures, options, requestType)
}

func (b *jsonrpcBackend) DryRunTransaction(
	ctx context.Context,
	txBytes lib.Base64Data,
) (*types.DryRunTransactionBlockResponse, error) {
	return b.c.DryRunTransaction(ctx, txBytes)
}

func (b *jsonrpcBackend) DevInspectTransactionBlock(
	ctx context.Context,
	sender sui_types.SuiAddress,
	txKindBytes lib.Base64Data,
	gasPrice *types.SafeSuiBigInt[uint64],
	epoch *uint64,
) (*types.DevInspectResults, error) {
	return b.c.DevInspectTransactionBlock(ctx, sender, txKindBytes, gasPrice, epoch)
}

// MARK - Checkpoints

func (b *jsonrpcBackend) GetLatestCheckpointSequenceNumber(ctx context.Context) (string, error) {
	return b.c.GetLatestCheckpointSequenceNumber(ctx)
}

func (b *jsonrpcBackend) GetCheckpoint(ctx context.Context, seqNum uint64) (*types.Checkpoint, error) {
	// The checkpoint id parameter is a BigInt encoded as a decimal string.
	var resp types.Checkpoint
	return &resp, b.c.CallContext(ctx, &resp, client.SuiMethod("getCheckpoint"), fmt.Sprint(seqNum))
}

func (b *jsonrpcBackend) GetCheckpoints(ctx context.Context, startSeqNum uint64, limit int) ([]*types.Checkpoint, error) {
	if limit <= 0 {
		return nil, nil
	}
	// The sui_getCheckpoints cursor is exclusive: nil starts from genesis,
	// otherwise pass the checkpoint just before startSeqNum.
	var cursor any
	if startSeqNum > 0 {
		cursor = fmt.Sprint(startSeqNum - 1)
	}

	var checkpoints []*types.Checkpoint
	for len(checkpoints) < limit {
		pageSize := min(limit-len(checkpoints), checkpointsPageLimit)
		var page struct {
			Data        []*types.Checkpoint `json:"data"`
			NextCursor  string              `json:"nextCursor"`
			HasNextPage bool                `json:"hasNextPage"`
		}
		err := b.c.CallContext(ctx, &page, client.SuiMethod("getCheckpoints"), cursor, pageSize, false /* descending */)
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, page.Data...)
		// The empty-data check guards against re-fetching the same page
		// forever should the node ever report hasNextPage without advancing.
		if !page.HasNextPage || page.NextCursor == "" || len(page.Data) == 0 {
			break
		}
		cursor = page.NextCursor
	}
	return checkpoints, nil
}

func (b *jsonrpcBackend) GetCheckpointTransactions(
	ctx context.Context,
	seqNum uint64,
	options types.SuiTransactionBlockResponseOptions,
) ([]*types.SuiTransactionBlockResponse, error) {
	// The Checkpoint filter must be a BigInt encoded as a decimal string, so
	// the query is built as a map (types.SuiTransactionBlockResponseQuery's
	// filter field is numeric).
	query := map[string]any{
		"filter":  map[string]string{"Checkpoint": fmt.Sprint(seqNum)},
		"options": options,
	}

	var (
		transactions []*types.SuiTransactionBlockResponse
		cursor       *sui_types.TransactionDigest
	)
	for {
		var page struct {
			Data        []*types.SuiTransactionBlockResponse `json:"data"`
			NextCursor  *sui_types.TransactionDigest         `json:"nextCursor"`
			HasNextPage bool                                 `json:"hasNextPage"`
		}
		err := b.c.CallContext(ctx, &page, client.SuiXMethod("queryTransactionBlocks"), query, cursor)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, page.Data...)
		// The nil-cursor check guards against re-fetching the first page
		// forever should the node ever report hasNextPage without a cursor.
		if !page.HasNextPage || page.NextCursor == nil {
			return transactions, nil
		}
		cursor = page.NextCursor
	}
}

// Close is a no-op: the JSON-RPC backend holds no persistent connection.
func (b *jsonrpcBackend) Close() error {
	return nil
}
