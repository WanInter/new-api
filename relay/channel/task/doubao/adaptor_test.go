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
		{name: "default", request: relaycommon.TaskSubmitReq{}, expected: byteforDefaultDurationSeconds},
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

func TestByteforDurationDefaultsToBilledDuration(t *testing.T) {
	request := relaycommon.TaskSubmitReq{}

	assert.Equal(t, "4s", byteforDuration(&request))
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

func TestModelListIncludesByteforModels(t *testing.T) {
	models := (&TaskAdaptor{}).GetModelList()
	for _, modelName := range byteforModelList {
		assert.Contains(t, models, modelName)
	}
}
