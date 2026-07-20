package rpcv2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

func TestObjectData_baseFieldsOnly(t *testing.T) {
	objDigestStr, objDigest := testDigest(0x81)
	obj := &Object{
		ObjectId:   proto.String(longObjectID),
		Version:    proto.Uint64(33),
		Digest:     proto.String(objDigestStr),
		ObjectType: proto.String(longSuiType), // must be ignored without ShowType
	}

	got, err := obj.ToInternalType(nil)
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
	obj := &Object{
		ObjectId:            proto.String(longObjectID),
		Version:             proto.Uint64(44),
		Digest:              proto.String(objDigestStr),
		ObjectType:          proto.String(longCoinType),
		HasPublicTransfer:   proto.Bool(true),
		Contents:            &Bcs{Value: []byte{0x0a, 0x0b}},
		Owner:               &Owner{Kind: Owner_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
		PreviousTransaction: proto.String(prevTxStr),
		StorageRebate:       proto.Uint64(9880000),
		Json:                fields,
	}

	got, err := obj.ToInternalType(&types.SuiObjectDataOptions{
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
	obj := &Object{
		ObjectId:   proto.String(longSuiPackage),
		Version:    proto.Uint64(1),
		Digest:     proto.String(objDigestStr),
		ObjectType: proto.String("package"),
	}

	got, err := obj.ToInternalType(&types.SuiObjectDataOptions{
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
		owner    *Owner
		want     *types.ObjectOwner
		wantJSON string
	}{
		{
			name:  "address owner",
			owner: &Owner{Kind: Owner_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
			},
		},
		{
			name:  "consensus address owner maps to address owner",
			owner: &Owner{Kind: Owner_CONSENSUS_ADDRESS.Enum(), Address: proto.String(longOwnerAddress)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{AddressOwner: &ownerAddr},
			},
		},
		{
			name:  "object owner",
			owner: &Owner{Kind: Owner_OBJECT.Enum(), Address: proto.String(longOwnerAddress)},
			want: &types.ObjectOwner{
				ObjectOwnerInternal: &types.ObjectOwnerInternal{ObjectOwner: &ownerAddr},
			},
		},
		{
			name:  "shared owner",
			owner: &Owner{Kind: Owner_SHARED.Enum(), Version: proto.Uint64(5)},
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
			owner:    &Owner{Kind: Owner_IMMUTABLE.Enum()},
			wantJSON: `"Immutable"`,
		},
	}
	objDigestStr, _ := testDigest(0x85)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&Object{
				ObjectId: proto.String(longObjectID),
				Version:  proto.Uint64(1),
				Digest:   proto.String(objDigestStr),
				Owner:    tt.owner,
			}).ToInternalType(&types.SuiObjectDataOptions{ShowOwner: true})
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
	_, err := (&Object{ObjectId: proto.String("0xzz")}).ToInternalType(nil)
	require.Error(t, err)
}

// Callers convert straight off the response getter, so a response carrying no
// object arrives here as a nil receiver. It must error rather than pass the
// zero address off as a real object 0x0.
func TestObjectData_nilObject(t *testing.T) {
	got, err := (*Object)(nil).ToInternalType(&types.SuiObjectDataOptions{ShowType: true})
	require.Error(t, err)
	require.Nil(t, got)
}

func TestObjectNotFound(t *testing.T) {
	objectID := mustAddress(t, longObjectID)
	got := ObjectNotFound(objectID)
	require.Nil(t, got.Data)
	require.NotNil(t, got.Error)
	require.NotNil(t, got.Error.Data.NotExists)
	require.Equal(t, objectID, got.Error.Data.NotExists.ObjectId)
}

func TestCoin(t *testing.T) {
	objDigestStr, objDigest := testDigest(0x51)
	prevTxStr, prevTx := testDigest(0x52)

	got, err := (&Object{
		ObjectId:            proto.String(longObjectID),
		Version:             proto.Uint64(9007199254740993), // above 2^53: JSON numbers would lose precision
		Digest:              proto.String(objDigestStr),
		ObjectType:          proto.String("0x2::coin::Coin<" + longUsdcType + ">"),
		Balance:             proto.Uint64(5_000_000),
		PreviousTransaction: proto.String(prevTxStr),
	}).ToInternalCoin()
	require.NoError(t, err)
	require.Equal(t, types.Coin{
		CoinType:            shortUsdcType,
		CoinObjectId:        mustAddress(t, longObjectID),
		Version:             types.NewSafeSuiBigInt[uint64](9007199254740993),
		Digest:              objDigest,
		Balance:             types.NewSafeSuiBigInt[uint64](5_000_000),
		PreviousTransaction: prevTx,
	}, got)
	require.Nil(t, got.LockedUntilEpoch)

	// SafeSuiBigInt fields must marshal as JSON strings. gRPC carries them as
	// numeric uint64; the internal types re-encode them as strings, which is
	// what consumers of the JSON decode.
	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(data), `"version":"9007199254740993"`)
	require.Contains(t, string(data), `"balance":"5000000"`)
}

func TestCoin_errors(t *testing.T) {
	tests := []struct {
		name string
		in   *Object
	}{
		{
			name: "invalid object id",
			in: &Object{
				ObjectId:   proto.String("0xzz"),
				ObjectType: proto.String("0x2::coin::Coin<" + shortSuiType + ">"),
			},
		},
		{
			name: "object type without generic",
			in: &Object{
				ObjectId:   proto.String(longObjectID),
				ObjectType: proto.String("0x2::coin::Coin"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.in.ToInternalCoin()
			require.Error(t, err)
		})
	}
}

func TestCoinTypeFromObjectType(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "long form inner type normalized",
			in:   "0x2::coin::Coin<" + longSuiType + ">",
			want: shortSuiType,
		},
		{
			name: "long form outer and inner",
			in:   longSuiPackage + "::coin::Coin<" + longUsdcType + ">",
			want: shortUsdcType,
		},
		{
			name: "nested generic inner type uses last closing bracket",
			in:   "0x2::coin::Coin<0x00000000000000000000000000000000000000000000000000000000000000ab::lp::LP<" + longSuiType + ", " + longUsdcType + ">>",
			want: "0xab::lp::LP<" + shortSuiType + ", " + shortUsdcType + ">",
		},
		{
			name:    "no generic brackets",
			in:      "0x2::coin::Coin",
			wantErr: true,
		},
		{
			name:    "package type",
			in:      "package",
			wantErr: true,
		},
		{
			name:    "closing bracket before opening",
			in:      "0x2>oops<",
			wantErr: true,
		},
		{
			name:    "empty string",
			in:      "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coinTypeFromObjectType(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
