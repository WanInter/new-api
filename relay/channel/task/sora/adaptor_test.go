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
	"github.com/stretchr/testify/assert"
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
			OriginModelName:   "sd-bak-2",
			UpstreamModelName: "internal-sdquan-model",
		},
		Data: []byte(`{
			"id":"task_upstream",
			"object":"video",
			"model":"internal-sdquan-model",
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

func TestMapDurationToSoraSecondsPreservesValues(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want any
	}{
		{name: "integer duration", body: map[string]any{"duration": 15}, want: "15"},
		{name: "fractional duration", body: map[string]any{"duration": 14.5}, want: "14.5"},
		{name: "explicit zero duration", body: map[string]any{"duration": 0}, want: "0"},
		{name: "duration takes precedence over seconds", body: map[string]any{"duration": 15, "seconds": 0}, want: "15"},
		{name: "seconds string is not rewritten", body: map[string]any{"seconds": "15s"}, want: "15s"},
		{name: "invalid duration is forwarded for upstream validation", body: map[string]any{"duration": map[string]any{"invalid": true}}, want: map[string]any{"invalid": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapDurationToSoraSeconds(tt.body)
			require.Equal(t, tt.want, tt.body["seconds"])
			assert.NotContains(t, tt.body, "duration")
		})
	}
}

func TestBuildRequestBodyPreservesCanvasStandardDuration(t *testing.T) {
	tests := []struct {
		name            string
		upstreamModel   string
		body            string
		expectedSeconds any
	}{
		{
			name:            "target model maps canonical duration",
			upstreamModel:   canvasStandardSeedanceModel,
			body:            `{"model":"alias","prompt":"test","duration":20,"seconds":"18s"}`,
			expectedSeconds: "20",
		},
		{
			name:            "target model preserves duration",
			upstreamModel:   canvasStandardSeedanceModel,
			body:            `{"model":"alias","prompt":"test","duration":14}`,
			expectedSeconds: "14",
		},
		{
			name:            "target model does not clamp fractional duration",
			upstreamModel:   canvasStandardSeedanceModel,
			body:            `{"model":"alias","prompt":"test","duration":14.5}`,
			expectedSeconds: "14.5",
		},
		{
			name:            "other models are not clamped",
			upstreamModel:   "other-video-model",
			body:            `{"model":"alias","prompt":"test","duration":20}`,
			expectedSeconds: "20",
		},
		{
			name:            "target model preserves explicit zero duration",
			upstreamModel:   canvasStandardSeedanceModel,
			body:            `{"model":"alias","prompt":"test","duration":0}`,
			expectedSeconds: "0",
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
			require.Equal(t, tt.expectedSeconds, got["seconds"])
			assert.NotContains(t, got, "duration")
		})
	}
}

func TestBuildRequestBodyPreservesCanvasStandardMultipartDuration(t *testing.T) {
	var input bytes.Buffer
	inputWriter := multipart.NewWriter(&input)
	require.NoError(t, inputWriter.WriteField("model", "alias"))
	require.NoError(t, inputWriter.WriteField("prompt", "test"))
	require.NoError(t, inputWriter.WriteField("duration", "20s"))
	require.NoError(t, inputWriter.WriteField("seconds", "18.5s"))
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
	require.Equal(t, []string{"20s"}, form.Value["seconds"])
	require.NotContains(t, form.Value, "duration")
}

func TestBuildRequestBodyMapsMultipartDurationAliasWithoutChangingValues(t *testing.T) {
	var input bytes.Buffer
	inputWriter := multipart.NewWriter(&input)
	require.NoError(t, inputWriter.WriteField("model", "alias"))
	require.NoError(t, inputWriter.WriteField("prompt", "test"))
	require.NoError(t, inputWriter.WriteField("duration", "0"))
	require.NoError(t, inputWriter.WriteField("duration", "14.5s"))
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
	require.Equal(t, []string{"0", "14.5s"}, form.Value["seconds"])
	require.NotContains(t, form.Value, "duration")
}

func TestEstimateBillingPreservesCanvasStandardDuration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{Duration: 20, Seconds: "4"})
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: canvasStandardSeedanceModel},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)

	require.Equal(t, float64(20), ratios["seconds"])
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

func TestApplyVeoReferenceImagesPreservesRepeatedURLs(t *testing.T) {
	body := map[string]any{
		"images": []any{
			"https://example.com/repeated.png",
			"https://example.com/repeated.png",
			"https://example.com/other.png",
		},
	}

	applyVeoReferenceImages(body)

	require.Equal(t, []string{
		"https://example.com/repeated.png",
		"https://example.com/repeated.png",
		"https://example.com/other.png",
	}, body["Ingredients_images"])
}

func TestEstimateVideoSecondsUsesSeedanceGatewayMetadataDuration(t *testing.T) {
	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{
		Model:    "seedance-gateway",
		Metadata: map[string]any{"duration": "15"},
	}, &relaycommon.RelayInfo{OriginModelName: "seedance-gateway"})

	require.Equal(t, 15, seconds)
}

func TestEstimateVideoSecondsUsesGenericDefault(t *testing.T) {
	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{Model: "seedance-gateway"}, nil)

	require.Equal(t, defaultUnprofiledVideoSeconds, seconds)
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
		OriginModelName: axMultimodalVideoModel,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
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
	require.Equal(t, axMultimodalVideoModel, response["model"])
	require.Equal(t, "queued", response["status"])
	require.Equal(t, "", response["error"])
}

func TestDoResponseHidesMappedUpstreamModel(t *testing.T) {
	body := `{"id":"upstream_task","model":"sdquan-2","status":"queued"}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{
		OriginModelName: "public-video-alias",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: sdquanImageVideoModel},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	})

	require.Nil(t, taskErr)
	require.Equal(t, "upstream_task", upstreamID)
	require.JSONEq(t, body, string(taskData))
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "public-video-alias", response["model"])
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
		"metadata":        map[string]any{"provider_option": true},
		"file_paths": []any{
			"https://example.com/ref-from-file-path.png",
		},
		"images": []any{
			"https://example.com/ref.png",
		},
		"videos": []any{
			"https://example.com/reference.mp4",
		},
		"audios": []any{
			"https://example.com/reference.mp3",
		},
	}

	applyOtoySeedanceMiniReferenceRequest(body)

	require.NotContains(t, body, "seconds")
	require.NotContains(t, body, "images")
	require.NotContains(t, body, "videos")
	require.NotContains(t, body, "audios")
	require.Contains(t, body, "file_paths")
	require.Contains(t, body, "functionMode")
	require.Contains(t, body, "ratio")
	require.Contains(t, body, "response_format")
	require.Equal(t, map[string]any{"provider_option": true}, body["metadata"])
	require.Equal(t, float64(15), body["duration"])
	require.Equal(t, "720p", body["resolution"])
	require.Equal(t, []string{"https://example.com/ref.png"}, body["image_urls"])
	require.Equal(t, []string{"https://example.com/reference.mp4"}, body["video_urls"])
	require.Equal(t, []string{"https://example.com/reference.mp3"}, body["audio_urls"])
	require.NotContains(t, body, "type")
	require.NotContains(t, body, "generate_audio")
}

func TestApplyOtoySeedanceMiniReferenceRequestMapsDocumentedSize(t *testing.T) {
	body := map[string]any{
		"size": "720x1280",
	}

	applyOtoySeedanceMiniReferenceRequest(body)

	require.Equal(t, "9:16", body["aspect_ratio"])
	require.Equal(t, "720p", body["resolution"])
	require.NotContains(t, body, "size")
}

func TestApplyOtoySeedanceMiniReferenceRequestPreservesRepeatedURLs(t *testing.T) {
	body := map[string]any{
		"images": []any{
			"https://example.com/repeated.png",
			"https://example.com/repeated.png",
		},
	}

	applyOtoySeedanceMiniReferenceRequest(body)

	require.Equal(t, []string{
		"https://example.com/repeated.png",
		"https://example.com/repeated.png",
	}, body["image_urls"])
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
		"metadata":        {`{"provider_option":true}`},
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
	require.Equal(t, []string{"720p"}, form.Value["resolution"])
	require.Equal(t, []string{"https://example.com/ref.png"}, form.Value["image_urls"])
	require.NotContains(t, form.Value, "seconds")
	require.NotContains(t, form.Value, "aspect_ratio")
	require.NotContains(t, form.Value, "generate_audio")
	require.Equal(t, []string{"9:16"}, form.Value["ratio"])
	require.Equal(t, []string{"omni_reference"}, form.Value["functionMode"])
	require.Equal(t, []string{"url"}, form.Value["response_format"])
	require.Equal(t, []string{`{"provider_option":true}`}, form.Value["metadata"])
	require.Equal(t, []string{"https://example.com/ref-from-file-path.png"}, form.Value["file_paths"])
}

func TestWriteOtoySeedanceMiniReferenceMultipartFieldsDoesNotAddDefaults(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writeOtoySeedanceMiniReferenceMultipartFields(writer, map[string][]string{
		"prompt": {"make a video"},
	})
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(buf.Bytes()), writer.Boundary())
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)

	require.NotContains(t, form.Value, "type")
	require.NotContains(t, form.Value, "generate_audio")
}

func TestWriteOtoySeedanceMiniReferenceMultipartFieldsMapsSecondsWithoutChangingValues(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writeOtoySeedanceMiniReferenceMultipartFields(writer, map[string][]string{
		"seconds": {"0", "14.5s"},
	})
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(buf.Bytes()), writer.Boundary())
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)

	require.Equal(t, []string{"0", "14.5s"}, form.Value["duration"])
	require.NotContains(t, form.Value, "seconds")
}

func TestApplyOtoySeedanceMiniReferenceRequestMapsZeroSecondsWithoutChangingIt(t *testing.T) {
	body := map[string]any{"seconds": float64(0)}

	applyOtoySeedanceMiniReferenceRequest(body)

	require.Equal(t, float64(0), body["duration"])
	require.NotContains(t, body, "seconds")
}

func TestParseTaskResultTreatsDetailErrorAsFailure(t *testing.T) {
	body := []byte(`{"detail":"{'message': '服务器内部错误: Invalid JSON response (502)', 'type': 'server_error'}","id":"task_upstream"}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Contains(t, info.Reason, "Invalid JSON response")
}
