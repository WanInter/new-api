package capture

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalStorageSaveListOpenAndDelete(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "relay-capture-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	first := Artifact{
		Metadata: Metadata{
			ID:        "capture-one",
			CreatedAt: 100,
			ChannelID: 42,
			Protocol:  "openai.chat_completions",
			Request:   PartMeta{Stored: true},
			Response:  PartMeta{Stored: true},
		},
		RequestBody:  []byte(`{"message":"first request"}`),
		ResponseBody: []byte(`{"message":"first response"}`),
	}
	second := Artifact{
		Metadata: Metadata{
			ID:        "capture-two",
			CreatedAt: 200,
			ChannelID: 42,
			Protocol:  "openai.responses",
			Request:   PartMeta{Stored: true},
		},
		RequestBody: []byte(`{"input":"second request"}`),
	}
	require.NoError(t, storage.Save(context.Background(), first))
	require.NoError(t, storage.Save(context.Background(), second))

	result, err := storage.List(context.Background(), ListFilter{ChannelID: 42, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Len(t, result.Items, 2)
	assert.Equal(t, "capture-two", result.Items[0].ID)
	assert.Equal(t, "capture-one", result.Items[1].ID)

	body, metadata, err := storage.Open(context.Background(), "capture-one", PartResponse)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, body.Close()) })
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"message":"first response"}`), content)
	assert.Equal(t, "capture-one", metadata.ID)

	assertEncryptedAtRest(t, storage.baseDir, "first request")
	deleted, err := storage.DeleteBefore(context.Background(), 150)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	result, err = storage.List(context.Background(), ListFilter{ChannelID: 42, Limit: 10})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "capture-two", result.Items[0].ID)
	assert.NoError(t, storage.Health(context.Background()))
}

func TestLocalStorageGzipRoundTrip(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "relay-capture-gzip-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	storage, err := NewLocalStorageWithCompression(t.TempDir(), PayloadCompressionGzip)
	require.NoError(t, err)
	request := []byte(strings.Repeat(`{"message":"repeated text"}`, 128))
	artifact := Artifact{
		Metadata: Metadata{
			ID:        "gzip-capture",
			CreatedAt: 100,
			ChannelID: 42,
			Protocol:  "openai.chat_completions",
			Request:   PartMeta{Stored: true},
		},
		RequestBody: request,
	}
	require.NoError(t, storage.Save(context.Background(), artifact))

	result, err := storage.List(context.Background(), ListFilter{ID: artifact.Metadata.ID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "gzip", result.Items[0].Request.Compression)

	body, metadata, err := storage.Open(context.Background(), artifact.Metadata.ID, PartRequest)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, body.Close()) })
	plaintext, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, request, plaintext)
	assert.Equal(t, "gzip", metadata.Request.Compression)

	encrypted, err := os.ReadFile(filepath.Join(storage.baseDir, "1970", "01", "01", "channel-42", "gzip-capture", "request.enc"))
	require.NoError(t, err)
	compressed, err := common.DecryptSecret(string(encrypted), "relay-capture")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(compressed, "\x1f\x8b"))
}

func assertEncryptedAtRest(t *testing.T, baseDir string, plaintext string) {
	t.Helper()
	foundEncryptedPart := false
	err := filepath.WalkDir(baseDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".enc") {
			return nil
		}
		foundEncryptedPart = true
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assert.NotContains(t, string(body), plaintext)
		assert.True(t, strings.HasPrefix(string(body), "enc:v1:"))
		return nil
	})
	require.NoError(t, err)
	assert.True(t, foundEncryptedPart)
}
