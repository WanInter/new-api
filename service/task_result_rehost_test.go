package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestLoadTaskResultRehostConfigTencentCOSDefaults(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_BACKEND", "tencent_cos")
	t.Setenv("TASK_RESULT_REHOST_ENDPOINT", "")
	t.Setenv("TASK_RESULT_REHOST_UPLOAD_ENDPOINT", "")
	t.Setenv("TASK_RESULT_REHOST_BUCKET", "media-1250000000")
	t.Setenv("TASK_RESULT_REHOST_REGION", "ap-guangzhou")
	t.Setenv("TASK_RESULT_REHOST_PUBLIC_BASE_URL", "")

	cfg := loadTaskResultRehostConfig()

	require.Equal(t, taskResultRehostBackendTencentCOS, cfg.Backend)
	require.Equal(t, "https://cos.ap-guangzhou.myqcloud.com", cfg.Endpoint)
	require.Equal(t, "https://cos.ap-guangzhou.myqcloud.com", cfg.UploadEndpoint)
	require.Equal(t, "https://media-1250000000.cos.ap-guangzhou.myqcloud.com", cfg.PublicBaseURL)
}

func TestTaskResultRehostConfigValidatesTencentCOS(t *testing.T) {
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendTencentCOS,
		UploadEndpoint:  "https://cos.ap-guangzhou.myqcloud.com",
		Bucket:          "media-1250000000",
		Region:          "ap-guangzhou",
		PublicBaseURL:   "https://media-1250000000.cos.ap-guangzhou.myqcloud.com",
		AccessKeyID:     "secret-id",
		AccessKeySecret: "secret-key",
	}
	require.NoError(t, cfg.validate())
}

func TestTencentCOSUploadUsesVirtualHostedSignedRequest(t *testing.T) {
	var captured *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})}
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendTencentCOS,
		UploadEndpoint:  "https://cos.ap-guangzhou.myqcloud.com",
		Bucket:          "media-1250000000",
		Region:          "ap-guangzhou",
		AccessKeyID:     "secret-id",
		AccessKeySecret: "secret-key",
	}
	client := cfg.newObjectStorageClient(httpClient)

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      ptr.String(cfg.Bucket),
		Key:         ptr.String("generated/newapi/videos/result.mp4"),
		Body:        bytes.NewReader([]byte("video")),
		ContentType: ptr.String("video/mp4"),
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Equal(t, "media-1250000000.cos.ap-guangzhou.myqcloud.com", captured.URL.Host)
	require.Equal(t, "/generated/newapi/videos/result.mp4", captured.URL.Path)
	require.True(t, strings.HasPrefix(captured.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=secret-id/"))
}

func TestTaskResultRehostConfigRejectsUnsupportedBackend(t *testing.T) {
	cfg := taskResultRehostConfig{Backend: "unknown"}
	err := cfg.validate()
	require.EqualError(t, err, "unsupported task result rehost backend: unknown")
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

func TestImageTaskFailureDataDoesNotExposeBase64(t *testing.T) {
	data := imageTaskFailureData("upload failed")
	require.NotContains(t, string(data), "data:image")
	require.NotContains(t, string(data), "b64_json")
	require.Contains(t, string(data), "upload failed")
}
