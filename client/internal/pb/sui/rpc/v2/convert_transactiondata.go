package rpcv2

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/fardream/go-bcs/bcs"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// The wire carries the transaction as opaque BCS, so the parsed input shape is
// built from a decoded sui_types.TransactionData rather than from a protobuf
// message. Leaf rendering lives in convert_ptbjson.go.

// decodeTransactionData BCS-decodes bare TransactionData bytes. System
// transaction kinds are not in sui_types' enum and fail to decode.
func decodeTransactionData(txData []byte) (*sui_types.TransactionData, error) {
	var data sui_types.TransactionData
	if _, err := bcs.Unmarshal(txData, &data); err != nil {
		return nil, fmt.Errorf("decode TransactionData: %w", err)
	}
	if data.V1 == nil {
		return nil, errors.New("decode TransactionData: unknown version")
	}
	return &data, nil
}

// toInternalTransactionBlock renders decoded TransactionData into the parsed
// showInput shape. Pure input value types are only inferred from built-in
// command usage (see pureValueTypes); unresolved pures render as raw bytes
// with a null valueType.
func toInternalTransactionBlock(data *sui_types.TransactionData, signatures []*UserSignature) *types.SuiTransactionBlock {
	gasData := types.SuiGasData{
		Payment: make([]types.SuiObjectRef, 0, len(data.V1.GasData.Payment)),
		Owner:   data.V1.GasData.Owner.String(),
		Price:   types.NewSafeSuiBigInt(data.V1.GasData.Price),
		Budget:  types.NewSafeSuiBigInt(data.V1.GasData.Budget),
	}
	for _, ref := range data.V1.GasData.Payment {
		if ref == nil {
			continue
		}
		gasData.Payment = append(gasData.Payment, types.SuiObjectRef{
			ObjectId: ref.ObjectId.String(),
			Version:  ref.Version,
			Digest:   ref.Digest,
		})
	}
	block := &types.SuiTransactionBlock{
		Data: lib.TagJson[types.SuiTransactionBlockData]{Data: types.SuiTransactionBlockData{
			V1: &types.SuiTransactionBlockDataV1{
				Transaction: transactionBlockKind(data.V1.Kind),
				Sender:      data.V1.Sender,
				GasData:     gasData,
			},
		}},
		TxSignatures: make([]string, 0, len(signatures)),
	}
	for _, signature := range signatures {
		block.TxSignatures = append(block.TxSignatures,
			base64.StdEncoding.EncodeToString(signature.GetBcs().GetValue()))
	}
	return block
}

// transactionBlockKind maps a decoded TransactionKind to the internal kind.
// Genesis is not mapped (only ever appears in checkpoint 0), leaving it zero.
func transactionBlockKind(kind sui_types.TransactionKind) types.SuiTransactionBlockKind {
	out := types.TransactionBlockKind{}
	switch {
	case kind.ProgrammableTransaction != nil:
		out.ProgrammableTransaction = programmableTransactionBlock(kind.ProgrammableTransaction)
	case kind.ChangeEpoch != nil:
		out.ChangeEpoch = &types.SuiChangeEpoch{
			Epoch:                 types.NewSafeSuiBigInt(kind.ChangeEpoch.Epoch),
			StorageCharge:         kind.ChangeEpoch.StorageCharge,
			ComputationCharge:     kind.ChangeEpoch.ComputationCharge,
			StorageRebate:         kind.ChangeEpoch.StorageRebate,
			EpochStartTimestampMs: kind.ChangeEpoch.EpochStartTimestampMs,
		}
	case kind.ConsensusCommitPrologue != nil:
		out.ConsensusCommitPrologue = &types.SuiConsensusCommitPrologue{
			Epoch:             kind.ConsensusCommitPrologue.Epoch,
			Round:             kind.ConsensusCommitPrologue.Round,
			CommitTimestampMs: kind.ConsensusCommitPrologue.CommitTimestampMs,
		}
	}
	return types.SuiTransactionBlockKind{Data: out}
}

// programmableTransactionBlock renders a PTB's inputs and commands into the
// generic maps they are serialized as.
func programmableTransactionBlock(pt *sui_types.ProgrammableTransaction) *types.SuiProgrammableTransactionBlock {
	valueTypes := pureValueTypes(pt)
	block := &types.SuiProgrammableTransactionBlock{
		Inputs:   make([]interface{}, 0, len(pt.Inputs)),
		Commands: make([]interface{}, 0, len(pt.Commands)),
	}
	for i, input := range pt.Inputs {
		block.Inputs = append(block.Inputs, callArgJSON(input, valueTypes[i]))
	}
	for _, command := range pt.Commands {
		block.Commands = append(block.Commands, commandJSON(command))
	}
	return block
}

// pureValueTypes infers pure input value types from built-in command usage:
// TransferObjects recipients are addresses, SplitCoins amounts are u64.
// MoveCall types would need on-chain function signatures and are not resolved.
func pureValueTypes(pt *sui_types.ProgrammableTransaction) map[int]string {
	valueTypes := make(map[int]string)
	for _, command := range pt.Commands {
		switch {
		case command.TransferObjects != nil:
			if input := command.TransferObjects.Argument.Input; input != nil {
				valueTypes[int(*input)] = "address"
			}
		case command.SplitCoins != nil:
			for _, amount := range command.SplitCoins.Arguments {
				if amount.Input != nil {
					valueTypes[int(*amount.Input)] = "u64"
				}
			}
		}
	}
	return valueTypes
}
