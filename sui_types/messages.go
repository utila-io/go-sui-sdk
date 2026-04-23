package sui_types

import (
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/move_types"
	"github.com/utila-io/go-sui-sdk/sui_protocol"
)

type TransactionData struct {
	V1 *TransactionDataV1
}

func (t TransactionData) IsBcsEnum() {

}

type TransactionDataV1 struct {
	Kind       TransactionKind
	Sender     SuiAddress
	GasData    GasData
	Expiration TransactionExpiration
}

type TransactionExpiration struct {
	None        *lib.EmptyEnum
	Epoch       *EpochId
	ValidDuring *ValidDuringExpiration
}

func (t TransactionExpiration) IsBcsEnum() {

}

// ValidDuringExpiration corresponds to TransactionExpiration::ValidDuring (SIP-58).
// Chain is a length-prefixed byte vector (33 bytes in BCS: 0x20 prefix + 32 bytes)
// matching Sui's ChainIdentifier(CheckpointDigest(Digest)) encoding.
type ValidDuringExpiration struct {
	MinEpoch     *uint64 `bcs:"optional"`
	MaxEpoch     *uint64 `bcs:"optional"`
	MinTimestamp *uint64 `bcs:"optional"`
	MaxTimestamp *uint64 `bcs:"optional"`
	Chain        []byte
	Nonce        uint32
}

type GasData struct {
	Payment []*ObjectRef
	Owner   SuiAddress
	Price   uint64
	Budget  uint64
}

type TransactionKind struct {
	ProgrammableTransaction *ProgrammableTransaction
	ChangeEpoch             *ChangeEpoch
	Genesis                 *GenesisTransaction
	ConsensusCommitPrologue *ConsensusCommitPrologue
}

func (t TransactionKind) IsBcsEnum() {
}

type ConsensusCommitPrologue struct {
	Epoch             uint64
	Round             uint64
	CommitTimestampMs CheckpointTimestamp
}

type ProgrammableTransaction struct {
	Inputs   []CallArg
	Commands []Command
}

type Command struct {
	MoveCall        *ProgrammableMoveCall
	TransferObjects *struct {
		Arguments []Argument
		Argument  Argument
	}
	SplitCoins *struct {
		Argument  Argument
		Arguments []Argument
	}
	MergeCoins *struct {
		Argument  Argument
		Arguments []Argument
	}
	Publish *struct {
		Bytes   [][]uint8
		Objects []ObjectID
	}
	MakeMoveVec *struct {
		TypeTag   *move_types.TypeTag `bcs:"optional"`
		Arguments []Argument
	}
	Upgrade *struct {
		Bytes    [][]uint8
		Objects  []ObjectID
		ObjectID ObjectID
		Argument Argument
	}
}

func (c Command) IsBcsEnum() {

}

type Argument struct {
	GasCoin      *lib.EmptyEnum
	Input        *uint16
	Result       *uint16
	NestedResult *struct {
		Result1 uint16
		Result2 uint16
	}
}

func (a Argument) IsBcsEnum() {

}

type ProgrammableMoveCall struct {
	Package       ObjectID
	Module        move_types.Identifier
	Function      move_types.Identifier
	TypeArguments []move_types.TypeTag
	Arguments     []Argument
}

type SingleTransactionKind struct {
	TransferObject *TransferObject
	Publish        *MoveModulePublish
	Call           *MoveCall
	TransferSui    *TransferSui
	Pay            *Pay
	PaySui         *PaySui
	PayAllSui      *PayAllSui
	ChangeEpoch    *ChangeEpoch
	Genesis        *GenesisTransaction
}

func (s SingleTransactionKind) IsBcsEnum() {
}

type TransferObject struct {
	Recipient SuiAddress
	ObjectRef ObjectRef
}

type MoveModulePublish struct {
	Modules [][]byte
}

type MoveCall struct {
	Package       ObjectID
	Module        string
	Function      string
	TypeArguments []*move_types.TypeTag
	Arguments     []*CallArg
}

type TransferSui struct {
	Recipient SuiAddress
	Amount    *uint64 `bcs:"optional"`
}

type Pay struct {
	Coins      []*ObjectRef
	Recipients []*SuiAddress
	Amounts    []*uint64
}

type PaySui = Pay

type PayAllSui struct {
	Coins     []*ObjectRef
	Recipient SuiAddress
}

type ChangeEpoch struct {
	Epoch                   EpochId
	ProtocolVersion         sui_protocol.ProtocolVersion
	StorageCharge           uint64
	ComputationCharge       uint64
	StorageRebate           uint64
	NonRefundableStorageFee uint64
	EpochStartTimestampMs   uint64
	SystemPackages          []*struct {
		SequenceNumber SequenceNumber
		Bytes          [][]uint8
		Objects        []*ObjectID
	}
}

type GenesisTransaction struct {
	Objects []GenesisObject
}

type GenesisObject struct {
	RawObject *struct {
		Data  Data
		Owner Owner
	}
}

type CallArg struct {
	Pure            *[]byte
	Object          *ObjectArg
	FundsWithdrawal *FundsWithdrawalArg
}

func (c CallArg) IsBcsEnum() {
}

type FundsWithdrawalArg struct {
	Reservation  Reservation
	TypeArg      WithdrawalTypeArg
	WithdrawFrom WithdrawFrom
}

type Reservation struct {
	MaxAmountU64 *uint64
}

func (Reservation) IsBcsEnum() {

}

type WithdrawalTypeArg struct {
	Balance *move_types.TypeTag
}

func (WithdrawalTypeArg) IsBcsEnum() {

}

type WithdrawFrom struct {
	Sender *lib.EmptyEnum
}

func (WithdrawFrom) IsBcsEnum() {

}

type ObjectArg struct {
	ImmOrOwnedObject *ObjectRef
	SharedObject     *struct {
		Id                   ObjectID
		InitialSharedVersion SequenceNumber
		Mutable              bool
	}
}

func (o ObjectArg) IsBcsEnum() {
}

func (o ObjectArg) id() ObjectID {
	switch {
	case o.ImmOrOwnedObject != nil:
		return o.ImmOrOwnedObject.ObjectId
	case o.SharedObject != nil:
		return o.SharedObject.Id
	default:
		return ObjectID{}
	}
}
