package rpcv2

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

// SignatureBytes extracts the raw serialized signature (flag || sig || pubkey)
// from the value forms accepted by ExecuteTransactionBlock.
func SignatureBytes(signature any) ([]byte, error) {
	switch sig := signature.(type) {
	case sui_types.Signature:
		return signatureData(sig)
	case *sui_types.Signature:
		return signatureData(*sig)
	case lib.Base64Data:
		return sig.Data(), nil
	case []byte:
		return sig, nil
	case string:
		return base64.StdEncoding.DecodeString(sig)
	default:
		return nil, fmt.Errorf("unsupported signature type %T", signature)
	}
}

func signatureData(sig sui_types.Signature) ([]byte, error) {
	switch {
	case sig.Ed25519SuiSignature != nil:
		return sig.Ed25519SuiSignature.Signature[:], nil
	case sig.Secp256k1SuiSignature != nil:
		return sig.Secp256k1SuiSignature.Signature, nil
	case sig.Secp256r1SuiSignature != nil:
		return sig.Secp256r1SuiSignature.Signature, nil
	default:
		return nil, errors.New("nil signature")
	}
}
