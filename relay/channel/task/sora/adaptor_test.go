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
	"github.com/QuantumNous/new-api/constant"
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

func TestBuildRequestBodyNormalizesMultipartVideoOutputAndMetadata(t *testing.T) {
	var input bytes.Buffer
	inputWriter := multipart.NewWriter(&input)
	require.NoError(t, inputWriter.WriteField("model", "public-video-model"))
	require.NoError(t, inputWriter.WriteField("prompt", "make it cinematic"))
	require.NoError(t, inputWriter.WriteField("ratio", "32:18"))
	require.NoError(t, inputWriter.WriteField("aspectRatio", "16:9"))
	require.NoError(t, inputWriter.WriteField("size", "0960X0540"))
	require.NoError(t, inputWriter.WriteField("resolution", "1080P"))
	require.NoError(t, inputWriter.WriteField("metadata", `{
		"ratio":"9:16",
		"aspectRatio":"9:16",
		"aspect_ratio":"9:16",
		"size":"720x1280",
		"resolution":"720P",
		"provider_option":{"keep":true}
	}`))
	require.NoError(t, inputWriter.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", inputWriter.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "sora-upstream-model"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()

	require.Equal(t, []string{"sora-upstream-model"}, form.Value["model"])
	assert.Equal(t, []string{"960x540"}, form.Value["size"])
	assert.Equal(t, []string{"16:9"}, form.Value["aspect_ratio"])
	assert.Equal(t, []string{"16:9"}, form.Value["ratio"])
	assert.Equal(t, []string{"16:9"}, form.Value["aspectRatio"])
	assert.Equal(t, []string{"1080p"}, form.Value["resolution"])

	var metadata map[string]any
	require.NoError(t, common.UnmarshalJsonStr(form.Value["metadata"][0], &metadata))
	assert.Equal(t, "960x540", metadata["size"])
	assert.Equal(t, "16:9", metadata["aspect_ratio"])
	assert.Equal(t, "1080p", metadata["resolution"])
	assert.NotContains(t, metadata, "ratio")
	assert.NotContains(t, metadata, "aspectRatio")
	assert.Equal(t, map[string]any{"keep": true}, metadata["provider_option"])
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

func TestEstimateBillingPreservesGenericFractionalWireDuration(t *testing.T) {
	tests := []struct {
		name        string
		newRequest  func(t *testing.T) *http.Request
		wantSeconds float64
		assertWire  func(t *testing.T, data []byte, contentType string)
	}{
		{
			name: "json duration takes precedence",
			newRequest: func(t *testing.T) *http.Request {
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"generic-video","prompt":"animate","duration":5.5,"seconds":"9"}`))
				request.Header.Set("Content-Type", "application/json")
				return request
			},
			wantSeconds: 5.5,
			assertWire: func(t *testing.T, data []byte, _ string) {
				var body map[string]interface{}
				require.NoError(t, common.Unmarshal(data, &body))
				assert.Equal(t, "5.5", body["seconds"])
				assert.NotContains(t, body, "duration")
			},
		},
		{
			name: "multipart seconds alias",
			newRequest: func(t *testing.T) *http.Request {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("model", "generic-video"))
				require.NoError(t, writer.WriteField("prompt", "animate"))
				require.NoError(t, writer.WriteField("seconds", "5.5"))
				require.NoError(t, writer.Close())
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
				request.Header.Set("Content-Type", writer.FormDataContentType())
				return request
			},
			wantSeconds: 5.5,
			assertWire: func(t *testing.T, data []byte, contentType string) {
				_, params, err := mime.ParseMediaType(contentType)
				require.NoError(t, err)
				form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
				require.NoError(t, err)
				defer form.RemoveAll()
				assert.Equal(t, []string{"5.5"}, form.Value["seconds"])
			},
		},
		{
			name: "json invalid duration does not fall back to seconds",
			newRequest: func(t *testing.T) *http.Request {
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"generic-video","prompt":"animate","duration":0,"seconds":"15"}`))
				request.Header.Set("Content-Type", "application/json")
				return request
			},
			wantSeconds: float64(defaultUnprofiledVideoSeconds),
			assertWire: func(t *testing.T, data []byte, _ string) {
				var body map[string]interface{}
				require.NoError(t, common.Unmarshal(data, &body))
				assert.Equal(t, "0", body["seconds"])
				assert.NotContains(t, body, "duration")
			},
		},
		{
			name: "multipart repeated duration does not fall back to seconds",
			newRequest: func(t *testing.T) *http.Request {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("model", "generic-video"))
				require.NoError(t, writer.WriteField("prompt", "animate"))
				require.NoError(t, writer.WriteField("duration", "0"))
				require.NoError(t, writer.WriteField("duration", "invalid"))
				require.NoError(t, writer.WriteField("seconds", "15"))
				require.NoError(t, writer.Close())
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
				request.Header.Set("Content-Type", writer.FormDataContentType())
				return request
			},
			wantSeconds: float64(defaultUnprofiledVideoSeconds),
			assertWire: func(t *testing.T, data []byte, contentType string) {
				_, params, err := mime.ParseMediaType(contentType)
				require.NoError(t, err)
				form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
				require.NoError(t, err)
				defer form.RemoveAll()
				assert.Equal(t, []string{"0", "invalid"}, form.Value["seconds"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = tt.newRequest(t)
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "generic-video"},
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			}
			adaptor := &TaskAdaptor{}

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			ratio := adaptor.EstimateBilling(c, info)
			assert.Equal(t, tt.wantSeconds, ratio["seconds"])

			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)
			tt.assertWire(t, data, c.GetHeader("Content-Type"))
		})
	}
}

func TestSeedanceGatewayMetadataDurationRemainsProviderWireInput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-gateway",
		"prompt":"animate",
		"metadata":{"duration":"15","resolution":"720p"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: seedanceGatewayModel},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, float64(15), adaptor.EstimateBilling(c, info)["seconds"])

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var upstream map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &upstream))
	assert.NotContains(t, upstream, "seconds")
	metadata, ok := upstream["metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "15", metadata["duration"])
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

func TestValidateRemixRequestNormalizesVideoOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_123/remix", strings.NewReader(`{
		"prompt":"make it cinematic",
		"ratio":"32:18",
		"resolution":"720P"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "sora-2"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionRemix},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "16:9", req.AspectRatio)
	assert.Equal(t, "720p", req.Resolution)

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, "16:9", payload["aspect_ratio"])
	assert.Equal(t, "16:9", payload["ratio"])
	assert.Equal(t, "720p", payload["resolution"])
}

func TestBuildRequestBodySynchronizesNestedParametersResolution(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"sora-2",
		"prompt":"make it cinematic",
		"ratio":"32:18",
		"resolution":"720P",
		"parameters":{"resolution":"1080P","duration":15},
		"metadata":{"ratio":"9:16","aspectRatio":"9:16","aspect_ratio":"9:16"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "sora-2"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(data, &payload))

	assert.Equal(t, "720p", payload["resolution"])
	parameters, ok := payload["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "720p", parameters["resolution"])
	assert.Equal(t, float64(15), parameters["duration"])
	metadata, ok := payload["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "16:9", metadata["aspect_ratio"])
	assert.NotContains(t, metadata, "ratio")
	assert.NotContains(t, metadata, "aspectRatio")
}

func TestBuildRequestBodyNormalizesEncodedMetadataVideoOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"sora-2",
		"prompt":"make it cinematic",
		"ratio":"32:18",
		"resolution":"720P",
		"metadata":"{\"ratio\":\"9:16\",\"aspectRatio\":\"9:16\",\"aspect_ratio\":\"9:16\"}"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "sora-2"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(data, &payload))
	rawMetadata, ok := payload["metadata"].(string)
	require.True(t, ok)
	var metadata map[string]any
	require.NoError(t, common.UnmarshalJsonStr(rawMetadata, &metadata))

	assert.Equal(t, "16:9", metadata["aspect_ratio"])
	assert.Equal(t, "720p", metadata["resolution"])
	assert.NotContains(t, metadata, "ratio")
	assert.NotContains(t, metadata, "aspectRatio")
}

func TestValidateRemixRequestRejectsConflictingVideoOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_123/remix", strings.NewReader(`{
		"prompt":"make it cinematic",
		"size":"960x540",
		"ratio":"9:16"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionRemix}})

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, "conflicts with aspect_ratio")
}

func TestParseTaskResultTreatsDetailErrorAsFailure(t *testing.T) {
	body := []byte(`{"detail":"{'message': '服务器内部错误: Invalid JSON response (502)', 'type': 'server_error'}","id":"task_upstream"}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Contains(t, info.Reason, "Invalid JSON response")
}
