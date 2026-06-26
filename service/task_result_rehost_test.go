package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskResultRehostEnabledForURL(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_ENABLED", "true")
	t.Setenv("TASK_RESULT_REHOST_DOMAINS", "vidgen.x.ai,example.com")
	require.True(t, TaskResultRehostEnabledForURL("https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"))
	require.True(t, TaskResultRehostEnabledForURL("https://sub.example.com/video.mp4"))
	require.False(t, TaskResultRehostEnabledForURL("https://cdn.example.net/video.mp4"))
}

func TestReplaceRehostedURLInJSON(t *testing.T) {
	oldURL := "https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"
	newURL := "https://cdn.example.com/generated/video.mp4"
	body := []byte(`{"result_url":"https://vidgen.x.ai/xai-vidgen-bucket/video.mp4","output":["https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"],"video":{"url":"https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"}}`)

	updated := replaceRehostedURLInJSON(body, oldURL, newURL)
	require.NotContains(t, string(updated), oldURL)
	require.Contains(t, string(updated), newURL)
}
