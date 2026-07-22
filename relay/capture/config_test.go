package capture

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitFromEnvRequiresPersistentCryptoSecret(t *testing.T) {
	previousStorage := GetStorage()
	t.Cleanup(func() { SetStorage(previousStorage) })
	t.Setenv("RELAY_CAPTURE_STORAGE", "local")
	t.Setenv("RELAY_CAPTURE_LOCAL_DIR", t.TempDir())
	t.Setenv("CRYPTO_SECRET", "")

	err := InitFromEnv(context.Background())
	require.Error(t, err)
	require.False(t, IsConfigured())
}

func TestInitFromEnvRejectsUnsupportedCompression(t *testing.T) {
	previousStorage := GetStorage()
	t.Cleanup(func() { SetStorage(previousStorage) })
	t.Setenv("RELAY_CAPTURE_STORAGE", "local")
	t.Setenv("RELAY_CAPTURE_LOCAL_DIR", t.TempDir())
	t.Setenv("RELAY_CAPTURE_COMPRESSION", "zstd")
	t.Setenv("CRYPTO_SECRET", "relay-capture-config-test-secret")

	err := InitFromEnv(context.Background())
	require.ErrorContains(t, err, "unsupported relay capture compression")
	require.False(t, IsConfigured())
}

func TestInitFromEnvSegmentsIgnorePerPartCompression(t *testing.T) {
	previousStorage := GetStorage()
	t.Cleanup(func() { SetStorage(previousStorage) })
	t.Setenv("RELAY_CAPTURE_STORAGE", "s3")
	t.Setenv("RELAY_CAPTURE_S3_LAYOUT", "segments")
	t.Setenv("RELAY_CAPTURE_S3_BUCKET", "relay-captures-test")
	t.Setenv("RELAY_CAPTURE_S3_REGION", "us-east-1")
	t.Setenv("RELAY_CAPTURE_S3_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("RELAY_CAPTURE_S3_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("RELAY_CAPTURE_S3_SPOOL_DIR", t.TempDir())
	t.Setenv("RELAY_CAPTURE_COMPRESSION", "zstd")
	t.Setenv("CRYPTO_SECRET", "relay-capture-config-test-secret")

	require.NoError(t, InitFromEnv(context.Background()))
	storage, ok := GetStorage().(*SegmentStorage)
	require.True(t, ok)
	require.Equal(t, PayloadCompressionNone, storage.spool.compression)
	require.Equal(t, PayloadCompressionNone, storage.objects.compression)
}
