package doubao

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestURLSelectsProtocolByModel(t *testing.T) {
	testCases := []struct {
		name     string
		model    string
		baseURL  string
		expected string
	}{
		{
			name:     "bytefor native video API",
			model:    "bytefor-2.0-real-priority",
			baseURL:  "https://k7q9m2x4a8z3.bytefor.com/",
			expected: "https://k7q9m2x4a8z3.bytefor.com/v1/videos/generations",
		},
		{
			name:     "bytefor model replaces doubao default base URL",
			model:    "bytefor-2.0-fast",
			baseURL:  "https://ark.cn-beijing.volces.com",
			expected: "https://k7q9m2x4a8z3.bytefor.com/v1/videos/generations",
		},
		{
			name:     "doubao ark compatible API",
			model:    "doubao-seedance-2-0-260128",
			baseURL:  "https://ark.example.com/",
			expected: "https://ark.example.com/api/v3/contents/generations/tasks",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{baseURL: testCase.baseURL}
			info := &relaycommon.RelayInfo{
				OriginModelName: testCase.model,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: testCase.model},
			}

			url, err := adaptor.BuildRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, url)
		})
	}
}

func TestBuildRequestBodyConvertsByteforNativeRequest(t *testing.T) {
	bodyJSON := `{
		"model":"public-bytefor",
		"prompt":"参考@图片1和@音频1生成视频",
		"size":"16:9",
		"resolution":"720p",
		"duration":"4s",
		"images":["https://example.com/person.png","https://example.com/voice.mp3","face:person-1"]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-bytefor",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "bytefor-2.0-real-priority",
			IsModelMapped:     true,
		},
	}
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload byteforRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, "bytefor-2.0-real-priority", payload.Model)
	assert.Equal(t, "参考@图片1和@音频1生成视频", payload.Prompt)
	assert.Equal(t, "16:9", payload.Size)
	assert.Equal(t, "720P", payload.Resolution)
	assert.Equal(t, "4s", payload.Duration)
	assert.Equal(t, []string{
		"https://example.com/person.png",
		"https://example.com/voice.mp3",
		"face:person-1",
	}, payload.Images)
}

func TestBuildRequestBodyPreservesTopLevelAspectRatio(t *testing.T) {
	bodyJSON := `{"model":"public-bytefor","prompt":"参考图片生成竖屏视频","aspect_ratio":"9:16","resolution":"720p","duration":15,"images":["https://example.com/reference.png"]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-bytefor",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "bytefor-2.0-real-priority",
		},
	}
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload byteforRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, "9:16", payload.Size)
	assert.Equal(t, "720P", payload.Resolution)
	assert.Equal(t, "15s", payload.Duration)
	assert.Equal(t, []string{"https://example.com/reference.png"}, payload.Images)
}

func TestCanonicalAspectRatioWinsLegacySizeForDoubaoPayloads(t *testing.T) {
	testCases := []struct {
		name     string
		request  relaycommon.TaskSubmitReq
		expected string
	}{
		{
			name:     "pixel size uses its canonical public aspect ratio",
			request:  relaycommon.TaskSubmitReq{Size: "960x540", AspectRatio: "16:9"},
			expected: "16:9",
		},
		{
			name:     "canonical aspect ratio wins legacy ratio-shaped size",
			request:  relaycommon.TaskSubmitReq{Size: "16:9", AspectRatio: "9:16"},
			expected: "9:16",
		},
		{
			name:     "legacy ratio-shaped size remains supported without canonical aspect ratio",
			request:  relaycommon.TaskSubmitReq{Size: "16:9"},
			expected: "16:9",
		},
		{
			name:     "pixel size does not fall back to metadata ratio",
			request:  relaycommon.TaskSubmitReq{Size: "960×540", Metadata: map[string]any{"ratio": "1:1"}},
			expected: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&testCase.request)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, payload.Ratio)
		})
	}
}

func TestByteforSizeUsesCanonicalAspectRatio(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "bytefor-2.0-fast",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "bytefor-2.0-fast"},
	}

	testCases := []struct {
		name     string
		request  relaycommon.TaskSubmitReq
		expected string
	}{
		{
			name:     "canonical aspect ratio wins legacy ratio-shaped size",
			request:  relaycommon.TaskSubmitReq{Size: "16:9", AspectRatio: "9:16"},
			expected: "9:16",
		},
		{
			name:     "pixel size is not sent as bytefor size without canonical aspect ratio",
			request:  relaycommon.TaskSubmitReq{Size: "960x540"},
			expected: "",
		},
		{
			name:     "pixel size uses its canonical public aspect ratio",
			request:  relaycommon.TaskSubmitReq{Size: "960x540", AspectRatio: "16:9"},
			expected: "16:9",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := convertToByteforRequestPayload(&testCase.request, info)
			assert.Equal(t, testCase.expected, payload.Size)
		})
	}
}

func TestMaxAPISeedanceBetaUsesVolcengineArkProtocol(t *testing.T) {
	bodyJSON := `{
		"model":"public-seedance-beta",
		"prompt":"Use the reference image as the character and the video as the motion",
		"aspect_ratio":"16:9",
		"resolution":"1080p",
		"duration":15,
		"metadata":{
			"generate_audio":true,
			"watermark":false,
			"tools":[{"type":"web_search"}],
			"content":[
				{"type":"image_url","image_url":{"url":"https://example.com/character.png"},"role":"reference_image"},
				{"type":"video_url","video_url":{"url":"https://example.com/motion.mp4"},"role":"reference_video"}
			]
		}
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance-beta",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "model/seedance-2.0-beta",
			IsModelMapped:     true,
		},
	}
	adaptor := &TaskAdaptor{baseURL: "https://api.maxapi.dev"}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.maxapi.dev/api/v3/contents/generations/tasks", requestURL)

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, "model/seedance-2.0-beta", payload["model"])
	assert.Equal(t, "16:9", payload["ratio"])
	assert.Equal(t, "1080p", payload["resolution"])
	assert.Equal(t, float64(15), payload["duration"])
	assert.Equal(t, true, payload["generate_audio"])
	assert.Equal(t, false, payload["watermark"])

	content, ok := payload["content"].([]interface{})
	require.True(t, ok)
	require.Len(t, content, 3)
	assert.Equal(t, map[string]interface{}{
		"type": "text",
		"text": "Use the reference image as the character and the video as the motion",
	}, content[2])
	assert.Equal(t, "reference_image", content[0].(map[string]interface{})["role"])
	assert.Equal(t, "reference_video", content[1].(map[string]interface{})["role"])
}

func TestBuildRequestBodyMapsUnifiedMediaForArk(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-seedance",
		"prompt":"combine every reference",
		"image_urls":"https://example.com/image-alias.png",
		"input_reference":"https://example.com/input-reference.png",
		"input":{"image_references":[{"url":"https://example.com/reference.png"}]},
		"videos":"https://example.com/direct-video.mp4",
		"audio_url":"https://example.com/direct-audio.mp3",
		"metadata":{"start_frames":"https://example.com/metadata-start.png"},
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/content-image.png"},"role":"reference_image","name":"one"},
			{"type":"video_url","video_url":{"url":"https://example.com/content-video.mp4"},"role":"reference_video","name":"two"},
			{"type":"audio_url","audio_url":{"url":"https://example.com/content-audio.mp3"},"role":"reference_audio","name":"three"}
		]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "doubao-seedance-2-0-260128",
			IsModelMapped:     true,
		},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload requestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Equal(t, "doubao-seedance-2-0-260128", payload.Model)
	require.Len(t, payload.Content, 10)
	assert.Equal(t, "https://example.com/image-alias.png", payload.Content[0].ImageURL.URL)
	assert.Equal(t, "https://example.com/input-reference.png", payload.Content[1].ImageURL.URL)
	assert.Equal(t, "https://example.com/reference.png", payload.Content[2].ImageURL.URL)
	assert.Equal(t, "https://example.com/metadata-start.png", payload.Content[3].ImageURL.URL)
	assert.Equal(t, "https://example.com/direct-video.mp4", payload.Content[4].VideoURL.URL)
	assert.Equal(t, "https://example.com/direct-audio.mp3", payload.Content[5].AudioURL.URL)
	assert.Equal(t, "reference_image", payload.Content[6].Role)
	assert.Equal(t, "one", payload.Content[6].Name)
	assert.Equal(t, "reference_video", payload.Content[7].Role)
	assert.Equal(t, "two", payload.Content[7].Name)
	assert.Equal(t, "reference_audio", payload.Content[8].Role)
	assert.Equal(t, "three", payload.Content[8].Name)
	assert.Equal(t, ContentItem{Type: "text", Text: "combine every reference"}, payload.Content[9])

	videoInputRatio, ok := GetVideoInputRatio("doubao-seedance-2-0-260128")
	require.True(t, ok)
	assert.Equal(t, map[string]float64{"video_input": videoInputRatio}, adaptor.EstimateBilling(c, info))
}

func TestConvertToRequestPayloadMapsDoubaoImageCompatibilityAliases(t *testing.T) {
	testCases := []struct {
		name    string
		request relaycommon.TaskSubmitReq
		url     string
	}{
		{name: "canonical images", request: relaycommon.TaskSubmitReq{Images: []string{"https://example.com/images.png"}}, url: "https://example.com/images.png"},
		{name: "single image", request: relaycommon.TaskSubmitReq{Image: "https://example.com/image.png"}, url: "https://example.com/image.png"},
		{name: "image URLs", request: relaycommon.TaskSubmitReq{ImageURLs: []string{"https://example.com/image-urls.png"}}, url: "https://example.com/image-urls.png"},
		{name: "input reference", request: relaycommon.TaskSubmitReq{InputReference: "https://example.com/input-reference.png"}, url: "https://example.com/input-reference.png"},
		{name: "input start frames", request: relaycommon.TaskSubmitReq{InputStartFrames: []string{"https://example.com/start-frame.png"}}, url: "https://example.com/start-frame.png"},
		{name: "input image references", request: relaycommon.TaskSubmitReq{InputImageReferences: []string{"https://example.com/image-reference.png"}}, url: "https://example.com/image-reference.png"},
		{name: "metadata start frames", request: relaycommon.TaskSubmitReq{MetadataStartFrames: []string{"https://example.com/metadata-start.png"}}, url: "https://example.com/metadata-start.png"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&testCase.request)

			require.NoError(t, err)
			require.NotEmpty(t, payload.Content)
			assert.Equal(t, testCase.url, payload.Content[0].ImageURL.URL)
		})
	}
}

func TestConvertByteforRequestPreservesAllMediaAliasesAndDuplicates(t *testing.T) {
	req := &relaycommon.TaskSubmitReq{
		Images:               []string{"duplicate", "duplicate"},
		ImageURLs:            []string{"image-alias"},
		InputStartFrames:     []string{"start-frame"},
		InputImageReferences: []string{"image-reference"},
		MetadataStartFrames:  []string{"metadata-start"},
		Audios:               []string{"audio"},
		AudioURLs:            []string{"audio-alias"},
		Videos:               []string{"video"},
		VideoURLs:            []string{"video-alias"},
		Content: []relaycommon.TaskContentItem{
			{Type: "image_url", ImageURL: &relaycommon.TaskContentURL{URL: "content-image"}},
			{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "content-video"}},
			{Type: "audio_url", AudioURL: &relaycommon.TaskContentURL{URL: "content-audio"}},
		},
		Image:          "single-image",
		InputReference: "input-reference",
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "bytefor-2.0-fast",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "bytefor-2.0-fast"},
	}

	payload := convertToByteforRequestPayload(req, info)
	assert.Equal(t, []string{
		"duplicate",
		"duplicate",
		"image-alias",
		"start-frame",
		"image-reference",
		"metadata-start",
		"audio",
		"audio-alias",
		"video",
		"video-alias",
		"content-image",
		"content-video",
		"content-audio",
		"single-image",
		"input-reference",
	}, payload.Images)
}

func TestEstimateBillingUsesByteforDurationSeconds(t *testing.T) {
	testCases := []struct {
		name     string
		request  relaycommon.TaskSubmitReq
		expected float64
	}{
		{name: "duration", request: relaycommon.TaskSubmitReq{Duration: 8}, expected: 8},
		{name: "seconds alias", request: relaycommon.TaskSubmitReq{Seconds: "12s"}, expected: 12},
		{name: "default", request: relaycommon.TaskSubmitReq{}, expected: 15},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("task_request", testCase.request)
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-bytefor",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "bytefor-2.0-fast",
					IsModelMapped:     true,
				},
			}

			ratios := (&TaskAdaptor{}).EstimateBilling(c, info)

			assert.Equal(t, map[string]float64{"seconds": testCase.expected}, ratios)
		})
	}
}

func TestByteforDurationDefaultsToFifteenSeconds(t *testing.T) {
	request := relaycommon.TaskSubmitReq{}

	assert.Equal(t, "15s", byteforDuration(&request))
}

func TestDoResponseAcceptsByteforTaskID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"created":1781690400,
			"model":"bytefor-2.0-real-priority",
			"task_id":"TASK-upstream",
			"status":"pending",
			"cost":2.5
		}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "bytefor-2.0-real-priority",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamTaskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "TASK-upstream", upstreamTaskID)

	var response dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "task_public", response.ID)
	assert.Equal(t, "task_public", response.TaskID)
	assert.Equal(t, dto.VideoStatusQueued, response.Status)
	assert.Equal(t, int64(1781690400), response.CreatedAt)
}

func TestParseTaskResultSupportsByteforAndDoubaoResponses(t *testing.T) {
	testCases := []struct {
		name             string
		body             string
		expectedStatus   model.TaskStatus
		expectedProgress string
		expectedURL      string
		expectedReason   string
	}{
		{
			name:             "bytefor pending",
			body:             `{"status":"pending","progress":12}`,
			expectedStatus:   model.TaskStatusQueued,
			expectedProgress: "12%",
		},
		{
			name:             "bytefor configuring",
			body:             `{"status":"configuring","progress":35,"progress_text":"configuring video"}`,
			expectedStatus:   model.TaskStatusInProgress,
			expectedProgress: "35%",
		},
		{
			name:             "bytefor generating",
			body:             `{"status":"generating","progress":60,"progress_text":"generating video"}`,
			expectedStatus:   model.TaskStatusInProgress,
			expectedProgress: "60%",
		},
		{
			name:             "bytefor completed data URL",
			body:             `{"status":"completed","progress":100,"data":[{"url":"https://cdn.example.com/video.mp4"}]}`,
			expectedStatus:   model.TaskStatusSuccess,
			expectedProgress: "100%",
			expectedURL:      "https://cdn.example.com/video.mp4",
		},
		{
			name:             "bytefor ark compatible object URL",
			body:             `{"status":"succeeded","content":{"video_url":{"url":"https://cdn.example.com/object.mp4"}}}`,
			expectedStatus:   model.TaskStatusSuccess,
			expectedProgress: "100%",
			expectedURL:      "https://cdn.example.com/object.mp4",
		},
		{
			name:             "doubao string URL",
			body:             `{"status":"succeeded","content":{"video_url":"https://cdn.example.com/string.mp4"}}`,
			expectedStatus:   model.TaskStatusSuccess,
			expectedProgress: "100%",
			expectedURL:      "https://cdn.example.com/string.mp4",
		},
		{
			name:             "failed",
			body:             `{"status":"failed","error":{"code":"upstream_failed","message":"generation failed"}}`,
			expectedStatus:   model.TaskStatusFailure,
			expectedProgress: "100%",
			expectedReason:   "generation failed",
		},
		{
			name:             "failed with string error",
			body:             `{"status":"failed","error":"generation rejected"}`,
			expectedStatus:   model.TaskStatusFailure,
			expectedProgress: "100%",
			expectedReason:   "generation rejected",
		},
		{
			name:             "bytefor failed with top-level error fields",
			body:             `{"status":"failed","progress":100,"error_code":"GENERATION_FAILED","error_msg":"视频生成失败，请检查素材是否清晰、可访问且符合要求，调整后重试","progress_text":"fallback progress text","queue_info":"fallback queue info"}`,
			expectedStatus:   model.TaskStatusFailure,
			expectedProgress: "100%",
			expectedReason:   "视频生成失败，请检查素材是否清晰、可访问且符合要求，调整后重试",
		},
		{
			name:             "bytefor failed falls back to progress text",
			body:             `{"status":"failed","error_code":"GENERATION_FAILED","progress_text":"generation failed during rendering","queue_info":"fallback queue info"}`,
			expectedStatus:   model.TaskStatusFailure,
			expectedProgress: "100%",
			expectedReason:   "generation failed during rendering",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(testCase.body))
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedStatus, model.TaskStatus(result.Status))
			assert.Equal(t, testCase.expectedProgress, result.Progress)
			assert.Equal(t, testCase.expectedURL, result.Url)
			assert.Equal(t, testCase.expectedReason, result.Reason)
		})
	}
}

func TestFetchTaskSelectsByteforQueryPath(t *testing.T) {
	testCases := []struct {
		name         string
		model        string
		expectedPath string
	}{
		{
			name:         "bytefor",
			model:        "bytefor-2.0-fast",
			expectedPath: "/v1/videos/generations/TASK-123",
		},
		{
			name:         "doubao",
			model:        "doubao-seedance-2-0-fast-260128",
			expectedPath: "/api/v3/contents/generations/tasks/TASK-123",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, testCase.expectedPath, r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"pending"}`))
			}))
			defer server.Close()

			resp, err := (&TaskAdaptor{}).FetchTask(context.Background(), server.URL+"/", "test-key", map[string]any{
				"task_id": "TASK-123",
				"model":   testCase.model,
			}, "")
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestConvertToOpenAIVideoExtractsByteforDataURL(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1781690400,
		Properties: model.Properties{
			OriginModelName: "bytefor-2.0-real-priority",
		},
		Data: []byte(`{"status":"completed","data":[{"url":"https://cdn.example.com/video.mp4"}]}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &response))
	require.NotNil(t, response.Metadata)
	assert.Equal(t, "https://cdn.example.com/video.mp4", response.Metadata["url"])
}

func TestConvertToOpenAIVideoIncludesByteforFailureReason(t *testing.T) {
	task := &model.Task{
		TaskID: "task_public",
		Status: model.TaskStatusFailure,
		Properties: model.Properties{
			OriginModelName: "bytefor-2.0-real-priority",
		},
		Data: []byte(`{"status":"failed","error_code":"GENERATION_FAILED","error_msg":"视频生成失败，请检查素材是否清晰、可访问且符合要求，调整后重试"}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &response))
	require.NotNil(t, response.Error)
	assert.Equal(t, "GENERATION_FAILED", response.Error.Code)
	assert.Equal(t, "视频生成失败，请检查素材是否清晰、可访问且符合要求，调整后重试", response.Error.Message)
}

func TestModelListIncludesByteforModels(t *testing.T) {
	models := (&TaskAdaptor{}).GetModelList()
	for _, modelName := range byteforModelList {
		assert.Contains(t, models, modelName)
	}
}
