package rpcv2

import (
	"fmt"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// Owner has two internal counterparts with different shapes and different
// tolerance for unknown kinds, so neither conversion claims the bare
// ToInternalType name: ToInternalOwner yields the sui_types.Owner enum used in
// effects, ToInternalObjectOwner yields the types.ObjectOwner union used on
// object data and object changes.

// ToInternalOwner converts an Owner into the sui_types.Owner enum. A
// CONSENSUS_ADDRESS owner is surfaced as AddressOwner, the closest internal
// variant; an unknown kind yields the zero value.
func (x *Owner) ToInternalOwner() (sui_types.Owner, error) {
	var out sui_types.Owner
	switch x.GetKind() {
	case Owner_ADDRESS, Owner_CONSENSUS_ADDRESS:
		addr, err := parseAddress(x.GetAddress())
		if err != nil {
			return out, fmt.Errorf("owner: %w", err)
		}
		out.AddressOwner = &addr
	case Owner_OBJECT:
		addr, err := parseAddress(x.GetAddress())
		if err != nil {
			return out, fmt.Errorf("owner: %w", err)
		}
		out.ObjectOwner = &addr
	case Owner_SHARED:
		out.Shared = &struct {
			InitialSharedVersion sui_types.SequenceNumber `json:"initial_shared_version"`
		}{InitialSharedVersion: x.GetVersion()}
	case Owner_IMMUTABLE:
		out.Immutable = &lib.EmptyEnum{}
	}
	return out, nil
}

// ToInternalObjectOwner converts an Owner into the types.ObjectOwner union
// used on object data and object changes. A CONSENSUS_ADDRESS owner is
// surfaced as AddressOwner, the closest internal variant.
func (x *Owner) ToInternalObjectOwner() (types.ObjectOwner, error) {
	internal := &types.ObjectOwnerInternal{}
	switch x.GetKind() {
	case Owner_ADDRESS, Owner_CONSENSUS_ADDRESS:
		addr, err := parseAddress(x.GetAddress())
		if err != nil {
			return types.ObjectOwner{}, fmt.Errorf("owner: %w", err)
		}
		internal.AddressOwner = &addr
	case Owner_OBJECT:
		addr, err := parseAddress(x.GetAddress())
		if err != nil {
			return types.ObjectOwner{}, fmt.Errorf("owner: %w", err)
		}
		internal.ObjectOwner = &addr
	case Owner_SHARED:
		version := x.GetVersion()
		internal.Shared = &struct {
			InitialSharedVersion *sui_types.SequenceNumber `json:"initial_shared_version"`
		}{InitialSharedVersion: &version}
	case Owner_IMMUTABLE:
		// ObjectOwner's bare-string form ("Immutable") is only settable
		// through its UnmarshalJSON: the embedded *string is unexported.
		var owner types.ObjectOwner
		if err := owner.UnmarshalJSON([]byte(`"Immutable"`)); err != nil {
			return types.ObjectOwner{}, fmt.Errorf("owner: %w", err)
		}
		return owner, nil
	default:
		return types.ObjectOwner{}, fmt.Errorf("owner: unknown kind %v", x.GetKind())
	}
	return types.ObjectOwner{ObjectOwnerInternal: internal}, nil
}
