package capture

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStoresCompleteTextPayloads(t *testing.T) {
	previousLimit := MaxTextPayloadBytes()
	maxTextPayloadBytes.Store(64)
	t.Cleanup(func() { maxTextPayloadBytes.Store(previousLimit) })

	session := NewSession(
		Metadata{ChannelID: 1},
		http.Header{"Content-Type": []string{"application/json"}},
		"application/json; charset=utf-8",
		[]byte(`{"model":"test"}`),
		16,
		false,
	)
	session.AppendResponse(http.Header{"Content-Type": []string{"application/json"}}, []byte(`{"ok":true}`))

	artifact := session.Finalize(http.StatusOK, "success", "upstream-model")
	require.Empty(t, artifact.Metadata.SkippedReason)
	assert.Equal(t, []byte(`{"model":"test"}`), artifact.RequestBody)
	assert.Equal(t, []byte(`{"ok":true}`), artifact.ResponseBody)
	assert.True(t, artifact.Metadata.Request.Stored)
	assert.True(t, artifact.Metadata.Response.Stored)
	assert.NotEmpty(t, artifact.Metadata.Request.SHA256)
	assert.NotEmpty(t, artifact.Metadata.Response.SHA256)
}

func TestSessionDiscardsBothPartsForUnsupportedResponses(t *testing.T) {
	previousLimit := MaxTextPayloadBytes()
	maxTextPayloadBytes.Store(64)
	t.Cleanup(func() { maxTextPayloadBytes.Store(previousLimit) })

	session := NewSession(
		Metadata{},
		http.Header{"Content-Type": []string{"application/json"}},
		"application/json",
		[]byte(`{"input":"secret"}`),
		18,
		false,
	)
	session.AppendResponse(http.Header{"Content-Type": []string{"text/event-stream"}}, []byte("data: partial\n\n"))

	artifact := session.Finalize(http.StatusOK, "success", "")
	assert.Equal(t, "streaming_not_supported", artifact.Metadata.SkippedReason)
	assert.False(t, artifact.Metadata.Request.Stored)
	assert.False(t, artifact.Metadata.Response.Stored)
	assert.Empty(t, artifact.RequestBody)
	assert.Empty(t, artifact.ResponseBody)
}

func TestSessionSkipsOversizedRequest(t *testing.T) {
	previousLimit := MaxTextPayloadBytes()
	maxTextPayloadBytes.Store(8)
	t.Cleanup(func() { maxTextPayloadBytes.Store(previousLimit) })

	session := NewSession(
		Metadata{},
		http.Header{"Content-Type": []string{"application/json"}},
		"application/json",
		nil,
		9,
		false,
	)
	artifact := session.Finalize(http.StatusOK, "success", "")

	assert.Equal(t, "request_too_large", artifact.Metadata.SkippedReason)
	assert.False(t, artifact.Metadata.Request.Stored)
	assert.False(t, artifact.Metadata.Response.Stored)
}
