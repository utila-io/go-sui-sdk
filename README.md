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



## Client

The client speaks the Sui [gRPC API](https://docs.sui.io/concepts/data-access/grpc-overview)
(`sui.rpc.v2`). The legacy JSON-RPC backend has been removed.

```go
import "github.com/utila-io/go-sui-sdk/suiclient"

// endpoint is "grpc://host[:port]" or "host[:port]" (port defaults to 443),
// always TLS — http:// and https:// are rejected
cli, err := suiclient.New("fullnode.mainnet.sui.io:443")
defer cli.Close()

// Plaintext (local node, internal bridge) is an explicit opt-in
cli, err := suiclient.New("localhost:9000", suiclient.WithInsecure())

// Provider auth headers, sent as gRPC metadata (repeatable)
cli, err := suiclient.New(endpoint, suiclient.WithHeader("x-token", token))

bal, err := cli.GetBalance(ctx, owner, "")
```

The `suiclient.SuiClient` interface covers balances & coins (including SIP-58
address balances and `accumulatorEvents` on effects), objects, transaction
reads/execution/simulation, and checkpoints. Build transactions locally with
`sui_types.ProgrammableTransactionBuilder` and submit them with
`ExecuteTransactionBlock`.

The gRPC bindings are generated from the
[MystenLabs/sui-apis](https://github.com/MystenLabs/sui-apis) protos, pinned
via the `third_party/sui-apis` git submodule. The generated code is committed,
so consumers installing via `go get` need nothing extra; the submodule is only
needed to regenerate (`git submodule update --init`, then `make proto`, or
`make proto-update` after bumping `SUI_APIS_REF` to move the pin). Note that
grpc-go raises the module's minimum Go version to 1.26.

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
