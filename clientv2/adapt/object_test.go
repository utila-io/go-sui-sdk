package adapt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestObjectData_baseFieldsOnly(t *testing.T) {
	objDigestStr, objDigest := testDigest(0x81)
	obj := &pb.Object{
		ObjectId:   proto.String(longObjectID),
		Version:    proto.Uint64(33),
		Digest:     proto.String(objDigestStr),
		ObjectType: proto.String(longSuiType), // must be ignored without ShowType
	}

	got, err := ObjectData(obj, nil)
	require.NoError(t, err)
	require.Equal(t, &types.SuiObjectData{
		ObjectId: mustAddress(t, longObjectID),
		Version:  types.NewSafeSuiBigInt[sui_types.SequenceNumber](33),
		Digest:   objDigest,
	}, got)
}

func TestObjectData_allOptions(t *testing.T) {
	objDigestStr, objDigest := testDigest(0x82)
	prevTxStr, prevTx := testDigest(0x83)
	fields, err := structpb.NewValue(map[string]any{"balance": "1000"})
	require.NoError(t, err)

	longCoinType := longSuiPackage + "::coin::Coin<" + longSuiType + ">"
	obj := &pb.Object{
		ObjectId:            proto.String(longObjectID),
		Version:             proto.Uint64(44),
		Digest:              proto.String(objDigestStr),
		ObjectType:          proto.String(longCoinType),
		HasPublicTransfer:   proto.Bool(true),
		Contents:            &pb.Bcs{Value: []byte{0x0a, 0x0b}},
		Owner:               &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
		PreviousTransaction: proto.String(prevTxStr),
		StorageRebate:       proto.Uint64(9880000),
		Json:                fields,
	}

	got, err := ObjectData(obj, &types.SuiObjectDataOptions{
		ShowType:                true,
		ShowContent:             true,
		ShowBcs:                 true,
		ShowOwner:               true,
		ShowPreviousTransaction: true,
		ShowStorageRebate:       true,
		ShowDisplay:             true,
	})
	require.NoError(t, err)

	ownerAddr := mustAddress(t, longOwnerAddress)
	shortCoinType := "0x2::coin::Coin<" + shortSuiType + ">"
	storageRebate := types.NewSafeSuiBigInt[uint64](9880000)
	require.Equal(t, &types.SuiObjectData{
		ObjectId: mustAddress(t, longObjectID),
		Version:  types.NewSafeSuiBigInt[sui_types.SequenceNumber](44),
		Digest:   objDigest,
		Type:     ptr(shortCoinType),
		Content: &lib.TagJson[types.SuiParsedData]{Data: types.SuiParsedData{
			MoveObject: &types.SuiParsedMoveObject{
				Type:              shortCoinType,
				HasPublicTransfer: true,
				Fields:            map[string]any{"balance": "1000"},
			},
		}},
		Bcs: &lib.TagJson[types.SuiRawData]{Data: types.SuiRawData{
			MoveObject: &types.SuiRawMoveObject{
				Type:              shortCoinType,
				HasPublicTransfer: true,
				Version:           44,
				BcsBytes:          lib.Base64Data([]byte{0x0a, 0x0b}),
			},
		}},
		Owner: &types.ObjectOwner{
			ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
		},
		PreviousTransaction: &prevTx,
		StorageRebate:       &storageRebate,
	}, got)
}

func TestObjectData_package(t *testing.T) {
	objDigestStr, _ := testDigest(0x84)
	obj := &pb.Object{
		ObjectId:   proto.String(longSuiPackage),
		Version:    proto.Uint64(1),
		Digest:     proto.String(objDigestStr),
		ObjectType: proto.String("package"),
	}

	got, err := ObjectData(obj, &types.SuiObjectDataOptions{
		ShowType:    true,
		ShowContent: true,
		ShowBcs:     true,
	})
	require.NoError(t, err)
	// "package" is not a hex type string and passes through unchanged.
	require.Equal(t, ptr("package"), got.Type)
	// Package content carries an empty Disassembled map; no raw BCS payload.
	require.Equal(t, &lib.TagJson[types.SuiParsedData]{
		Data: types.SuiParsedData{Package: &types.SuiMovePackage{}},
	}, got.Content)
	require.Nil(t, got.Bcs)
}

func TestObjectData_ownerVariants(t *testing.T) {
	ownerAddr := mustAddress(t, longOwnerAddress)
	sharedVersion := sui_types.SequenceNumber(5)

	tests := []struct {
		name     string
		owner    *pb.Owner
		want     *types.ObjectOwner
		wantJSON string
	}{
		{
			name:  "address owner",
			owner: &pb.Owner{Kind: pb.Owner_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
			},
		},
		{
			name:  "consensus address owner maps to address owner",
			owner: &pb.Owner{Kind: pb.Owner_CONSENSUS_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
			},
		},
		{
			name:  "object owner",
			owner: &pb.Owner{Kind: pb.Owner_OBJECT.Enum(), Address: proto.String(longOwnerAddress)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{ObjectOwner: &ownerAddr},
			},
		},
		{
			name:  "shared owner",
			owner: &pb.Owner{Kind: pb.Owner_SHARED.Enum(), Version: proto.Uint64(5)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{
					Shared: &struct {
						InitialSharedVersion *sui_types.SequenceNumber `json:"initial_shared_version"`
					}{InitialSharedVersion: &sharedVersion},
				},
			},
		},
		{
			name:     "immutable owner",
			owner:    &pb.Owner{Kind: pb.Owner_IMMUTABLE.Enum()},
			wantJSON: `"Immutable"`,
		},
	}
	objDigestStr, _ := testDigest(0x85)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ObjectData(&pb.Object{
				ObjectId: proto.String(longObjectID),
				Version:  proto.Uint64(1),
				Digest:   proto.String(objDigestStr),
				Owner:    tt.owner,
			}, &types.SuiObjectDataOptions{ShowOwner: true})
			require.NoError(t, err)
			if tt.wantJSON != "" {
				data, err := json.Marshal(got.Owner)
				require.NoError(t, err)
				require.Equal(t, tt.wantJSON, string(data))
				return
			}
			require.Equal(t, tt.want, got.Owner)
		})
	}
}

func TestObjectData_invalidObjectID(t *testing.T) {
	_, err := ObjectData(&pb.Object{ObjectId: proto.String("0xzz")}, nil)
	require.Error(t, err)
}

func TestObjectNotFound(t *testing.T) {
	objectID := mustAddress(t, longObjectID)
	got := ObjectNotFound(objectID)
	require.Nil(t, got.Data)
	require.NotNil(t, got.Error)
	require.NotNil(t, got.Error.Data.NotExists)
	require.Equal(t, objectID, got.Error.Data.NotExists.ObjectId)
}
