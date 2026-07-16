package clientv2

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/utila-io/go-sui-sdk/clientv2/adapt"
	pb "github.com/utila-io/go-sui-sdk/clientv2/internal/pb/sui/rpc/v2"
	"github.com/utila-io/go-sui-sdk/sui_types"
	"github.com/utila-io/go-sui-sdk/types"
)

// GetObject returns the latest version of the object, shaped per options. A
// missing object is a notExists response, not a call error, like JSON-RPC.
func (c *Client) GetObject(
	ctx context.Context,
	objID sui_types.ObjectID,
	options *types.SuiObjectDataOptions,
) (*types.SuiObjectResponse, error) {
	resp, err := c.ledger.GetObject(ctx, &pb.GetObjectRequest{
		ObjectId: proto.String(objID.String()),
		ReadMask: &fieldmaskpb.FieldMask{Paths: adapt.ObjectReadMaskPaths(options)},
	})
	if status.Code(err) == codes.NotFound {
		notFound := adapt.ObjectNotFound(objID)
		return &notFound, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetObject: %w", err)
	}
	data, err := adapt.ObjectData(resp.GetObject(), options)
	if err != nil {
		return nil, fmt.Errorf("GetObject: %w", err)
	}
	return &types.SuiObjectResponse{Data: data}, nil
}

// MultiGetObjects returns the latest versions of the given objects, shaped
// per options and in request order. Per-object NOT_FOUND results become
// notExists response entries; any other per-object error fails the call.
func (c *Client) MultiGetObjects(
	ctx context.Context,
	objIDs []sui_types.ObjectID,
	options *types.SuiObjectDataOptions,
) ([]types.SuiObjectResponse, error) {
	if len(objIDs) == 0 {
		return nil, nil
	}
	requests := make([]*pb.GetObjectRequest, len(objIDs))
	for i, objID := range objIDs {
		requests[i] = &pb.GetObjectRequest{ObjectId: proto.String(objID.String())}
	}
	resp, err := c.ledger.BatchGetObjects(ctx, &pb.BatchGetObjectsRequest{
		Requests: requests,
		ReadMask: &fieldmaskpb.FieldMask{Paths: adapt.ObjectReadMaskPaths(options)},
	})
	if err != nil {
		return nil, fmt.Errorf("MultiGetObjects: %w", err)
	}
	results := resp.GetObjects()
	if len(results) != len(objIDs) {
		return nil, fmt.Errorf("MultiGetObjects: got %d results for %d objects", len(results), len(objIDs))
	}

	responses := make([]types.SuiObjectResponse, 0, len(results))
	for i, result := range results {
		if resultErr := result.GetError(); resultErr != nil {
			if codes.Code(resultErr.GetCode()) == codes.NotFound {
				responses = append(responses, adapt.ObjectNotFound(objIDs[i]))
				continue
			}
			return nil, fmt.Errorf("MultiGetObjects: object %s: %s", objIDs[i], resultErr.GetMessage())
		}
		data, err := adapt.ObjectData(result.GetObject(), options)
		if err != nil {
			return nil, fmt.Errorf("MultiGetObjects: %w", err)
		}
		responses = append(responses, types.SuiObjectResponse{Data: data})
	}
	return responses, nil
}
