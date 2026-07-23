package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIVideoResultURLs(t *testing.T) {
	testCases := []struct {
		name string
		body string
		url  string
	}{
		{
			name: "sora top level result URL wins",
			body: `{"id":"task_sora","result_url":"https://example.com/sora.mp4","url":"https://example.com/legacy.mp4"}`,
			url:  "https://example.com/sora.mp4",
		},
		{
			name: "aggc top level video URL",
			body: `{"id":"task_aggc","video_url":"https://example.com/aggc.mp4"}`,
			url:  "https://example.com/aggc.mp4",
		},
		{
			name: "yobox metadata URL",
			body: `{"id":"task_yobox","metadata":{"provider":"yobox","result_url":"https://example.com/yobox.mp4"}}`,
			url:  "https://example.com/yobox.mp4",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, err := normalizeOpenAIVideoResultURLs([]byte(testCase.body))

			require.NoError(t, err)
			var response map[string]any
			require.NoError(t, common.Unmarshal(normalized, &response))
			assert.Equal(t, testCase.url, response["result_url"])
			assert.Equal(t, testCase.url, response["url"])
			assert.Equal(t, testCase.url, response["video_url"])
			metadata, ok := response["metadata"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, testCase.url, metadata["result_url"])
			assert.Equal(t, testCase.url, metadata["url"])
			assert.Equal(t, testCase.url, metadata["video_url"])
			if testCase.name == "yobox metadata URL" {
				assert.Equal(t, "yobox", metadata["provider"])
			}
		})
	}
}

func TestNormalizeOpenAIVideoResultURLsLeavesResponsesWithoutMediaUntouched(t *testing.T) {
	body := []byte(`{"id":"task_pending","status":"queued"}`)

	normalized, err := normalizeOpenAIVideoResultURLs(body)

	require.NoError(t, err)
	assert.Equal(t, body, normalized)
}
