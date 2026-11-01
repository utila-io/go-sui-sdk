package rpcv2

import (
	"fmt"
	"regexp"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// ToInternalObjectChanges derives the object changes list from the effects'
// changed_objects; sender is the transaction's sender (zero for system
// transactions, whose kinds sui_types cannot decode — that matches their real
// sender). Only written objects are rendered — mutated (gas coin included),
// created and published; deletions, wraps, unwraps and accumulator writes do
// not appear, and an owner change renders as "mutated", since the internal
// shape never emits the legacy "transferred" variant. Unrenderable entries are
// dropped and reported in the error slice.
func (x *TransactionEffects) ToInternalObjectChanges(sender sui_types.SuiAddress) ([]lib.TagJson[types.ObjectChange], []error) {
	if x == nil {
		return nil, nil
	}
	var errs []error
	var out []lib.TagJson[types.ObjectChange]
	for _, changed := range x.GetChangedObjects() {
		change, err := changed.toInternalObjectChange(sender)
		if err != nil {
			errs = append(errs, fmt.Errorf("object change %s: %w", changed.GetObjectId(), err))
			continue
		}
		if change != nil {
			out = append(out, lib.TagJson[types.ObjectChange]{Data: *change})
		}
	}
	return out, errs
}

// toInternalObjectChange renders one changed_objects entry, or nil for the
// entries the internal object changes list leaves out.
func (x *ChangedObject) toInternalObjectChange(sender sui_types.SuiAddress) (*types.ObjectChange, error) {
	created := x.GetIdOperation() == ChangedObject_CREATED
	switch {
	case x.GetOutputState() == ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE && created:
		return x.toInternalPublishedChange()
	case x.GetOutputState() != ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE:
		return nil, nil
	case created:
		return x.toInternalCreatedChange(sender)
	case x.GetIdOperation() == ChangedObject_NONE &&
		x.GetInputState() == ChangedObject_INPUT_OBJECT_STATE_EXISTS:
		return x.toInternalMutatedChange(sender)
	default: // unwrapped objects are not part of the object changes list
		return nil, nil
	}
}

func (x *ChangedObject) toInternalPublishedChange() (*types.ObjectChange, error) {
	packageID, err := parseAddress(x.GetObjectId())
	if err != nil {
		return nil, err
	}
	change := &types.ObjectChange{}
	// Nodules (the package's module names, "modules" on the wire) has no gRPC
	// source and stays nil; the types struct's json tag never matches the
	// "modules" key either, so decoded responses carry nil there too.
	change.Published = &struct {
		PackageId sui_types.ObjectID                            `json:"packageId"`
		Version   types.SafeSuiBigInt[sui_types.SequenceNumber] `json:"version"`
		Digest    sui_types.ObjectDigest                        `json:"digest"`
		Nodules   []string                                      `json:"nodules"`
	}{
		PackageId: packageID,
		Version:   types.NewSafeSuiBigInt[sui_types.SequenceNumber](x.GetOutputVersion()),
		Digest:    parseDigest(x.GetOutputDigest()),
	}
	return change, nil
}

func (x *ChangedObject) toInternalCreatedChange(sender sui_types.SuiAddress) (*types.ObjectChange, error) {
	objectID, owner, err := x.toInternalWrittenObjectParts()
	if err != nil {
		return nil, err
	}
	change := &types.ObjectChange{}
	change.Created = &struct {
		Sender     sui_types.SuiAddress                          `json:"sender"`
		Owner      types.ObjectOwner                             `json:"owner"`
		ObjectType string                                        `json:"objectType"`
		ObjectId   sui_types.ObjectID                            `json:"objectId"`
		Version    types.SafeSuiBigInt[sui_types.SequenceNumber] `json:"version"`
		Digest     sui_types.ObjectDigest                        `json:"digest"`
	}{
		Sender:     sender,
		Owner:      owner,
		ObjectType: normalizeObjectType(x.GetObjectType()),
		ObjectId:   objectID,
		Version:    types.NewSafeSuiBigInt[sui_types.SequenceNumber](x.GetOutputVersion()),
		Digest:     parseDigest(x.GetOutputDigest()),
	}
	return change, nil
}

func (x *ChangedObject) toInternalMutatedChange(sender sui_types.SuiAddress) (*types.ObjectChange, error) {
	objectID, owner, err := x.toInternalWrittenObjectParts()
	if err != nil {
		return nil, err
	}
	change := &types.ObjectChange{}
	change.Mutated = &struct {
		Sender          sui_types.SuiAddress                          `json:"sender"`
		Owner           types.ObjectOwner                             `json:"owner"`
		ObjectType      string                                        `json:"objectType"`
		ObjectId        sui_types.ObjectID                            `json:"objectId"`
		Version         types.SafeSuiBigInt[sui_types.SequenceNumber] `json:"version"`
		PreviousVersion types.SafeSuiBigInt[sui_types.SequenceNumber] `json:"previousVersion"`
		Digest          sui_types.ObjectDigest                        `json:"digest"`
	}{
		Sender:          sender,
		Owner:           owner,
		ObjectType:      normalizeObjectType(x.GetObjectType()),
		ObjectId:        objectID,
		Version:         types.NewSafeSuiBigInt[sui_types.SequenceNumber](x.GetOutputVersion()),
		PreviousVersion: types.NewSafeSuiBigInt[sui_types.SequenceNumber](x.GetInputVersion()),
		Digest:          parseDigest(x.GetOutputDigest()),
	}
	return change, nil
}

// toInternalWrittenObjectParts parses the id and post-transaction owner of an
// OBJECT_WRITE entry.
func (x *ChangedObject) toInternalWrittenObjectParts() (sui_types.ObjectID, types.ObjectOwner, error) {
	objectID, err := parseAddress(x.GetObjectId())
	if err != nil {
		return sui_types.ObjectID{}, types.ObjectOwner{}, err
	}
	owner, err := x.GetOutputOwner().ToInternalObjectOwner()
	if err != nil {
		return sui_types.ObjectID{}, types.ObjectOwner{}, err
	}
	return objectID, owner, nil
}

// typeParamSeparator matches generic type parameter separators with no
// following space, which is how gRPC effects print them.
var typeParamSeparator = regexp.MustCompile(`,\s*`)

// normalizeObjectType rewrites a changed object's type to the exact objectType
// rendering the internal shape uses: short-form addresses and ", " between
// type parameters.
func normalizeObjectType(typeStr string) string {
	return normalizeTypeString(typeParamSeparator.ReplaceAllString(typeStr, ", "))
}
