package sora

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultDoneWithVideoURL(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"model":"grok-image-video",
		"status":"done",
		"progress":100,
		"result_url":"https://example.com/result.mp4",
		"video":{"url":"https://example.com/video.mp4"},
		"output":["https://example.com/output.mp4"]
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/result.mp4", info.Url)
}

func TestExtractResponseTaskVideoURLFallbacks(t *testing.T) {
	require.Equal(t, "https://example.com/video.mp4", extractResponseTaskVideoURL(responseTask{Video: &struct {
		URL string `json:"url,omitempty"`
	}{URL: "https://example.com/video.mp4"}}))
	require.Equal(t, "https://example.com/output.mp4", extractResponseTaskVideoURL(responseTask{Output: []any{"https://example.com/output.mp4"}}))
	require.Equal(t, "https://example.com/object.mp4", extractResponseTaskVideoURL(responseTask{Output: map[string]any{"url": "https://example.com/object.mp4"}}))
}

func TestParseTaskResultAcceptsObjectOutput(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"model":"grok-image-video",
		"status":"done",
		"progress":100,
		"output":{"url":"https://example.com/object-output.mp4"}
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/object-output.mp4", info.Url)
}

func TestConvertToOpenAIVideoPromotesMetadataURLToSoraResponseShape(t *testing.T) {
	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.test"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1782570791,
		UpdatedAt: 1782571022,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "task_upstream",
		},
		Properties: model.Properties{
			OriginModelName: "sd-bak-2",
		},
		Data: []byte(`{
			"id":"task_upstream",
			"object":"video",
			"model":"sd-bak-2",
			"status":"completed",
			"progress":100,
			"metadata":{
				"result_url":"https://example.com/video.mp4",
				"url":"https://example.com/video.mp4",
				"video_url":"https://example.com/video.mp4"
			}
		}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	require.Equal(t, "task_public", got["id"])
	require.Equal(t, "video", got["object"])
	require.Equal(t, "task_upstream", got["task_id"])
	require.Equal(t, "sd-bak-2", got["model"])
	require.Equal(t, "completed", got["status"])
	require.Equal(t, "https://api.example.test/v1/videos/task_public/content", got["result_url"])
	require.Equal(t, "https://api.example.test/v1/videos/task_public/content", got["url"])
	require.Equal(t, "https://api.example.test/v1/videos/task_public/content", got["video_url"])
	require.Equal(t, []any{"https://api.example.test/v1/videos/task_public/content"}, got["output"])

	video, ok := got["video"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://api.example.test/v1/videos/task_public/content", video["url"])

	metadata, ok := got["metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://api.example.test/v1/videos/task_public/content", metadata["url"])
	require.Equal(t, []any{"https://api.example.test/v1/videos/task_public/content"}, metadata["result_urls"])
}

func TestNormalizeVideoSeconds(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "int", in: 15, want: "15"},
		{name: "float", in: float64(15), want: "15"},
		{name: "string", in: "15", want: "15"},
		{name: "string seconds suffix", in: "15s", want: "15"},
		{name: "string word suffix", in: "15 seconds", want: "15"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeVideoSeconds(tt.in)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeVideoSecondsFromFormUsesDurationFallback(t *testing.T) {
	require.Equal(t, "15", normalizeVideoSecondsFromForm(map[string][]string{"duration": {"15s"}}))
	require.Equal(t, "10", normalizeVideoSecondsFromForm(map[string][]string{"seconds": {"10s"}, "duration": {"15s"}}))
}

func TestBuildRequestBodyClampsCanvasStandardDuration(t *testing.T) {
	tests := []struct {
		name             string
		upstreamModel    string
		body             string
		expectedDuration float64
		expectedSeconds  string
	}{
		{
			name:             "target model clamps duration and seconds",
			upstreamModel:    canvasStandardSeedanceModel,
			body:             `{"model":"alias","prompt":"test","duration":20,"seconds":"18s"}`,
			expectedDuration: 14,
			expectedSeconds:  "14",
		},
		{
			name:             "target model preserves boundary",
			upstreamModel:    canvasStandardSeedanceModel,
			body:             `{"model":"alias","prompt":"test","duration":14}`,
			expectedDuration: 14,
			expectedSeconds:  "14",
		},
		{
			name:             "target model clamps fractional duration above boundary",
			upstreamModel:    canvasStandardSeedanceModel,
			body:             `{"model":"alias","prompt":"test","duration":14.5}`,
			expectedDuration: 14,
			expectedSeconds:  "14",
		},
		{
			name:             "other models are not clamped",
			upstreamModel:    "other-video-model",
			body:             `{"model":"alias","prompt":"test","duration":20}`,
			expectedDuration: 20,
			expectedSeconds:  "20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tt.upstreamModel},
			})
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			require.Equal(t, tt.upstreamModel, got["model"])
			require.Equal(t, tt.expectedDuration, got["duration"])
			require.Equal(t, tt.expectedSeconds, got["seconds"])
		})
	}
}

func TestBuildRequestBodyClampsCanvasStandardMultipartDuration(t *testing.T) {
	var input bytes.Buffer
	inputWriter := multipart.NewWriter(&input)
	require.NoError(t, inputWriter.WriteField("model", "alias"))
	require.NoError(t, inputWriter.WriteField("prompt", "test"))
	require.NoError(t, inputWriter.WriteField("duration", "20s"))
	require.NoError(t, inputWriter.WriteField("seconds", "18"))
	require.NoError(t, inputWriter.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", inputWriter.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: canvasStandardSeedanceModel},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	require.Equal(t, []string{canvasStandardSeedanceModel}, form.Value["model"])
	require.Equal(t, []string{"14"}, form.Value["seconds"])
	require.Equal(t, []string{"14"}, form.Value["duration"])
}

func TestEstimateBillingClampsCanvasStandardDuration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{Duration: 20})
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: canvasStandardSeedanceModel},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)

	require.Equal(t, float64(canvasStandardMaxVideoSeconds), ratios["seconds"])
}

func TestApplyVeoReferenceImagesUsesIngredientsForMoreThanTwoImages(t *testing.T) {
	body := map[string]any{
		"images": []any{
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png",
			"https://example.com/4.png",
		},
	}

	applyVeoReferenceImages(body)

	require.NotContains(t, body, "images")
	require.Equal(t, []string{
		"https://example.com/1.png",
		"https://example.com/2.png",
		"https://example.com/3.png",
		"https://example.com/4.png",
	}, body["Ingredients_images"])
}

func TestApplyVeoReferenceImagesUsesImagesForAtMostTwoImages(t *testing.T) {
	body := map[string]any{
		"Ingredients_images": []any{
			"https://example.com/1.png",
			"https://example.com/2.png",
		},
	}

	applyVeoReferenceImages(body)

	require.NotContains(t, body, "Ingredients_images")
	require.Equal(t, []string{
		"https://example.com/1.png",
		"https://example.com/2.png",
	}, body["images"])
}

func TestEstimateVideoSecondsUsesSeedanceGatewayMetadataDuration(t *testing.T) {
	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{
		Model:    "seedance-gateway",
		Metadata: map[string]any{"duration": "15"},
	}, &relaycommon.RelayInfo{OriginModelName: "seedance-gateway"})

	require.Equal(t, 15, seconds)
}

func TestEstimateVideoSecondsSeedanceGatewayDefaultsToFifteen(t *testing.T) {
	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{Model: "seedance-gateway"}, nil)

	require.Equal(t, 15, seconds)
}

func TestModelListIncludesSeedanceGateway(t *testing.T) {
	require.Contains(t, (&TaskAdaptor{}).GetModelList(), "seedance-gateway")
}

func TestParseTaskResultAcceptsStringError(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"model":"seedance-gateway",
		"status":"failed",
		"progress":0,
		"error":"生成失败，请稍后重试"
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Equal(t, "生成失败，请稍后重试", info.Reason)
}

func TestParseTaskResultAcceptsObjectError(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"model":"seedance-gateway",
		"status":"failed",
		"progress":0,
		"error":{"message":"生成失败","code":"upstream_failed"}
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Equal(t, "生成失败", info.Reason)
}

func TestParseTaskResultAcceptsCamelCaseVideoURL(t *testing.T) {
	body := []byte(`{
		"taskId":"task_upstream",
		"status":"completed",
		"progress":100,
		"videoUrl":"https://example.com/video.mp4",
		"output_url":"https://example.com/output.mp4",
		"error":""
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/video.mp4", info.Url)
}

func TestDoResponseAcceptsQueuedTaskWithEmptyStringError(t *testing.T) {
	body := `{
		"ok":true,
		"task":{
			"id":"cc363ea4-bd1c-4c93-9aaa-a66ce0ad6ebf",
			"navosTaskId":"",
			"status":"queued",
			"progress":1,
			"stage":"queued for submit",
			"prompt":"test",
			"createdAt":"2026-07-15T04:30:37.190Z"
		},
		"id":"cc363ea4-bd1c-4c93-9aaa-a66ce0ad6ebf",
		"taskId":"cc363ea4-bd1c-4c93-9aaa-a66ce0ad6ebf",
		"status":"queued",
		"progress":1,
		"videoUrl":"",
		"output_url":"",
		"error":"",
		"task_id":"cc363ea4-bd1c-4c93-9aaa-a66ce0ad6ebf"
	}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	})

	require.Nil(t, taskErr)
	require.Equal(t, "cc363ea4-bd1c-4c93-9aaa-a66ce0ad6ebf", upstreamID)
	require.JSONEq(t, body, string(taskData))
	require.Equal(t, http.StatusOK, recorder.Code)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "task_public", response["id"])
	require.Equal(t, "task_public", response["task_id"])
	require.Equal(t, "task_public", response["taskId"])
	require.Equal(t, "queued", response["status"])
	require.Equal(t, "", response["error"])
}

func TestApplyOtoySeedanceMiniReferenceRequest(t *testing.T) {
	body := map[string]any{
		"model":           "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
		"prompt":          "make a video",
		"duration":        float64(15),
		"seconds":         "15",
		"ratio":           "9:16",
		"resolution":      "720p",
		"functionMode":    "omni_reference",
		"response_format": "url",
		"file_paths": []any{
			"https://example.com/ref-from-file-path.png",
		},
		"images": []any{
			"https://example.com/ref.png",
		},
	}

	applyOtoySeedanceMiniReferenceRequest(body)

	require.NotContains(t, body, "seconds")
	require.NotContains(t, body, "images")
	require.NotContains(t, body, "file_paths")
	require.NotContains(t, body, "functionMode")
	require.NotContains(t, body, "ratio")
	require.NotContains(t, body, "response_format")
	require.Equal(t, "15", body["duration"])
	require.Equal(t, "9:16", body["aspect_ratio"])
	require.Equal(t, []string{"https://example.com/ref.png", "https://example.com/ref-from-file-path.png"}, body["image_urls"])
	require.Equal(t, "image-to-video", body["type"])
	require.Equal(t, true, body["generate_audio"])
}

func TestWriteOtoySeedanceMiniReferenceMultipartFields(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writeOtoySeedanceMiniReferenceMultipartFields(writer, map[string][]string{
		"prompt":          {"make a video"},
		"type":            {"custom-image-to-video"},
		"duration":        {"15"},
		"seconds":         {"15"},
		"ratio":           {"9:16"},
		"resolution":      {"720p"},
		"functionMode":    {"omni_reference"},
		"response_format": {"url"},
		"file_paths":      {"https://example.com/ref-from-file-path.png"},
		"image_urls":      {"https://example.com/ref.png"},
	})
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(buf.Bytes()), writer.Boundary())
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)

	require.Equal(t, []string{"make a video"}, form.Value["prompt"])
	require.Equal(t, []string{"custom-image-to-video"}, form.Value["type"])
	require.Equal(t, []string{"15"}, form.Value["duration"])
	require.Equal(t, []string{"9:16"}, form.Value["aspect_ratio"])
	require.Equal(t, []string{"720p"}, form.Value["resolution"])
	require.Equal(t, []string{"true"}, form.Value["generate_audio"])
	require.Equal(t, []string{"https://example.com/ref.png", "https://example.com/ref-from-file-path.png"}, form.Value["image_urls"])
	require.NotContains(t, form.Value, "seconds")
	require.NotContains(t, form.Value, "ratio")
	require.NotContains(t, form.Value, "functionMode")
	require.NotContains(t, form.Value, "response_format")
	require.NotContains(t, form.Value, "file_paths")
}

func TestWriteOtoySeedanceMiniReferenceMultipartFieldsDefaultsType(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writeOtoySeedanceMiniReferenceMultipartFields(writer, map[string][]string{
		"prompt": {"make a video"},
	})
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(buf.Bytes()), writer.Boundary())
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)

	require.Equal(t, []string{"image-to-video"}, form.Value["type"])
	require.Equal(t, []string{"true"}, form.Value["generate_audio"])
}

func TestNormalizeVideoDurationStringAllowsAuto(t *testing.T) {
	got, ok := normalizeVideoDurationString("auto")
	require.True(t, ok)
	require.Equal(t, "auto", got)
}

func TestParseTaskResultTreatsDetailErrorAsFailure(t *testing.T) {
	body := []byte(`{"detail":"{'message': '服务器内部错误: Invalid JSON response (502)', 'type': 'server_error'}","id":"task_upstream"}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Contains(t, info.Reason, "Invalid JSON response")
}
