package adapt

import (
	"fmt"
	"regexp"

	"github.com/fardream/go-bcs/bcs"

	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// ObjectChanges derives the JSON-RPC showObjectChanges list from the effects'
// changed_objects. Matching live JSON-RPC nodes, only written objects are
// rendered — mutated (gas coin included), created and published; deletions,
// wraps, unwraps and accumulator writes do not appear, and an owner change
// renders as "mutated" (v1 never emits the legacy "transferred" variant).
// Unrenderable entries are dropped and reported in the error slice.
func ObjectChanges(txData []byte, fx *pb.TransactionEffects) ([]lib.TagJson[types.ObjectChange], []error) {
	if fx == nil {
		return nil, nil
	}
	sender := transactionSender(txData)
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

// transactionSender BCS-decodes TransactionData and returns V1.Sender. System
// transaction kinds are not in sui_types' enum and fail to decode; every
// system transaction's sender is the zero address, which is what that yields.
func transactionSender(txData []byte) sui_types.SuiAddress {
	var data sui_types.TransactionData
	if _, err := bcs.Unmarshal(txData, &data); err != nil || data.V1 == nil {
		return sui_types.SuiAddress{}
	}
	return data.V1.Sender
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
	owner, err := changeOwner(changed.GetOutputOwner())
	if err != nil {
		return sui_types.ObjectID{}, types.ObjectOwner{}, err
	}
	return objectID, owner, nil
}

// changeOwner converts a proto Owner into the types.ObjectOwner shape used by
// objectChanges. A CONSENSUS_ADDRESS owner is surfaced as AddressOwner, the
// closest v1 variant.
func changeOwner(protoOwner *pb.Owner) (types.ObjectOwner, error) {
	internal := &types.ObjectOwnerInternal{}
	switch protoOwner.GetKind() {
	case pb.Owner_ADDRESS, pb.Owner_CONSENSUS_ADDRESS:
		addr, err := parseAddress(protoOwner.GetAddress())
		if err != nil {
			return types.ObjectOwner{}, fmt.Errorf("owner: %w", err)
		}
		internal.AddressOwner = &addr
	case pb.Owner_OBJECT:
		addr, err := parseAddress(protoOwner.GetAddress())
		if err != nil {
			return types.ObjectOwner{}, fmt.Errorf("owner: %w", err)
		}
		internal.ObjectOwner = &addr
	case pb.Owner_SHARED:
		version := protoOwner.GetVersion()
		internal.Shared = &struct {
			InitialSharedVersion *sui_types.SequenceNumber `json:"initial_shared_version"`
		}{InitialSharedVersion: &version}
	case pb.Owner_IMMUTABLE:
		// ObjectOwner's bare-string form ("Immutable") is only settable
		// through its UnmarshalJSON: the embedded *string is unexported.
		var owner types.ObjectOwner
		if err := owner.UnmarshalJSON([]byte(`"Immutable"`)); err != nil {
			return types.ObjectOwner{}, fmt.Errorf("owner: %w", err)
		}
		return owner, nil
	default:
		return types.ObjectOwner{}, fmt.Errorf("owner: unknown kind %v", protoOwner.GetKind())
	}
	return types.ObjectOwner{ObjectOwnerInternal: internal}, nil
}

// typeParamSeparator matches generic type parameter separators with no
// following space, which is how gRPC effects print them.
var typeParamSeparator = regexp.MustCompile(`,\s*`)

// normalizeObjectType rewrites a changed object's type to the exact v1
// objectType rendering: short-form addresses and ", " between type parameters.
func normalizeObjectType(typeStr string) string {
	return NormalizeTypeString(typeParamSeparator.ReplaceAllString(typeStr, ", "))
}
