//go:build live

// Parity tests: run every SuiClient method against the JSON-RPC (v1) and gRPC
// (v2) backends on Sui testnet and diff the results.
//
// Run with:
//
//	go test -tags live ./suiclient/ -run TestParity -v
//
// Endpoints (overridable via env):
//   - SUI_PARITY_GRPC_ENDPOINT    (default fullnode.testnet.sui.io:443)
//   - SUI_PARITY_JSONRPC_ENDPOINT (default: fullnode.testnet.sui.io, falling
//     back to public testnet JSON-RPC nodes — Mysten's fullnode has retired its
//     JSON-RPC route and answers 404 there as of mid-2026)
//
// Fixtures (a recent checkpoint, a transaction with balance changes, an
// address owning SUI coins and, when found, an address with SIP-58
// accumulator activity) are discovered at run time from the chain tip, since
// testnet fullnodes prune history and hard-coded fixtures would rot.
//
// Documented, tolerated backend gaps (asserted as such, not as equality):
//   - Balance.CoinObjectCount / LockedBalance: no gRPC source, zero on v2.
//   - Coin.LockedUntilEpoch: no gRPC source, nil on v2.
//   - DryRun .Input: not populated by the v2 backend.
//   - Parsed Transaction pure inputs: v2 only resolves pure input value types
//     from built-in command usage, not Move call signatures, so input/command
//     rendering is not compared field-for-field; the fields consumers rely on
//     (sender, gasData, txSignatures, raw bytes) are compared exactly.
//   - Checkpoint.ValidatorSignature: certificate aggregation differs between
//     nodes, compared for presence only.
//   - GetCoins on SIP-58 accounts: v1 lists the accumulator-derived Coin
//     object, v2's ListOwnedObjects object-type filter does not return it, so
//     the coin fixture is restricted to accounts without address balances.
//   - BalanceChanges on SIP-58 movements: v1 reports coin-object movements
//     and address-balance withdrawals (accumulator splits, e.g. gas paid from
//     an address balance) but not address-balance deposits (merges); gRPC
//     balance_changes count both, so the comparator nets the effects' merge
//     events out of the v2 side before comparing.
//   - Volatile values (live balances, coin lists, gas price at epoch
//     boundaries) are fetched back-to-back and the pair is retried on
//     mismatch.
package suiclient

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fardream/go-bcs/bcs"
	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

const (
	defaultGRPCEndpoint = "fullnode.testnet.sui.io:443"
	// volatileRetries is how many times a pair of volatile reads is retried
	// before the mismatch is reported as a failure.
	volatileRetries = 3
)

// jsonrpcEndpointCandidates is tried in order when SUI_PARITY_JSONRPC_ENDPOINT
// is unset. All serve the same testnet chain as the gRPC endpoint.
var jsonrpcEndpointCandidates = []string{
	"https://fullnode.testnet.sui.io:443",
	"https://rpc-testnet.suiscan.xyz",
	"https://sui-testnet-rpc.publicnode.com",
}

// parityEnv holds the two backends plus fixtures discovered from the chain.
type parityEnv struct {
	v1 SuiClient // JSON-RPC backend
	v2 SuiClient // gRPC backend

	jsonrpcEndpoint string

	seq       uint64                        // fixture checkpoint sequence number
	txDigests []sui_types.TransactionDigest // transactions of the fixture checkpoint
	txDigest  sui_types.TransactionDigest   // fixture transaction with balance changes
	coinAddr  sui_types.SuiAddress          // address owning a small number of SUI coins
	accumAddr *sui_types.SuiAddress         // address with SIP-58 address-balance activity, if found
}

var (
	envOnce sync.Once
	envVal  *parityEnv
	envErr  error
)

func parity(t *testing.T) *parityEnv {
	t.Helper()
	envOnce.Do(func() { envVal, envErr = setupParity() })
	require.NoError(t, envErr, "parity fixture setup failed")
	return envVal
}

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func setupParity() (*parityEnv, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	grpcEndpoint := os.Getenv("SUI_PARITY_GRPC_ENDPOINT")
	if grpcEndpoint == "" {
		grpcEndpoint = defaultGRPCEndpoint
	}
	v2, err := New(grpcEndpoint, WithBackend(BackendGRPC))
	if err != nil {
		return nil, fmt.Errorf("dial gRPC backend %s: %w", grpcEndpoint, err)
	}
	if _, err := v2.GetLatestCheckpointSequenceNumber(ctx); err != nil {
		return nil, fmt.Errorf("gRPC endpoint %s unusable: %w", grpcEndpoint, err)
	}

	env := &parityEnv{v2: v2}
	candidates := jsonrpcEndpointCandidates
	if fromEnv := os.Getenv("SUI_PARITY_JSONRPC_ENDPOINT"); fromEnv != "" {
		candidates = []string{fromEnv}
	}
	var probeErrs []error
	for _, endpoint := range candidates {
		v1, err := New(endpoint, WithBackend(BackendJSONRPC))
		if err != nil {
			probeErrs = append(probeErrs, fmt.Errorf("%s: %w", endpoint, err))
			continue
		}
		probeCtx, probeCancel := context.WithTimeout(ctx, 15*time.Second)
		_, err = v1.GetLatestCheckpointSequenceNumber(probeCtx)
		probeCancel()
		if err != nil {
			probeErrs = append(probeErrs, fmt.Errorf("%s: %w", endpoint, err))
			continue
		}
		env.v1 = v1
		env.jsonrpcEndpoint = endpoint
		break
	}
	if env.v1 == nil {
		return nil, fmt.Errorf("no usable JSON-RPC testnet endpoint: %v", probeErrs)
	}

	if err := env.discoverFixtures(ctx); err != nil {
		return nil, err
	}
	return env, nil
}

// discoverFixtures walks recent checkpoints (via the v1 backend) looking for a
// checkpoint containing a transaction with balance changes, a SUI-holding
// address with a modest coin count, and SIP-58 accumulator activity.
func (env *parityEnv) discoverFixtures(ctx context.Context) error {
	latestStr, err := env.v1.GetLatestCheckpointSequenceNumber(ctx)
	if err != nil {
		return fmt.Errorf("discover: latest checkpoint: %w", err)
	}
	latest, err := strconv.ParseUint(latestStr, 10, 64)
	if err != nil {
		return fmt.Errorf("discover: latest checkpoint %q: %w", latestStr, err)
	}

	options := types.SuiTransactionBlockResponseOptions{ShowEffects: true, ShowBalanceChanges: true}
	const scanWindow = 60
	// Start a little behind the tip so both nodes are guaranteed to have the
	// fixture checkpoint.
	for seq := latest - 5; seq > latest-5-scanWindow; seq-- {
		txs, err := env.v1.GetCheckpointTransactions(ctx, seq, options)
		if err != nil {
			return fmt.Errorf("discover: checkpoint %d transactions: %w", seq, err)
		}
		for _, tx := range txs {
			if env.accumAddr == nil && tx.Effects != nil && tx.Effects.Data.V1 != nil {
				for _, event := range tx.Effects.Data.V1.AccumulatorEvents {
					if addr, err := sui_types.NewAddressFromHex(event.Address); err == nil {
						env.accumAddr = addr
						break
					}
				}
			}
			if env.seq != 0 || len(tx.BalanceChanges) == 0 {
				continue
			}
			coinAddr, ok := env.pickCoinAddress(ctx, tx.BalanceChanges)
			if !ok {
				continue
			}
			env.seq = seq
			env.txDigest = tx.Digest
			env.coinAddr = coinAddr
			for _, checkpointTx := range txs {
				env.txDigests = append(env.txDigests, checkpointTx.Digest)
			}
		}
		if env.seq != 0 && env.accumAddr != nil {
			break
		}
	}
	if env.seq == 0 {
		return fmt.Errorf("discover: no checkpoint with balance changes in [%d, %d]", latest-5-scanWindow, latest-5)
	}
	return nil
}

// pickCoinAddress returns an AddressOwner from the balance changes that holds
// SUI coin objects in a count small enough to enumerate exhaustively (needed
// for order-insensitive GetCoins parity).
func (env *parityEnv) pickCoinAddress(ctx context.Context, changes []types.BalanceChange) (sui_types.SuiAddress, bool) {
	var singleCoinFallback *sui_types.SuiAddress
	for _, change := range changes {
		if change.CoinType != types.SUI_COIN_TYPE {
			continue
		}
		if change.Owner.ObjectOwnerInternal == nil || change.Owner.AddressOwner == nil {
			continue
		}
		addr := *change.Owner.AddressOwner
		balance, err := env.v1.GetBalance(ctx, addr, types.SUI_COIN_TYPE)
		if err != nil {
			continue
		}
		// Skip SIP-58 accounts: their funds live in an accumulator, exposed by
		// v1 getCoins as a derived Coin object that the gRPC backend's
		// ListOwnedObjects filter does not return (documented gap), and whose
		// balance moves too fast to compare.
		if !balance.FundsInAddressBalance.IsZero() {
			continue
		}
		// Prefer addresses with multiple coins so the pagination walk in
		// TestParityGetCoins crosses page boundaries.
		if balance.CoinObjectCount >= 2 && balance.CoinObjectCount <= 25 {
			return addr, true
		}
		if balance.CoinObjectCount == 1 && singleCoinFallback == nil {
			singleCoinFallback = &addr
		}
	}
	if singleCoinFallback != nil {
		return *singleCoinFallback, true
	}
	return sui_types.SuiAddress{}, false
}

// retryVolatile runs compare up to volatileRetries times, tolerating
// transient mismatches from live chain state moving between the paired reads.
func retryVolatile(t *testing.T, compare func() error) {
	t.Helper()
	var err error
	for attempt := 0; attempt < volatileRetries; attempt++ {
		if err = compare(); err == nil {
			return
		}
		t.Logf("volatile mismatch (attempt %d/%d): %v", attempt+1, volatileRetries, err)
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("still mismatched after %d attempts: %v", volatileRetries, err)
}

// MARK - Comparators

func requireSameCheckpoint(t *testing.T, want, got *types.Checkpoint) {
	t.Helper()
	require.Equal(t, want.SequenceNumber, got.SequenceNumber)
	require.Equal(t, want.Epoch, got.Epoch)
	require.Equal(t, want.Digest.String(), got.Digest.String())
	require.Equal(t, want.NetworkTotalTransactions, got.NetworkTotalTransactions)
	require.Equal(t, want.TimestampMs, got.TimestampMs)
	require.Equal(t, want.EpochRollingGasCostSummary, got.EpochRollingGasCostSummary)
	if want.PreviousDigest != nil || got.PreviousDigest != nil {
		require.NotNil(t, want.PreviousDigest)
		require.NotNil(t, got.PreviousDigest)
		require.Equal(t, want.PreviousDigest.String(), got.PreviousDigest.String())
	}
	require.Equal(t, digestStrings(want.Transactions), digestStrings(got.Transactions))
	// The aggregate certificate can be a different (equally valid) validator
	// subset on different nodes, so only its presence is comparable.
	require.NotEmpty(t, want.ValidatorSignature)
	require.NotEmpty(t, got.ValidatorSignature)
}

func digestStrings(digests []*sui_types.TransactionDigest) []string {
	out := make([]string, len(digests))
	for i, digest := range digests {
		out[i] = digest.String()
	}
	return out
}

// balanceChangeKeys renders balance changes into canonical sorted strings so
// the two backends can be compared order-insensitively.
func balanceChangeKeys(changes []types.BalanceChange) []string {
	keys := make([]string, 0, len(changes))
	for _, change := range changes {
		owner := "<none>"
		if change.Owner.ObjectOwnerInternal != nil && change.Owner.AddressOwner != nil {
			owner = change.Owner.AddressOwner.String()
		} else if change.Owner.ObjectOwnerInternal != nil && change.Owner.ObjectOwner != nil {
			owner = "obj:" + change.Owner.ObjectOwner.String()
		}
		keys = append(keys, owner+"|"+change.CoinType+"|"+change.Amount)
	}
	sort.Strings(keys)
	return keys
}

// withoutAddressBalanceMovements nets the effects' SIP-58 deposit (merge)
// events out of the gRPC backend's balance changes so they compare against
// v1, which leaves address-balance deposits out (documented gap, see the
// file header). Entries that net to zero are dropped.
func withoutAddressBalanceMovements(changes []types.BalanceChange, effects *lib.TagJson[types.SuiTransactionBlockEffects]) []types.BalanceChange {
	if effects == nil || effects.Data.V1 == nil || len(effects.Data.V1.AccumulatorEvents) == 0 {
		return changes
	}
	accumulated := map[string]*big.Int{}
	for _, event := range effects.Data.V1.AccumulatorEvents {
		if event.Value.Integer == nil || event.Operation != types.AccumulatorOperationMerge {
			continue
		}
		coinType, ok := strings.CutPrefix(event.Ty, "0x2::balance::Balance<")
		if !ok {
			continue
		}
		coinType = strings.TrimSuffix(coinType, ">")
		amount := new(big.Int).SetUint64(*event.Value.Integer)
		key := event.Address + "|" + coinType
		if total, ok := accumulated[key]; ok {
			total.Add(total, amount)
		} else {
			accumulated[key] = amount
		}
	}
	out := make([]types.BalanceChange, 0, len(changes))
	for _, change := range changes {
		if change.Owner.ObjectOwnerInternal != nil && change.Owner.AddressOwner != nil {
			key := change.Owner.AddressOwner.String() + "|" + change.CoinType
			if fromAccumulator, ok := accumulated[key]; ok {
				amount, parsed := new(big.Int).SetString(change.Amount, 10)
				if parsed {
					amount.Sub(amount, fromAccumulator)
					if amount.Sign() == 0 {
						continue
					}
					change.Amount = amount.String()
				}
			}
		}
		out = append(out, change)
	}
	return out
}

func eventTypes(events []types.SuiEvent) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.Type
	}
	return out
}

func compareTxResponse(want, got *types.SuiTransactionBlockResponse, options types.SuiTransactionBlockResponseOptions) error {
	if want.Digest.String() != got.Digest.String() {
		return fmt.Errorf("digest: v1 %s, v2 %s", want.Digest, got.Digest)
	}
	if (want.TimestampMs == nil) != (got.TimestampMs == nil) {
		return fmt.Errorf("%s: timestampMs presence: v1 %v, v2 %v", want.Digest, want.TimestampMs, got.TimestampMs)
	}
	if want.TimestampMs != nil && want.TimestampMs.Uint64() != got.TimestampMs.Uint64() {
		return fmt.Errorf("%s: timestampMs: v1 %d, v2 %d", want.Digest, want.TimestampMs.Uint64(), got.TimestampMs.Uint64())
	}
	if want.Checkpoint != nil && got.Checkpoint != nil && want.Checkpoint.Uint64() != got.Checkpoint.Uint64() {
		return fmt.Errorf("%s: checkpoint: v1 %d, v2 %d", want.Digest, want.Checkpoint.Uint64(), got.Checkpoint.Uint64())
	}
	if options.ShowEffects {
		if err := compareEffects(want.Effects, got.Effects); err != nil {
			return fmt.Errorf("%s: %w", want.Digest, err)
		}
	}
	if options.ShowBalanceChanges {
		gotChanges := got.BalanceChanges
		if options.ShowEffects {
			gotChanges = withoutAddressBalanceMovements(gotChanges, got.Effects)
		}
		wantKeys, gotKeys := balanceChangeKeys(want.BalanceChanges), balanceChangeKeys(gotChanges)
		if fmt.Sprint(wantKeys) != fmt.Sprint(gotKeys) {
			return fmt.Errorf("%s: balanceChanges: v1 %v, v2 %v", want.Digest, wantKeys, gotKeys)
		}
	}
	if options.ShowEvents {
		if len(want.Events) != len(got.Events) {
			return fmt.Errorf("%s: events: v1 %d, v2 %d", want.Digest, len(want.Events), len(got.Events))
		}
		if fmt.Sprint(eventTypes(want.Events)) != fmt.Sprint(eventTypes(got.Events)) {
			return fmt.Errorf("%s: event types: v1 %v, v2 %v", want.Digest, eventTypes(want.Events), eventTypes(got.Events))
		}
	}
	return nil
}

func compareEffects(want, got *lib.TagJson[types.SuiTransactionBlockEffects]) error {
	if want == nil || got == nil || want.Data.V1 == nil || got.Data.V1 == nil {
		return fmt.Errorf("effects presence: v1 %v, v2 %v", want, got)
	}
	wantV1, gotV1 := want.Data.V1, got.Data.V1
	if wantV1.Status != gotV1.Status {
		return fmt.Errorf("effects.status: v1 %+v, v2 %+v", wantV1.Status, gotV1.Status)
	}
	if wantV1.GasUsed != gotV1.GasUsed {
		return fmt.Errorf("effects.gasUsed: v1 %+v, v2 %+v", wantV1.GasUsed, gotV1.GasUsed)
	}
	if wantV1.ExecutedEpoch != gotV1.ExecutedEpoch {
		return fmt.Errorf("effects.executedEpoch: v1 %v, v2 %v", wantV1.ExecutedEpoch, gotV1.ExecutedEpoch)
	}
	if wantV1.TransactionDigest.String() != gotV1.TransactionDigest.String() {
		return fmt.Errorf("effects.transactionDigest: v1 %s, v2 %s", wantV1.TransactionDigest, gotV1.TransactionDigest)
	}
	if len(wantV1.AccumulatorEvents) > 0 || len(gotV1.AccumulatorEvents) > 0 {
		wantJSON, _ := json.Marshal(wantV1.AccumulatorEvents)
		gotJSON, _ := json.Marshal(gotV1.AccumulatorEvents)
		if string(wantJSON) != string(gotJSON) {
			return fmt.Errorf("effects.accumulatorEvents: v1 %s, v2 %s", wantJSON, gotJSON)
		}
	}
	return nil
}

// MARK - Checkpoints

func TestParityGetLatestCheckpointSequenceNumber(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	fromV1, err := env.v1.GetLatestCheckpointSequenceNumber(ctx)
	require.NoError(t, err)
	fromV2, err := env.v2.GetLatestCheckpointSequenceNumber(ctx)
	require.NoError(t, err)

	seqV1, err := strconv.ParseUint(fromV1, 10, 64)
	require.NoError(t, err, "v1 checkpoint height %q is not numeric", fromV1)
	seqV2, err := strconv.ParseUint(fromV2, 10, 64)
	require.NoError(t, err, "v2 checkpoint height %q is not numeric", fromV2)

	delta := int64(seqV2) - int64(seqV1)
	if delta < 0 {
		delta = -delta
	}
	// Two independent nodes at the same tip; testnet does ~5 checkpoints/s so
	// 600 is about two minutes of allowed lag.
	require.LessOrEqual(t, delta, int64(600), "checkpoint heights too far apart: v1 %d, v2 %d", seqV1, seqV2)
}

func TestParityGetCheckpoint(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	fromV1, err := env.v1.GetCheckpoint(ctx, env.seq)
	require.NoError(t, err)
	fromV2, err := env.v2.GetCheckpoint(ctx, env.seq)
	require.NoError(t, err)
	requireSameCheckpoint(t, fromV1, fromV2)
}

func TestParityGetCheckpoints(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	const limit = 3
	fromV1, err := env.v1.GetCheckpoints(ctx, env.seq, limit)
	require.NoError(t, err)
	fromV2, err := env.v2.GetCheckpoints(ctx, env.seq, limit)
	require.NoError(t, err)

	require.Len(t, fromV1, limit)
	require.Len(t, fromV2, limit)
	for i := range fromV1 {
		require.Equal(t, env.seq+uint64(i), fromV1[i].SequenceNumber.Uint64())
		requireSameCheckpoint(t, fromV1[i], fromV2[i])
	}
}

func TestParityGetCheckpointTransactions(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	options := types.SuiTransactionBlockResponseOptions{ShowEffects: true, ShowBalanceChanges: true}
	fromV1, err := env.v1.GetCheckpointTransactions(ctx, env.seq, options)
	require.NoError(t, err)
	fromV2, err := env.v2.GetCheckpointTransactions(ctx, env.seq, options)
	require.NoError(t, err)

	byDigestV1 := make(map[string]*types.SuiTransactionBlockResponse, len(fromV1))
	for _, tx := range fromV1 {
		byDigestV1[tx.Digest.String()] = tx
	}
	require.Len(t, fromV2, len(fromV1), "different transaction counts for checkpoint %d", env.seq)
	for _, txV2 := range fromV2 {
		txV1, ok := byDigestV1[txV2.Digest.String()]
		require.True(t, ok, "v2 returned digest %s missing from v1", txV2.Digest)
		require.NoError(t, compareTxResponse(txV1, txV2, options))
	}
}

// MARK - Transactions

// fullOptions asks for everything both backends can serve. ShowInput and
// ShowRawInput are exercised separately in TestParityRawTransaction, which
// pins down the raw-byte and parsed-transaction parity.
var fullOptions = types.SuiTransactionBlockResponseOptions{
	ShowEffects:        true,
	ShowEvents:         true,
	ShowBalanceChanges: true,
}

func TestParityGetTransactionBlock(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	fromV1, err := env.v1.GetTransactionBlock(ctx, env.txDigest, fullOptions)
	require.NoError(t, err)
	fromV2, err := env.v2.GetTransactionBlock(ctx, env.txDigest, fullOptions)
	require.NoError(t, err)
	require.NoError(t, compareTxResponse(fromV1, fromV2, fullOptions))
	require.NotEmpty(t, fromV1.BalanceChanges, "fixture transaction should have balance changes")
}

// TestParityRawTransaction covers ShowRawInput and ShowInput: both backends
// must return the identical BCS SenderSignedData bytes under ShowRawInput,
// and the parsed transaction fields consumers rely on (sender, gasData,
// txSignatures) must match exactly under ShowInput. PTB input/command
// rendering is only compared structurally: v2 cannot resolve pure input value
// types that JSON-RPC derives from on-chain Move signatures (see file header).
func TestParityRawTransaction(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)

	inputOptions := types.SuiTransactionBlockResponseOptions{ShowInput: true, ShowRawInput: true}
	fromV1, err := env.v1.GetTransactionBlock(ctx, env.txDigest, inputOptions)
	require.NoError(t, err)
	fromV2, err := env.v2.GetTransactionBlock(ctx, env.txDigest, inputOptions)
	require.NoError(t, err)

	// ShowRawInput: identical BCS SenderSignedData on both backends.
	require.NotEmpty(t, fromV1.RawTransaction, "v1 ShowRawInput should populate RawTransaction")
	require.Equal(t, fromV1.RawTransaction, fromV2.RawTransaction,
		"raw SenderSignedData bytes should be identical across backends")

	// ShowInput: parsed transaction parity on the fields consumers read.
	require.NotNil(t, fromV1.Transaction, "v1 ShowInput returns the parsed transaction")
	require.NotNil(t, fromV2.Transaction, "v2 ShowInput returns the parsed transaction (errors: %v)", fromV2.Errors)
	wantData, gotData := fromV1.Transaction.Data.Data.V1, fromV2.Transaction.Data.Data.V1
	require.NotNil(t, wantData)
	require.NotNil(t, gotData)
	require.Equal(t, wantData.Sender.String(), gotData.Sender.String(), "sender")
	require.Equal(t, wantData.GasData.Owner, gotData.GasData.Owner, "gasData.owner")
	require.Equal(t, wantData.GasData.Price.Uint64(), gotData.GasData.Price.Uint64(), "gasData.price")
	require.Equal(t, wantData.GasData.Budget.Uint64(), gotData.GasData.Budget.Uint64(), "gasData.budget")
	require.Equal(t, wantData.GasData.Payment, gotData.GasData.Payment, "gasData.payment")
	require.Equal(t, fromV1.Transaction.TxSignatures, fromV2.Transaction.TxSignatures, "txSignatures")

	// Kind parity: same variant with the same PTB input/command counts.
	wantPTB, gotPTB := wantData.Transaction.Data.ProgrammableTransaction, gotData.Transaction.Data.ProgrammableTransaction
	require.NotNil(t, wantPTB, "fixture transaction should be a ProgrammableTransaction")
	require.NotNil(t, gotPTB)
	require.Len(t, gotPTB.Inputs, len(wantPTB.Inputs), "PTB input count")
	require.Len(t, gotPTB.Commands, len(wantPTB.Commands), "PTB command count")
}

// objectChangeKeys renders object changes as canonical sorted JSON strings so
// the two backends can be compared order-insensitively (v1 groups mutations
// before creations, v2 keeps the effects' object-id order).
func objectChangeKeys(t *testing.T, changes []lib.TagJson[types.ObjectChange]) []string {
	t.Helper()
	keys := make([]string, 0, len(changes))
	for _, change := range changes {
		encoded, err := json.Marshal(change.Data)
		require.NoError(t, err)
		keys = append(keys, string(encoded))
	}
	sort.Strings(keys)
	return keys
}

// TestParityObjectChanges compares ShowObjectChanges across every transaction
// of the fixture checkpoint, covering user and system transactions alike.
func TestParityObjectChanges(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	options := types.SuiTransactionBlockResponseOptions{ShowObjectChanges: true}
	fromV1, err := env.v1.MultiGetTransactionBlocks(ctx, env.txDigests, options)
	require.NoError(t, err)
	fromV2, err := env.v2.MultiGetTransactionBlocks(ctx, env.txDigests, options)
	require.NoError(t, err)
	require.Len(t, fromV1, len(env.txDigests))
	require.Len(t, fromV2, len(env.txDigests))

	sawChanges := false
	for i, digest := range env.txDigests {
		require.Empty(t, fromV2[i].Errors, "%s: v2 reported conversion errors", digest)
		require.Nil(t, fromV2[i].Effects, "%s: effects fetched for the derivation must not be echoed back", digest)
		require.Equal(t,
			objectChangeKeys(t, fromV1[i].ObjectChanges),
			objectChangeKeys(t, fromV2[i].ObjectChanges),
			"%s: objectChanges", digest)
		sawChanges = sawChanges || len(fromV2[i].ObjectChanges) > 0
	}
	require.True(t, sawChanges, "fixture checkpoint should have at least one object change")
}

func TestParityMultiGetTransactionBlocks(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	require.GreaterOrEqual(t, len(env.txDigests), 2, "fixture checkpoint should have at least 2 transactions")
	digests := env.txDigests[:2]

	fromV1, err := env.v1.MultiGetTransactionBlocks(ctx, digests, fullOptions)
	require.NoError(t, err)
	fromV2, err := env.v2.MultiGetTransactionBlocks(ctx, digests, fullOptions)
	require.NoError(t, err)

	require.Len(t, fromV1, len(digests))
	require.Len(t, fromV2, len(digests))
	for i, digest := range digests {
		require.Equal(t, digest.String(), fromV1[i].Digest.String(), "v1 order")
		require.Equal(t, digest.String(), fromV2[i].Digest.String(), "v2 order")
		require.NoError(t, compareTxResponse(fromV1[i], fromV2[i], fullOptions))
	}
}

// MARK - Coins & balances

func TestParityGetBalance(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	addr := env.coinAddr
	if env.accumAddr != nil {
		addr = *env.accumAddr // prefer an address with SIP-58 address-balance funds
	}

	retryVolatile(t, func() error {
		fromV1, err := env.v1.GetBalance(ctx, addr, types.SUI_COIN_TYPE)
		if err != nil {
			return err
		}
		fromV2, err := env.v2.GetBalance(ctx, addr, types.SUI_COIN_TYPE)
		if err != nil {
			return err
		}
		if fromV1.CoinType != fromV2.CoinType {
			return fmt.Errorf("coinType: v1 %s, v2 %s", fromV1.CoinType, fromV2.CoinType)
		}
		if !fromV1.TotalBalance.Equal(fromV2.TotalBalance) {
			return fmt.Errorf("totalBalance: v1 %s, v2 %s", fromV1.TotalBalance, fromV2.TotalBalance)
		}
		if !fromV1.FundsInAddressBalance.Equal(fromV2.FundsInAddressBalance) {
			return fmt.Errorf("fundsInAddressBalance: v1 %s, v2 %s",
				fromV1.FundsInAddressBalance, fromV2.FundsInAddressBalance)
		}
		return nil
	})
}

func TestParityGetAllBalances(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)

	type balancePair struct{ total, funds string }
	snapshot := func(balances []types.Balance) map[string]balancePair {
		out := make(map[string]balancePair, len(balances))
		for _, balance := range balances {
			out[balance.CoinType] = balancePair{
				total: balance.TotalBalance.String(),
				funds: balance.FundsInAddressBalance.String(),
			}
		}
		return out
	}

	retryVolatile(t, func() error {
		fromV1, err := env.v1.GetAllBalances(ctx, env.coinAddr)
		if err != nil {
			return err
		}
		fromV2, err := env.v2.GetAllBalances(ctx, env.coinAddr)
		if err != nil {
			return err
		}
		mapV1, mapV2 := snapshot(fromV1), snapshot(fromV2)
		if len(mapV1) != len(mapV2) {
			return fmt.Errorf("coin type counts: v1 %v, v2 %v", mapV1, mapV2)
		}
		for coinType, pairV1 := range mapV1 {
			pairV2, ok := mapV2[coinType]
			if !ok {
				return fmt.Errorf("coin type %s missing from v2: v1 %v, v2 %v", coinType, mapV1, mapV2)
			}
			if pairV1 != pairV2 {
				return fmt.Errorf("balance for %s: v1 %+v, v2 %+v", coinType, pairV1, pairV2)
			}
		}
		return nil
	})
}

// allCoins drains every GetCoins page with the given page size, asserting
// along the way that each backend's own opaque cursor yields pages disjoint
// from the ones before it.
func allCoins(ctx context.Context, backend SuiClient, owner sui_types.SuiAddress, pageSize uint) (map[string]uint64, error) {
	seen := make(map[string]uint64)
	var cursor *string
	for page := 0; ; page++ {
		if page > 50 {
			return nil, fmt.Errorf("more than %d pages, aborting", page)
		}
		resp, err := backend.GetCoins(ctx, owner, nil, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		for _, coin := range resp.Data {
			id := coin.CoinObjectId.String()
			if _, dup := seen[id]; dup {
				return nil, fmt.Errorf("coin %s repeated across pages (cursor not advancing)", id)
			}
			seen[id] = coin.Balance.Uint64()
		}
		if !resp.HasNextPage || resp.NextCursor == nil {
			return seen, nil
		}
		cursor = resp.NextCursor
	}
}

func TestParityGetCoins(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)

	retryVolatile(t, func() error {
		fromV1, err := allCoins(ctx, env.v1, env.coinAddr, 5)
		if err != nil {
			return fmt.Errorf("v1: %w", err)
		}
		fromV2, err := allCoins(ctx, env.v2, env.coinAddr, 5)
		if err != nil {
			return fmt.Errorf("v2: %w", err)
		}
		if len(fromV1) == 0 {
			return fmt.Errorf("fixture address %s has no coins", env.coinAddr)
		}
		if fmt.Sprint(fromV1) != fmt.Sprint(fromV2) {
			return fmt.Errorf("coin sets differ: v1 %v, v2 %v", fromV1, fromV2)
		}

		// With more than one coin, a page size of 1 forces a multi-page walk,
		// exercising each backend's own opaque cursor. Page disjointness is
		// asserted inside allCoins via its duplicate check.
		if len(fromV1) < 2 {
			t.Logf("fixture address %s has %d coin(s); multi-page cursor walk not exercised", env.coinAddr, len(fromV1))
			return nil
		}
		pagedV1, err := allCoins(ctx, env.v1, env.coinAddr, 1)
		if err != nil {
			return fmt.Errorf("v1 paged: %w", err)
		}
		pagedV2, err := allCoins(ctx, env.v2, env.coinAddr, 1)
		if err != nil {
			return fmt.Errorf("v2 paged: %w", err)
		}
		if fmt.Sprint(pagedV1) != fmt.Sprint(pagedV2) {
			return fmt.Errorf("paged coin sets differ: v1 %v, v2 %v", pagedV1, pagedV2)
		}
		return nil
	})
}

func TestParityGetCoinMetadata(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	fromV1, err := env.v1.GetCoinMetadata(ctx, types.SUI_COIN_TYPE)
	require.NoError(t, err)
	fromV2, err := env.v2.GetCoinMetadata(ctx, types.SUI_COIN_TYPE)
	require.NoError(t, err)

	require.Equal(t, fromV1.Decimals, fromV2.Decimals)
	require.Equal(t, fromV1.Symbol, fromV2.Symbol)
	require.Equal(t, fromV1.Name, fromV2.Name)
	require.EqualValues(t, 9, fromV2.Decimals)
	require.Equal(t, "SUI", fromV2.Symbol)
}

func TestParityGetReferenceGasPrice(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	// Retried because the price can legitimately move between the two reads at
	// an epoch boundary.
	retryVolatile(t, func() error {
		fromV1, err := env.v1.GetReferenceGasPrice(ctx)
		if err != nil {
			return err
		}
		fromV2, err := env.v2.GetReferenceGasPrice(ctx)
		if err != nil {
			return err
		}
		if fromV1.Uint64() != fromV2.Uint64() {
			return fmt.Errorf("reference gas price: v1 %d, v2 %d", fromV1.Uint64(), fromV2.Uint64())
		}
		if fromV2.Uint64() == 0 {
			return fmt.Errorf("reference gas price is zero")
		}
		return nil
	})
}

// MARK - Objects

// objectParityOptions asks for every object field both backends serve for
// regular Move objects. ShowBcs and ShowDisplay are left out: package
// disassembly and display are v1-only shapes (see the object conversions in
// clientv2/internal/pb/sui/rpc/v2/object_convert.go).
var objectParityOptions = &types.SuiObjectDataOptions{
	ShowType:                true,
	ShowOwner:               true,
	ShowPreviousTransaction: true,
	ShowContent:             true,
	ShowStorageRebate:       true,
}

// fixtureCoinObjectID resolves a coin object owned by the fixture coin
// address. Re-resolved per attempt: the live account can spend between reads.
func (env *parityEnv) fixtureCoinObjectID(ctx context.Context) (sui_types.ObjectID, error) {
	coins, err := env.v1.GetCoins(ctx, env.coinAddr, nil, nil, 1)
	if err != nil {
		return sui_types.ObjectID{}, err
	}
	if len(coins.Data) == 0 {
		return sui_types.ObjectID{}, fmt.Errorf("fixture address %s has no coins", env.coinAddr)
	}
	return coins.Data[0].CoinObjectId, nil
}

// ownerKey renders an object owner into a canonical comparable string.
func ownerKey(owner *types.ObjectOwner) string {
	if owner == nil {
		return "<nil>"
	}
	switch {
	case owner.ObjectOwnerInternal != nil && owner.AddressOwner != nil:
		return "addr:" + owner.AddressOwner.String()
	case owner.ObjectOwnerInternal != nil && owner.ObjectOwner != nil:
		return "obj:" + owner.ObjectOwner.String()
	case owner.ObjectOwnerInternal != nil && owner.Shared != nil:
		return "shared"
	default:
		return fmt.Sprintf("%v", *owner)
	}
}

// normalizeMoveFieldValue canonicalizes the two backends' Move field
// renderings so they can be compared: v1 (JSON-RPC) wraps a UID as
// {"id": "0x…"} where v2's server-side json rendering may inline it, and
// integers can arrive as JSON numbers on one side and decimal strings on the
// other.
func normalizeMoveFieldValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 1 {
			if inner, ok := v["id"]; ok {
				return normalizeMoveFieldValue(inner)
			}
		}
		out := make(map[string]any, len(v))
		for key, elem := range v {
			out[key] = normalizeMoveFieldValue(elem)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, elem := range v {
			out[i] = normalizeMoveFieldValue(elem)
		}
		return out
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return v
	default:
		return value
	}
}

func compareObjectResponse(want, got *types.SuiObjectResponse) error {
	if want.Error != nil || got.Error != nil {
		return fmt.Errorf("unexpected error entries: v1 %+v, v2 %+v", want.Error, got.Error)
	}
	if want.Data == nil || got.Data == nil {
		return fmt.Errorf("data presence: v1 %+v, v2 %+v", want.Data, got.Data)
	}
	wantData, gotData := want.Data, got.Data
	if wantData.ObjectId != gotData.ObjectId {
		return fmt.Errorf("objectId: v1 %s, v2 %s", wantData.ObjectId, gotData.ObjectId)
	}
	if wantData.Version.Uint64() != gotData.Version.Uint64() {
		return fmt.Errorf("%s: version: v1 %d, v2 %d", wantData.ObjectId, wantData.Version.Uint64(), gotData.Version.Uint64())
	}
	if wantData.Digest.String() != gotData.Digest.String() {
		return fmt.Errorf("%s: digest: v1 %s, v2 %s", wantData.ObjectId, wantData.Digest, gotData.Digest)
	}
	if wantData.Type == nil || gotData.Type == nil || *wantData.Type != *gotData.Type {
		return fmt.Errorf("%s: type: v1 %v, v2 %v", wantData.ObjectId, wantData.Type, gotData.Type)
	}
	if ownerKey(wantData.Owner) != ownerKey(gotData.Owner) {
		return fmt.Errorf("%s: owner: v1 %s, v2 %s", wantData.ObjectId, ownerKey(wantData.Owner), ownerKey(gotData.Owner))
	}
	if wantData.PreviousTransaction == nil || gotData.PreviousTransaction == nil {
		return fmt.Errorf("%s: previousTransaction presence: v1 %v, v2 %v",
			wantData.ObjectId, wantData.PreviousTransaction, gotData.PreviousTransaction)
	}
	if wantData.PreviousTransaction.String() != gotData.PreviousTransaction.String() {
		return fmt.Errorf("%s: previousTransaction: v1 %s, v2 %s",
			wantData.ObjectId, wantData.PreviousTransaction, gotData.PreviousTransaction)
	}
	if wantData.StorageRebate == nil || gotData.StorageRebate == nil {
		return fmt.Errorf("%s: storageRebate presence: v1 %v, v2 %v",
			wantData.ObjectId, wantData.StorageRebate, gotData.StorageRebate)
	}
	if wantData.StorageRebate.Uint64() != gotData.StorageRebate.Uint64() {
		return fmt.Errorf("%s: storageRebate: v1 %d, v2 %d",
			wantData.ObjectId, wantData.StorageRebate.Uint64(), gotData.StorageRebate.Uint64())
	}
	return compareObjectContent(wantData, gotData)
}

func compareObjectContent(want, got *types.SuiObjectData) error {
	if want.Content == nil || got.Content == nil {
		return fmt.Errorf("%s: content presence: v1 %v, v2 %v", want.ObjectId, want.Content != nil, got.Content != nil)
	}
	wantMove, gotMove := want.Content.Data.MoveObject, got.Content.Data.MoveObject
	if (wantMove == nil) != (gotMove == nil) {
		return fmt.Errorf("%s: content dataType: v1 moveObject=%v, v2 moveObject=%v",
			want.ObjectId, wantMove != nil, gotMove != nil)
	}
	if wantMove == nil {
		// Both packages: module disassembly is a v1-only shape (documented
		// gap), nothing more to compare.
		return nil
	}
	if wantMove.Type != gotMove.Type {
		return fmt.Errorf("%s: content type: v1 %s, v2 %s", want.ObjectId, wantMove.Type, gotMove.Type)
	}
	if wantMove.HasPublicTransfer != gotMove.HasPublicTransfer {
		return fmt.Errorf("%s: hasPublicTransfer: v1 %v, v2 %v",
			want.ObjectId, wantMove.HasPublicTransfer, gotMove.HasPublicTransfer)
	}
	wantFields := fmt.Sprint(normalizeMoveFieldValue(wantMove.Fields))
	gotFields := fmt.Sprint(normalizeMoveFieldValue(gotMove.Fields))
	if wantFields != gotFields {
		return fmt.Errorf("%s: content fields: v1 %s, v2 %s", want.ObjectId, wantFields, gotFields)
	}
	return nil
}

func TestParityGetObject(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	// The fixture coin is live (its owner can spend it between the paired
	// reads, bumping version/digest), so mismatches are retried with a freshly
	// resolved coin.
	retryVolatile(t, func() error {
		objID, err := env.fixtureCoinObjectID(ctx)
		if err != nil {
			return err
		}
		fromV1, err := env.v1.GetObject(ctx, objID, objectParityOptions)
		if err != nil {
			return err
		}
		fromV2, err := env.v2.GetObject(ctx, objID, objectParityOptions)
		if err != nil {
			return err
		}
		return compareObjectResponse(fromV1, fromV2)
	})
}

// requireNotExistsEntry asserts the response is the notExists error entry
// shape both backends use for a missing object in a multi-get.
func requireNotExistsEntry(backend string, resp *types.SuiObjectResponse, id sui_types.ObjectID) error {
	if resp.Data != nil {
		return fmt.Errorf("%s: missing object %s has data: %+v", backend, id, resp.Data)
	}
	if resp.Error == nil {
		return fmt.Errorf("%s: missing object %s has neither data nor error", backend, id)
	}
	notExists := resp.Error.Data.NotExists
	if notExists == nil {
		return fmt.Errorf("%s: missing object %s error is not notExists: %+v", backend, id, resp.Error.Data)
	}
	if notExists.ObjectId != id {
		return fmt.Errorf("%s: notExists object_id: got %s, want %s", backend, notExists.ObjectId, id)
	}
	return nil
}

func TestParityMultiGetObjects(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)

	// A random 32-byte id is astronomically unlikely to exist on chain, giving
	// both backends a notExists entry to render.
	var missingID sui_types.ObjectID
	_, err := rand.Read(missingID[:])
	require.NoError(t, err)

	retryVolatile(t, func() error {
		objID, err := env.fixtureCoinObjectID(ctx)
		if err != nil {
			return err
		}
		ids := []sui_types.ObjectID{objID, missingID}
		fromV1, err := env.v1.MultiGetObjects(ctx, ids, objectParityOptions)
		if err != nil {
			return err
		}
		fromV2, err := env.v2.MultiGetObjects(ctx, ids, objectParityOptions)
		if err != nil {
			return err
		}
		if len(fromV1) != len(ids) || len(fromV2) != len(ids) {
			return fmt.Errorf("result counts: v1 %d, v2 %d, want %d", len(fromV1), len(fromV2), len(ids))
		}
		if err := compareObjectResponse(&fromV1[0], &fromV2[0]); err != nil {
			return fmt.Errorf("existing object: %w", err)
		}
		if err := requireNotExistsEntry("v1", &fromV1[1], missingID); err != nil {
			return err
		}
		return requireNotExistsEntry("v2", &fromV2[1], missingID)
	})
}

// MARK - Simulation

// trivialPTB builds SplitCoins(gas, [1000]) + TransferObjects([split], sender):
// the simplest transaction that touches gas, produces effects and balance
// changes, and needs no owned-object fixtures.
func trivialPTB(t *testing.T, sender sui_types.SuiAddress) sui_types.ProgrammableTransaction {
	t.Helper()
	ptb := sui_types.NewProgrammableTransactionBuilder()
	amount, err := ptb.Pure(uint64(1000))
	require.NoError(t, err)
	split := ptb.Command(sui_types.Command{
		SplitCoins: &sui_types.SplitCoinsCommand{
			Argument:  sui_types.Argument{GasCoin: &lib.EmptyEnum{}},
			Arguments: []sui_types.Argument{amount},
		},
	})
	recipient, err := ptb.Pure(sender)
	require.NoError(t, err)
	ptb.Command(sui_types.Command{
		TransferObjects: &sui_types.TransferObjectsCommand{
			Arguments: []sui_types.Argument{split},
			Argument:  recipient,
		},
	})
	return ptb.Finish()
}

func TestParityDevInspectTransactionBlock(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	sender := env.coinAddr

	pt := trivialPTB(t, sender)
	kindBytes, err := bcs.Marshal(sui_types.TransactionKind{ProgrammableTransaction: &pt})
	require.NoError(t, err)

	fromV1, err := env.v1.DevInspectTransactionBlock(ctx, sender, kindBytes, nil, nil)
	require.NoError(t, err)
	fromV2, err := env.v2.DevInspectTransactionBlock(ctx, sender, kindBytes, nil, nil)
	require.NoError(t, err)

	// Costs may differ slightly between the two execution paths; parity here
	// is "both succeed and report a real computation cost".
	for backend, result := range map[string]*types.DevInspectResults{"v1": fromV1, "v2": fromV2} {
		require.NotNil(t, result.Effects.Data.V1, "%s effects", backend)
		require.Equal(t, types.ExecutionStatusSuccess, result.Effects.Data.V1.Status.Status,
			"%s status (error: %s)", backend, result.Effects.Data.V1.Status.Error)
		require.Positive(t, result.Effects.Data.V1.GasUsed.ComputationCost.Uint64(), "%s computationCost", backend)
	}
}

func TestParityDryRunTransaction(t *testing.T) {
	env, ctx := parity(t), liveCtx(t)
	sender := env.coinAddr

	price, err := env.v2.GetReferenceGasPrice(ctx)
	require.NoError(t, err)

	retryVolatile(t, func() error {
		// Re-resolve the gas coin each attempt: the fixture address is a live
		// account and can spend/receive between reads.
		const gasBudget = uint64(10_000_000)
		coins, err := env.v1.GetCoins(ctx, sender, nil, nil, 50)
		if err != nil {
			return err
		}
		var gasCoin *types.Coin
		for i := range coins.Data {
			if coins.Data[i].Balance.Uint64() >= 2*gasBudget {
				gasCoin = &coins.Data[i]
				break
			}
		}
		if gasCoin == nil {
			t.Skipf("no coin with balance >= %d on fixture address %s", 2*gasBudget, sender)
		}

		pt := trivialPTB(t, sender)
		txData := sui_types.NewProgrammable(sender, []*sui_types.ObjectRef{gasCoin.Reference()}, pt, gasBudget, price.Uint64())
		txBytes, err := bcs.Marshal(txData)
		if err != nil {
			return err
		}

		fromV1, err := env.v1.DryRunTransaction(ctx, txBytes)
		if err != nil {
			return err
		}
		fromV2, err := env.v2.DryRunTransaction(ctx, txBytes)
		if err != nil {
			return err
		}
		if fromV1.Effects.Data.V1 == nil || fromV2.Effects.Data.V1 == nil {
			return fmt.Errorf("effects presence: v1 %+v, v2 %+v", fromV1.Effects, fromV2.Effects)
		}
		if fromV1.Effects.Data.V1.Status != fromV2.Effects.Data.V1.Status {
			return fmt.Errorf("status: v1 %+v, v2 %+v", fromV1.Effects.Data.V1.Status, fromV2.Effects.Data.V1.Status)
		}
		wantKeys := balanceChangeKeys(fromV1.BalanceChanges)
		gotKeys := balanceChangeKeys(fromV2.BalanceChanges)
		if fmt.Sprint(wantKeys) != fmt.Sprint(gotKeys) {
			return fmt.Errorf("balanceChanges: v1 %v, v2 %v", wantKeys, gotKeys)
		}
		return nil
	})
}
