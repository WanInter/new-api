package capture

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSegmentArchiveRoundTrip(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "relay-capture-segment-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	captures := []segmentCapture{
		{
			metadata: Metadata{
				ID:        "capture-one",
				CreatedAt: 100,
				ChannelID: 42,
				Protocol:  "openai.chat_completions",
				Request:   PartMeta{Stored: true},
				Response:  PartMeta{Stored: true},
			},
			request:  []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
			response: []byte(`data: {"choices":[{"delta":{"content":"hi"}}]}\n\n`),
		},
		{
			metadata: Metadata{
				ID:        "capture-two",
				CreatedAt: 101,
				ChannelID: 42,
				Protocol:  "openai.responses",
				Request:   PartMeta{Stored: true},
			},
			request: []byte(`{"input":"second request"}`),
		},
	}

	encrypted, err := encodeSegmentArchive(captures)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encrypted, "enc:v1:"))
	assert.NotContains(t, encrypted, "second request")

	compressed, err := common.DecryptSecret(encrypted, segmentArchivePurpose)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(compressed), 2)
	assert.Equal(t, byte(0x1f), compressed[0])
	assert.Equal(t, byte(0x8b), compressed[1])

	decoded, err := decodeSegmentArchive([]byte(compressed), nil)
	require.NoError(t, err)
	require.Len(t, decoded, 2)
	assert.Equal(t, captures[0].metadata, decoded[0].metadata)
	assert.Equal(t, captures[0].request, decoded[0].request)
	assert.Equal(t, captures[0].response, decoded[0].response)
	assert.Equal(t, captures[1].metadata, decoded[1].metadata)
	assert.Equal(t, captures[1].request, decoded[1].request)
	assert.Empty(t, decoded[1].response)
}

func TestSegmentStorageUsesUncompressedEncryptedSpool(t *testing.T) {
	storage, err := NewSegmentStorage(context.Background(), SegmentOptions{
		S3: S3Options{
			Bucket:          "relay-captures-test",
			Region:          "us-east-1",
			AccessKeyID:     "test-access-key",
			SecretAccessKey: "test-secret-key",
			Compression:     PayloadCompressionGzip,
		},
		SpoolDir:      t.TempDir(),
		MaxBytes:      64 << 20,
		FlushInterval: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(storage.Close)

	assert.Equal(t, PayloadCompressionNone, storage.spool.compression)
	assert.Equal(t, PayloadCompressionNone, storage.objects.compression)
}

func TestSplitSegmentEntriesAndReplacementKeys(t *testing.T) {
	entries := []spoolEntry{
		{metadata: Metadata{ID: "first", Request: PartMeta{Size: 3}}},
		{metadata: Metadata{ID: "second", Request: PartMeta{Size: 3}}},
		{metadata: Metadata{ID: "third", Request: PartMeta{Size: 3}}},
	}
	batches := splitSegmentEntries(entries, 5)
	require.Len(t, batches, 3)
	assert.Equal(t, "first", batches[0][0].metadata.ID)
	assert.Equal(t, "second", batches[1][0].metadata.ID)
	assert.Equal(t, "third", batches[2][0].metadata.ID)

	storage := &SegmentStorage{objects: &S3Storage{prefix: "new-api"}}
	captures := []segmentCapture{
		{metadata: Metadata{ID: "first", CreatedAt: 100, ChannelID: 42}},
		{metadata: Metadata{ID: "last", CreatedAt: 101, ChannelID: 42}},
	}
	archiveKey, indexKey := storage.segmentKeys(captures, "")
	replacementArchiveKey, replacementIndexKey := storage.segmentKeys(captures, "replacement")
	assert.NotEqual(t, archiveKey, replacementArchiveKey)
	assert.NotEqual(t, indexKey, replacementIndexKey)
}
