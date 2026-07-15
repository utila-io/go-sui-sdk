package adapt

import (
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// packageObjectType is the object_type gRPC reports for Move packages.
const packageObjectType = "package"

// ObjectReadMaskPaths maps SuiObjectDataOptions to the Object read mask
// paths needed to populate the corresponding SuiObjectData fields.
// object_id, version and digest are always fetched, like JSON-RPC.
func ObjectReadMaskPaths(options *types.SuiObjectDataOptions) []string {
	paths := []string{"object_id", "version", "digest"}
	if options == nil {
		return paths
	}
	if options.ShowType || options.ShowContent || options.ShowBcs {
		paths = append(paths, "object_type")
	}
	if options.ShowContent {
		paths = append(paths, "json", "has_public_transfer")
	}
	if options.ShowBcs {
		paths = append(paths, "contents", "has_public_transfer")
	}
	if options.ShowOwner {
		paths = append(paths, "owner")
	}
	if options.ShowPreviousTransaction {
		paths = append(paths, "previous_transaction")
	}
	if options.ShowStorageRebate {
		paths = append(paths, "storage_rebate")
	}
	if options.ShowDisplay {
		paths = append(paths, "display")
	}
	return paths
}

// ObjectData converts a proto Object into the JSON-RPC sui_getObject data
// shape, populating only the fields requested by options.
func ObjectData(obj *pb.Object, options *types.SuiObjectDataOptions) (*types.SuiObjectData, error) {
	objectID, err := parseAddress(obj.GetObjectId())
	if err != nil {
		return nil, err
	}
	data := &types.SuiObjectData{
		ObjectId: objectID,
		Version:  types.NewSafeSuiBigInt(obj.GetVersion()),
		Digest:   parseDigest(obj.GetDigest()),
	}
	if options == nil {
		return data, nil
	}
	if options.ShowType {
		objectType := NormalizeTypeString(obj.GetObjectType())
		data.Type = &objectType
	}
	if options.ShowContent {
		data.Content = parsedData(obj)
	}
	if options.ShowBcs {
		data.Bcs = rawData(obj)
	}
	if options.ShowOwner && obj.GetOwner() != nil {
		data.Owner = objectOwner(obj.GetOwner())
	}
	if options.ShowPreviousTransaction && obj.GetPreviousTransaction() != "" {
		previousTx := parseDigest(obj.GetPreviousTransaction())
		data.PreviousTransaction = &previousTx
	}
	if options.ShowStorageRebate {
		storageRebate := types.NewSafeSuiBigInt(obj.GetStorageRebate())
		data.StorageRebate = &storageRebate
	}
	if options.ShowDisplay && obj.GetDisplay() != nil {
		data.Display = obj.GetDisplay().GetOutput().AsInterface()
	}
	return data, nil
}

// ObjectNotFound builds the SuiObjectResponse JSON-RPC returns for an object
// that does not exist, so per-object NOT_FOUND gRPC errors keep v1 semantics.
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

// parsedData builds the showContent payload from the server-rendered json
// field. Package disassembly is not available over gRPC, so packages carry an
// empty Disassembled map.
func parsedData(obj *pb.Object) *lib.TagJson[types.SuiParsedData] {
	if obj.GetObjectType() == packageObjectType {
		return &lib.TagJson[types.SuiParsedData]{
			Data: types.SuiParsedData{Package: &types.SuiMovePackage{}},
		}
	}
	return &lib.TagJson[types.SuiParsedData]{
		Data: types.SuiParsedData{
			MoveObject: &types.SuiParsedMoveObject{
				Type:              NormalizeTypeString(obj.GetObjectType()),
				HasPublicTransfer: obj.GetHasPublicTransfer(),
				Fields:            obj.GetJson().AsInterface(),
			},
		},
	}
}

// rawData builds the showBcs payload from the raw Move struct contents.
// Package module maps are not mapped (JSON-RPC-only shape).
func rawData(obj *pb.Object) *lib.TagJson[types.SuiRawData] {
	if obj.GetObjectType() == packageObjectType {
		return nil
	}
	return &lib.TagJson[types.SuiRawData]{
		Data: types.SuiRawData{
			MoveObject: &types.SuiRawMoveObject{
				Type:              NormalizeTypeString(obj.GetObjectType()),
				HasPublicTransfer: obj.GetHasPublicTransfer(),
				Version:           obj.GetVersion(),
				BcsBytes:          lib.Base64Data(obj.GetContents().GetValue()),
			},
		},
	}
}

// objectOwner converts a proto Owner into the JSON-RPC ObjectOwner union used
// on object data and balance changes.
func objectOwner(protoOwner *pb.Owner) *types.ObjectOwner {
	switch protoOwner.GetKind() {
	case pb.Owner_ADDRESS, pb.Owner_CONSENSUS_ADDRESS:
		if addr, err := parseAddress(protoOwner.GetAddress()); err == nil {
			return &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &addr},
			}
		}
	case pb.Owner_OBJECT:
		if addr, err := parseAddress(protoOwner.GetAddress()); err == nil {
			return &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{ObjectOwner: &addr},
			}
		}
	case pb.Owner_SHARED:
		version := protoOwner.GetVersion()
		return &types.ObjectOwner{
			ObjectOwnerInternal: &types.ObjectOwnerInternal{
				Shared: &struct {
					InitialSharedVersion *sui_types.SequenceNumber `json:"initial_shared_version"`
				}{InitialSharedVersion: &version},
			},
		}
	case pb.Owner_IMMUTABLE:
		// The string variant of ObjectOwner is only settable via JSON (the
		// field is unexported), matching how v1 responses populate it.
		var immutable types.ObjectOwner
		if err := immutable.UnmarshalJSON([]byte(`"Immutable"`)); err == nil {
			return &immutable
		}
	}
	return nil
}
