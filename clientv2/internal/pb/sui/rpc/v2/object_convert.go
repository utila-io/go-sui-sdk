package rpcv2

import (
	"errors"
	"fmt"
	"strings"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// packageObjectType is the object_type gRPC reports for Move packages.
const packageObjectType = "package"

// ToInternalType converts an Object into the internal object data shape,
// populating only the fields requested by options. A nil receiver is an
// error rather than a zero object: an absent object_id parses as the zero
// address, so without this guard a response carrying no object would become a
// plausible-looking object 0x0.
func (x *Object) ToInternalType(options *types.SuiObjectDataOptions) (*types.SuiObjectData, error) {
	if x == nil {
		return nil, errors.New("no object in response")
	}
	objectID, err := parseAddress(x.GetObjectId())
	if err != nil {
		return nil, err
	}
	data := &types.SuiObjectData{
		ObjectId: objectID,
		Version:  types.NewSafeSuiBigInt(x.GetVersion()),
		Digest:   parseDigest(x.GetDigest()),
	}
	if options == nil {
		return data, nil
	}
	if options.ShowType {
		objectType := normalizeTypeString(x.GetObjectType())
		data.Type = &objectType
	}
	if options.ShowContent {
		data.Content = x.toInternalParsedData()
	}
	if options.ShowBcs {
		data.Bcs = x.toInternalRawData()
	}
	if options.ShowOwner && x.GetOwner() != nil {
		owner, err := x.GetOwner().ToInternalObjectOwner()
		if err != nil {
			return nil, err
		}
		data.Owner = &owner
	}
	if options.ShowPreviousTransaction && x.GetPreviousTransaction() != "" {
		previousTx := parseDigest(x.GetPreviousTransaction())
		data.PreviousTransaction = &previousTx
	}
	if options.ShowStorageRebate {
		storageRebate := types.NewSafeSuiBigInt(x.GetStorageRebate())
		data.StorageRebate = &storageRebate
	}
	if options.ShowDisplay && x.GetDisplay() != nil {
		data.Display = x.GetDisplay().GetOutput().AsInterface()
	}
	return data, nil
}

// ToInternalCoin converts a Coin<T> object into the internal coin entry shape.
// LockedUntilEpoch has no gRPC source and is left nil.
func (x *Object) ToInternalCoin() (types.Coin, error) {
	coinObjectID, err := parseAddress(x.GetObjectId())
	if err != nil {
		return types.Coin{}, err
	}
	coinType, err := coinTypeFromObjectType(x.GetObjectType())
	if err != nil {
		return types.Coin{}, err
	}
	return types.Coin{
		CoinType:            coinType,
		CoinObjectId:        coinObjectID,
		Version:             types.NewSafeSuiBigInt(x.GetVersion()),
		Digest:              parseDigest(x.GetDigest()),
		Balance:             types.NewSafeSuiBigInt(x.GetBalance()),
		PreviousTransaction: parseDigest(x.GetPreviousTransaction()),
	}, nil
}

// toInternalParsedData builds the showContent payload from the server-rendered
// json field. Package disassembly is not available over gRPC, so packages
// carry an empty Disassembled map.
func (x *Object) toInternalParsedData() *lib.TagJson[types.SuiParsedData] {
	if x.GetObjectType() == packageObjectType {
		return &lib.TagJson[types.SuiParsedData]{
			Data: types.SuiParsedData{Package: &types.SuiMovePackage{}},
		}
	}
	return &lib.TagJson[types.SuiParsedData]{
		Data: types.SuiParsedData{
			MoveObject: &types.SuiParsedMoveObject{
				Type:              normalizeTypeString(x.GetObjectType()),
				HasPublicTransfer: x.GetHasPublicTransfer(),
				Fields:            x.GetJson().AsInterface(),
			},
		},
	}
}

// toInternalRawData builds the showBcs payload from the raw Move struct
// contents. Raw package data is not implemented: types.SuiRawMovePackage and
// the proto's Package.Modules both exist, so packages could be mapped, but
// ObjectReadMaskPaths does not request the package fields.
func (x *Object) toInternalRawData() *lib.TagJson[types.SuiRawData] {
	if x.GetObjectType() == packageObjectType {
		return nil
	}
	return &lib.TagJson[types.SuiRawData]{
		Data: types.SuiRawData{
			MoveObject: &types.SuiRawMoveObject{
				Type:              normalizeTypeString(x.GetObjectType()),
				HasPublicTransfer: x.GetHasPublicTransfer(),
				Version:           x.GetVersion(),
				BcsBytes:          lib.Base64Data(x.GetContents().GetValue()),
			},
		},
	}
}

// coinTypeFromObjectType extracts the normalized inner type T from a
// "0x2::coin::Coin<T>" object type string.
func coinTypeFromObjectType(objectType string) (string, error) {
	open := strings.Index(objectType, "<")
	close_ := strings.LastIndex(objectType, ">")
	if open < 0 || close_ < open {
		return "", fmt.Errorf("unexpected coin object type format: %s", objectType)
	}
	return normalizeTypeString(objectType[open+1 : close_]), nil
}

// ObjectNotFound builds the response for an object that does not exist, so a
// per-object NOT_FOUND gRPC error keeps the internal notExists semantics.
func ObjectNotFound(objectID sui_types.ObjectID) types.SuiObjectResponse {
	return types.SuiObjectResponse{
		Error: &lib.TagJson[types.SuiObjectResponseError]{
			Data: types.SuiObjectResponseError{
				NotExists: &struct {
					ObjectId sui_types.ObjectID `json:"object_id"`
				}{ObjectId: objectID},
			},
		},
	}
}
