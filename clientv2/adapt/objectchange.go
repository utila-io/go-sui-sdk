package adapt

import (
	"fmt"
	"regexp"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// ObjectChanges derives the JSON-RPC showObjectChanges list from the effects'
// changed_objects; sender is the transaction's sender (zero for system
// transactions, whose kinds sui_types cannot decode — that matches their real
// sender). Matching live JSON-RPC nodes, only written objects are rendered —
// mutated (gas coin included), created and published; deletions, wraps,
// unwraps and accumulator writes do not appear, and an owner change renders as
// "mutated" (v1 never emits the legacy "transferred" variant). Unrenderable
// entries are dropped and reported in the error slice.
func ObjectChanges(sender sui_types.SuiAddress, fx *pb.TransactionEffects) ([]lib.TagJson[types.ObjectChange], []error) {
	if fx == nil {
		return nil, nil
	}
	var errs []error
	var out []lib.TagJson[types.ObjectChange]
	for _, changed := range fx.GetChangedObjects() {
		change, err := objectChange(sender, changed)
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

// objectChange renders one changed_objects entry, or nil for the entries
// JSON-RPC leaves out of objectChanges.
func objectChange(sender sui_types.SuiAddress, changed *pb.ChangedObject) (*types.ObjectChange, error) {
	created := changed.GetIdOperation() == pb.ChangedObject_CREATED
	switch {
	case changed.GetOutputState() == pb.ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE && created:
		return publishedChange(changed)
	case changed.GetOutputState() != pb.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE:
		return nil, nil
	case created:
		return createdChange(sender, changed)
	case changed.GetIdOperation() == pb.ChangedObject_NONE &&
		changed.GetInputState() == pb.ChangedObject_INPUT_OBJECT_STATE_EXISTS:
		return mutatedChange(sender, changed)
	default: // unwrapped objects are not part of objectChanges
		return nil, nil
	}
}

func publishedChange(changed *pb.ChangedObject) (*types.ObjectChange, error) {
	packageID, err := parseAddress(changed.GetObjectId())
	if err != nil {
		return nil, err
	}
	change := &types.ObjectChange{}
	// Nodules (the package's module names, "modules" on the wire) has no gRPC
	// source and stays nil; the types struct's json tag never matches v1's
	// "modules" key either, so decoded v1 responses carry nil there too.
	change.Published = &struct {
		PackageId sui_types.ObjectID                            `json:"packageId"`
		Version   types.SafeSuiBigInt[sui_types.SequenceNumber] `json:"version"`
		Digest    sui_types.ObjectDigest                        `json:"digest"`
		Nodules   []string                                      `json:"nodules"`
	}{
		PackageId: packageID,
		Version:   types.NewSafeSuiBigInt[sui_types.SequenceNumber](changed.GetOutputVersion()),
		Digest:    parseDigest(changed.GetOutputDigest()),
	}
	return change, nil
}

func createdChange(sender sui_types.SuiAddress, changed *pb.ChangedObject) (*types.ObjectChange, error) {
	objectID, owner, err := writtenObjectParts(changed)
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
		ObjectType: normalizeObjectType(changed.GetObjectType()),
		ObjectId:   objectID,
		Version:    types.NewSafeSuiBigInt[sui_types.SequenceNumber](changed.GetOutputVersion()),
		Digest:     parseDigest(changed.GetOutputDigest()),
	}
	return change, nil
}

func mutatedChange(sender sui_types.SuiAddress, changed *pb.ChangedObject) (*types.ObjectChange, error) {
	objectID, owner, err := writtenObjectParts(changed)
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
		ObjectType:      normalizeObjectType(changed.GetObjectType()),
		ObjectId:        objectID,
		Version:         types.NewSafeSuiBigInt[sui_types.SequenceNumber](changed.GetOutputVersion()),
		PreviousVersion: types.NewSafeSuiBigInt[sui_types.SequenceNumber](changed.GetInputVersion()),
		Digest:          parseDigest(changed.GetOutputDigest()),
	}
	return change, nil
}

// writtenObjectParts parses the id and post-transaction owner of an
// OBJECT_WRITE entry.
func writtenObjectParts(changed *pb.ChangedObject) (sui_types.ObjectID, types.ObjectOwner, error) {
	objectID, err := parseAddress(changed.GetObjectId())
	if err != nil {
		return sui_types.ObjectID{}, types.ObjectOwner{}, err
	}
	owner, err := objectOwner(changed.GetOutputOwner())
	if err != nil {
		return sui_types.ObjectID{}, types.ObjectOwner{}, err
	}
	return objectID, owner, nil
}

// typeParamSeparator matches generic type parameter separators with no
// following space, which is how gRPC effects print them.
var typeParamSeparator = regexp.MustCompile(`,\s*`)

// normalizeObjectType rewrites a changed object's type to the exact v1
// objectType rendering: short-form addresses and ", " between type parameters.
func normalizeObjectType(typeStr string) string {
	return NormalizeTypeString(typeParamSeparator.ReplaceAllString(typeStr, ", "))
}
