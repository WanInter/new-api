package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTaskResultRehostEnabledForURL(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_ENABLED", "true")
	t.Setenv("TASK_RESULT_REHOST_DOMAINS", "vidgen.x.ai,example.com")
	require.True(t, TaskResultRehostEnabledForURL("https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"))
	require.True(t, TaskResultRehostEnabledForURL("https://sub.example.com/video.mp4"))
	require.False(t, TaskResultRehostEnabledForURL("https://cdn.example.net/video.mp4"))
}

func TestTaskResultRehostEnabledForDataURL(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_ENABLED", "false")
	require.True(t, TaskResultRehostEnabledForDataURL("data:image/png;base64,aW1hZ2U="))
	require.False(t, TaskResultRehostEnabledForDataURL("https://example.com/image.png"))
}

func TestDecodeRehostDataURL(t *testing.T) {
	body, contentType, ext, err := decodeRehostDataURL("data:image/png;base64,aW1hZ2U=", 1024)
	require.NoError(t, err)
	require.Equal(t, []byte("image"), body)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, "png", ext)
}

func TestDecodeRehostDataURLRejectsTooLargePayload(t *testing.T) {
	_, _, _, err := decodeRehostDataURL("data:image/png;base64,aW1hZ2U=", 4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
}

func TestTaskResultRehostDataURLUsesImagePrefixByDefault(t *testing.T) {
	cfg := taskResultRehostConfig{Prefix: defaultTaskResultRehostPrefix}
	cfg = cfg.withDataURLPrefix(&model.Task{
		TaskID:   "task_1",
		Platform: constant.TaskPlatformImage,
	})
	key := cfg.objectKey(&model.Task{TaskID: "task_1"}, "data:image/png;base64,aW1hZ2U=", "png")
	require.Contains(t, key, imageTaskResultRehostPrefix+"/")
}

func TestTaskResultRehostDataURLKeepsCustomPrefix(t *testing.T) {
	cfg := taskResultRehostConfig{Prefix: "custom/prefix"}
	cfg = cfg.withDataURLPrefix(&model.Task{
		TaskID:   "task_1",
		Platform: constant.TaskPlatformImage,
	})
	key := cfg.objectKey(&model.Task{TaskID: "task_1"}, "data:image/png;base64,aW1hZ2U=", "png")
	require.Contains(t, key, "custom/prefix/")
}

func TestReplaceRehostedURLInJSON(t *testing.T) {
	oldURL := "https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"
	newURL := "https://cdn.example.com/generated/video.mp4"
	body := []byte(`{"result_url":"https://vidgen.x.ai/xai-vidgen-bucket/video.mp4","output":["https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"],"video":{"url":"https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"}}`)

	updated := replaceRehostedURLInJSON(body, oldURL, newURL)
	require.NotContains(t, string(updated), oldURL)
	require.Contains(t, string(updated), newURL)
}

func TestReplaceRehostedImageDataURLInJSON(t *testing.T) {
	oldURL := "data:image/png;base64,aW1hZ2U="
	newURL := "https://cdn.example.com/generated/image.png"
	body := []byte(`{"status":"success","result_url":"data:image/png;base64,aW1hZ2U=","data":{"data":[{"b64_json":"aW1hZ2U="}]}}`)

	updated := replaceRehostedImageDataURLInJSON(body, oldURL, newURL)
	require.NotContains(t, string(updated), oldURL)
	require.NotContains(t, string(updated), "b64_json")
	require.Contains(t, string(updated), newURL)
}
