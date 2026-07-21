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
