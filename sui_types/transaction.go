package sui_types

import "github.com/coming-chat/go-sui/v2/lib"

var (
	SuiSystemMut = CallArg{
		Object: &SuiSystemMutObj,
	}

	SuiSystemMutObj = ObjectArg{
		SharedObject: &struct {
			Id                   ObjectID
			InitialSharedVersion SequenceNumber
			Mutable              bool
		}{Id: *SuiSystemStateObjectId, InitialSharedVersion: SuiSystemStateObjectSharedVersion, Mutable: true},
	}
)

func NewProgrammableAllowSponsor(
	sender SuiAddress,
	gasPayment []*ObjectRef,
	pt ProgrammableTransaction,
	gasBudge,
	gasPrice uint64,
	sponsor SuiAddress,
) TransactionData {
	kind := TransactionKind{
		ProgrammableTransaction: &pt,
	}
	return newWithGasCoinsAllowSponsor(kind, sender, gasPayment, gasBudge, gasPrice, sponsor)
}

func NewProgrammable(
	sender SuiAddress,
	gasPayment []*ObjectRef,
	pt ProgrammableTransaction,
	gasBudget uint64,
	gasPrice uint64,
) TransactionData {
	return NewProgrammableAllowSponsor(sender, gasPayment, pt, gasBudget, gasPrice, sender)
}

// NewProgrammableWithExpiration creates a ProgrammableTransaction with a custom
// TransactionExpiration. Use this for SIP-58 transactions that require ValidDuring.
func NewProgrammableWithExpiration(
	sender SuiAddress,
	gasPayment []*ObjectRef,
	pt ProgrammableTransaction,
	gasBudget uint64,
	gasPrice uint64,
	expiration TransactionExpiration,
) TransactionData {
	return TransactionData{
		V1: &TransactionDataV1{
			Kind: TransactionKind{
				ProgrammableTransaction: &pt,
			},
			Sender: sender,
			GasData: GasData{
				Price:   gasPrice,
				Owner:   sender,
				Payment: gasPayment,
				Budget:  gasBudget,
			},
			Expiration: expiration,
		},
	}
}

func newWithGasCoinsAllowSponsor(
	kind TransactionKind,
	sender SuiAddress,
	gasPayment []*ObjectRef,
	gasBudget uint64,
	gasPrice uint64,
	gasSponsor SuiAddress,
) TransactionData {
	return TransactionData{
		V1: &TransactionDataV1{
			Kind:   kind,
			Sender: sender,
			GasData: GasData{
				Price:   gasPrice,
				Owner:   gasSponsor,
				Payment: gasPayment,
				Budget:  gasBudget,
			},
			Expiration: TransactionExpiration{
				None: &lib.EmptyEnum{},
			},
		},
	}
}
