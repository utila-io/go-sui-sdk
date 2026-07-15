package adapt

import (
	"encoding/base64"
	"encoding/binary"
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

func TestDevInspectTransactionData(t *testing.T) {
	sender := mustAddress(t, longOwnerAddress)
	kindBytes := []byte{0xaa, 0xbb, 0xcc}
	gasPrice := uint64(1000)

	got := DevInspectTransactionData(sender, kindBytes, gasPrice)

	// Layout: V1 tag, kind, sender, empty gas payment vector, gas owner,
	// gas price (u64 LE), gas budget (u64 LE), expiration None.
	want := []byte{0x00}
	want = append(want, kindBytes...)
	want = append(want, sender[:]...)
	want = append(want, 0x00)
	want = append(want, sender[:]...)
	want = binary.LittleEndian.AppendUint64(want, gasPrice)
	want = binary.LittleEndian.AppendUint64(want, 50_000_000_000)
	want = append(want, 0x00)
	require.Equal(t, want, got)
	require.Len(t, got, 1+len(kindBytes)+32+1+32+8+8+1)
}
