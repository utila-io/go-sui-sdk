# go-sui-sdk
Sui Golang SDK

[![Documentation (master)](https://img.shields.io/badge/docs-master-59f)](https://github.com/coming-chat/go-sui-sdk)
[![License](https://img.shields.io/badge/license-Apache-green.svg)](https://github.com/coming-chat/go-sui-sdk/blob/main/LICENSE)

The Sui Golang SDK for ComingChat. 
We welcome other developers to participate in the development and testing of sui-sdk.

## Install

```sh
go get github.com/coming-chat/go-sui/v2
```



## Backends: JSON-RPC (v1) and gRPC (v2)

Sui is retiring its public JSON-RPC endpoints in favor of the
[gRPC API](https://docs.sui.io/concepts/data-access/grpc-overview) (`sui.rpc.v2`).
This SDK supports both behind one interface; select the backend with a flag:

```go
import "github.com/utila-io/go-sui-sdk/suiclient"

// Default: JSON-RPC (unchanged behavior)
cli, err := suiclient.New(endpoint)

// Opt into gRPC
cli, err := suiclient.New(endpoint, suiclient.WithBackend(suiclient.BackendGRPC))
defer cli.Close()

bal, err := cli.GetBalance(ctx, owner, "") // same interface either way
```

The backend can also be forced at runtime with `SUI_SDK_BACKEND=v1|v2`
(an explicit `WithBackend` always wins). The `suiclient.SuiClient` interface
contains only methods both backends fully support: balances & coins (including
SIP-58 address balances and `accumulatorEvents` on effects), objects,
transaction reads/execution/simulation, and checkpoints.

Not on the interface (JSON-RPC concrete `client.Client` only): the `unsafe_*`
server-side transaction builders, faucet, staking/APY reads, and arbitrary
`queryTransactionBlocks`/`queryEvents` filters — build transactions locally
with `sui_types.ProgrammableTransactionBuilder` instead, and enumerate
checkpoint transactions with `GetCheckpointTransactions`.

The gRPC bindings are generated from protos vendored at a pinned commit of
[MystenLabs/sui-apis](https://github.com/MystenLabs/sui-apis); see the
`Makefile` (`make proto-update`) to update them.

## Usage

### Account

```go
import "github.com/coming-chat/go-sui/account"

// Import account with mnemonic
acc, err := account.NewAccountWithMnemonic(mnemonic)

// Import account with private key
privateKey, err := hex.DecodeString("4ec5a9eefc0bb86027a6f3ba718793c813505acc25ed09447caf6a069accdd4b")
acc, err := account.NewAccount(privateKey)

// Get private key, public key, address
fmt.Printf("privateKey = %x\n", acc.PrivateKey[:32])
fmt.Printf(" publicKey = %x\n", acc.PublicKey)
fmt.Printf("   address = %v\n", acc.Address)

// Sign data
signedData := acc.Sign(data)
```



### JSON RPC Client

All data interactions on the Sui chain are implemented through the rpc client.

```go
import "github.com/coming-chat/go-sui/client"
import "github.com/coming-chat/go-sui/types"

cli, err := client.Dial(rpcUrl)

// call JSON RPC
responseObject := uint64(0) // if response is a uint64
err := cli.CallContext(ctx, &responseObject, funcName, params...)

// e.g. call get transaction
digest, err := types.NewBase64Data("/KXvTwNRHKKzAB+/Dz1O64LjVbISgIW4VUCmuuPyEfU=")
resp := types.TransactionResponse{}
err := cli.CallContext(ctx, &resp, "sui_getTransaction", digest)
print("transaction status = ", resp.Effects.Status)
print("transaction timestamp = ", resp.TimestampMs)

// And you can call some predefined methods
digest, err := types.NewBase64Data("/KXvTwNRHKKzAB+/Dz1O64LjVbISgIW4VUCmuuPyEfU=")
resp, err := cli.GetTransaction(ctx, digest)
print("transaction status = ", resp.Effects.Status)
print("transaction timestamp = ", resp.TimestampMs)

```

We currently have some rpc methods built-in, [see here](https://github.com/coming-chat/go-sui-sdk/blob/main/client/client_call.go)



### Build Transaction & Sign ( Transfer Sui )

```go
import "github.com/coming-chat/go-sui/client"
import "github.com/coming-chat/go-sui/types"
import "github.com/coming-chat/go-sui/account"

acc, err := account.NewAccountWithMnemonic(mnemonic)
signer, _ := types.NewAddressFromHex(acc.Address)

recipient, err := types.NewAddressFromHex("0x12345678.......")
suiObjectId, err := types.NewHexData("0x36d3176a796e167ffcbd823c94718e7db56b955f")
transferAmount := uint64(10000)
maxGasTransfer := 100

cli, err := client.Dial(rpcUrl)
txnBytes, err := cli.TransferSui(ctx, *signer, *recipient, suiObjectId, transferAmount, maxGasTransfer)

// Sign
signedTxn := txnBytes.SignWith(acc.PrivateKey)

```



### Send Signed Transaction

```go
txnResponse, err := cli.ExecuteTransaction(ctx, signedTxn)

print("transaction digest = ", txnResponse.Certificate.TransactionDigest)
print("transaction status = ", txnResponse.Effects.Status)
print("transaction gasFee = ", txnResponse.Effects.GasFee())
```

