package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptSecretRoundTripAndPurposeIsolation(t *testing.T) {
	originalSecret := CryptoSecret
	CryptoSecret = "stable-test-secret"
	t.Cleanup(func() { CryptoSecret = originalSecret })

	ciphertext, err := EncryptSecret("credential-value", "storage/access-key")
	require.NoError(t, err)
	assert.True(t, IsEncryptedSecret(ciphertext))
	assert.NotContains(t, ciphertext, "credential-value")

	plaintext, err := DecryptSecret(ciphertext, "storage/access-key")
	require.NoError(t, err)
	assert.Equal(t, "credential-value", plaintext)

	_, err = DecryptSecret(ciphertext, "storage/other-key")
	assert.Error(t, err)
}

func TestDecryptSecretRejectsTamperingAndPlaintext(t *testing.T) {
	originalSecret := CryptoSecret
	CryptoSecret = "stable-test-secret"
	t.Cleanup(func() { CryptoSecret = originalSecret })

	ciphertext, err := EncryptSecret("credential-value", "storage/access-key")
	require.NoError(t, err)

	replacement := "A"
	if strings.HasSuffix(ciphertext, replacement) {
		replacement = "B"
	}
	tampered := ciphertext[:len(ciphertext)-1] + replacement
	_, err = DecryptSecret(tampered, "storage/access-key")
	assert.Error(t, err)

	_, err = DecryptSecret("credential-value", "storage/access-key")
	assert.EqualError(t, err, "unsupported encrypted secret format")
}
