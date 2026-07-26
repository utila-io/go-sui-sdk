package sui_types

import (
	"testing"

	"github.com/fardream/go-bcs/bcs"
	"github.com/stretchr/testify/require"
)

func TestObjectArgReceivingBcsRoundTrip(t *testing.T) {
	objectID, err := NewAddressFromHex("0x1351555dfb66e746c04e1f22a6b55535352075806d55558796b78d732dc7ec33")
	require.NoError(t, err)
	digest, err := NewDigest("FPkg47tVeD87ufWfcfkfaV4eBg85Yi8XuXckNJUCsWPM")
	require.NoError(t, err)
	ref := &ObjectRef{ObjectId: *objectID, Version: 450348042, Digest: *digest}

	arg := ObjectArg{Receiving: ref}
	require.Equal(t, *objectID, arg.id())

	encoded, err := bcs.Marshal(arg)
	require.NoError(t, err)
	// Receiving is BCS enum variant 2, with the same ObjectRef payload as
	// ImmOrOwnedObject (variant 0).
	require.EqualValues(t, 2, encoded[0])
	owned, err := bcs.Marshal(ObjectArg{ImmOrOwnedObject: ref})
	require.NoError(t, err)
	require.Equal(t, owned[1:], encoded[1:])

	var decoded ObjectArg
	_, err = bcs.Unmarshal(encoded, &decoded)
	require.NoError(t, err)
	require.Nil(t, decoded.ImmOrOwnedObject)
	require.Nil(t, decoded.SharedObject)
	require.Equal(t, arg, decoded)
}
