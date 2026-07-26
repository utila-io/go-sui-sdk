package rpcv2

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utila-io/go-sui-sdk/lib"
	"github.com/utila-io/go-sui-sdk/sui_types"
)

func TestSignatureBytes(t *testing.T) {
	var ed25519Sig sui_types.Ed25519SuiSignature
	for i := range ed25519Sig.Signature {
		ed25519Sig.Signature[i] = byte(i)
	}

	tests := []struct {
		name    string
		in      any
		want    []byte
		wantErr bool
	}{
		{
			name: "ed25519 signature value",
			in:   sui_types.Signature{Ed25519SuiSignature: &ed25519Sig},
			want: ed25519Sig.Signature[:],
		},
		{
			name: "ed25519 signature pointer",
			in:   &sui_types.Signature{Ed25519SuiSignature: &ed25519Sig},
			want: ed25519Sig.Signature[:],
		},
		{
			name: "base64 string",
			in:   base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}),
			want: []byte{0x01, 0x02, 0x03},
		},
		{
			name: "base64 data",
			in:   lib.Base64Data([]byte{0x04, 0x05}),
			want: []byte{0x04, 0x05},
		},
		{
			name: "raw bytes",
			in:   []byte{0x06, 0x07},
			want: []byte{0x06, 0x07},
		},
		{
			name:    "invalid base64 string",
			in:      "!!not-base64!!",
			wantErr: true,
		},
		{
			name:    "empty signature union",
			in:      sui_types.Signature{},
			wantErr: true,
		},
		{
			name:    "unsupported type",
			in:      42,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SignatureBytes(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
