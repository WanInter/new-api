package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		request  string
		response string
		expected []map[string]any
	}{
		{
			name:     "chat completions",
			protocol: "openai.chat_completions",
			request:  `{"messages":[{"role":"system","content":"brief"},{"role":"user","content":"hello"}]}`,
			response: `{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`,
			expected: []map[string]any{{"role": "system", "content": "brief"}, {"role": "user", "content": "hello"}, {"role": "assistant", "content": "hi"}},
		},
		{
			name:     "anthropic messages",
			protocol: "anthropic.messages",
			request:  `{"system":"brief","messages":[{"role":"user","content":"hello"}]}`,
			response: `{"content":[{"type":"text","text":"hi"}]}`,
			expected: []map[string]any{{"role": "system", "content": "brief"}, {"role": "user", "content": "hello"}, {"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "hi"}}}},
		},
		{
			name:     "responses",
			protocol: "openai.responses",
			request:  `{"input":"hello"}`,
			response: `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}`,
			expected: []map[string]any{{"role": "user", "content": "hello"}, {"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "hi"}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := normalize(tt.protocol, []byte(tt.request), []byte(tt.response))
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, tt.expected, records[0].Messages)
		})
	}
}

func TestRunExportsEncryptedCaptureAsJSONL(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "offline-decrypt-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })
	t.Setenv("CRYPTO_SECRET", common.CryptoSecret)

	captureRoot := t.TempDir()
	captureDir := filepath.Join(captureRoot, "2026", "07", "22", "channel-1", "capture-fixture")
	require.NoError(t, os.MkdirAll(captureDir, 0o700))
	writeFixture(t, captureDir)
	output := filepath.Join(t.TempDir(), "conversations.jsonl")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	require.Zero(t, run([]string{"--capture-dir", captureRoot, "--output", output}, &stdout, &stderr), stderr.String())
	require.Contains(t, stdout.String(), "exported 1 conversation")
	body, err := os.ReadFile(output)
	require.NoError(t, err)
	var record conversation
	require.NoError(t, common.Unmarshal(bytes.TrimSpace(body), &record))
	require.Equal(t, []map[string]any{{"role": "user", "content": "hello"}, {"role": "assistant", "content": "hi"}}, record.Messages)
	info, err := os.Stat(output)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestRunFailsWithDifferentSecret(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "correct-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })
	t.Setenv("CRYPTO_SECRET", "wrong-secret")
	captureDir := t.TempDir()
	writeFixture(t, captureDir)
	output := filepath.Join(t.TempDir(), "conversations.jsonl")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	require.Equal(t, 1, run([]string{"--capture-dir", captureDir, "--output", output}, &stdout, &stderr))
	require.Contains(t, stderr.String(), "no complete conversations were exported")
}

func writeFixture(t *testing.T, directory string) {
	t.Helper()
	request, err := common.EncryptSecret(`{"messages":[{"role":"user","content":"hello"}]}`, relayCapturePurpose)
	require.NoError(t, err)
	response, err := common.EncryptSecret(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`, relayCapturePurpose)
	require.NoError(t, err)
	manifest, err := common.Marshal(captureManifest{ID: "capture-fixture", Protocol: "openai.chat_completions", Request: capturePart{Stored: true}, Response: capturePart{Stored: true}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "manifest.json"), manifest, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "request.enc"), []byte(request), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "response.enc"), []byte(response), 0o600))
}
