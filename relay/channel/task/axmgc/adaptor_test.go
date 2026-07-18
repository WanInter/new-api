package axmgc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJSONContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func newRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: Seedance720p933Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:         "hm_test",
			ChannelBaseUrl: DefaultBaseURL,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
}

func parseBuiltJSONBody(t *testing.T, c *gin.Context, body io.Reader) axmgcJSONRequest {
	t.Helper()
	assert.Equal(t, "application/json", c.GetHeader("Content-Type"))
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload axmgcJSONRequest
	require.NoError(t, common.Unmarshal(data, &payload))
	return payload
}

func TestBuildJSONRequestSupportsDocumentedURLForms(t *testing.T) {
	c := newJSONContext(t, `{
		"model":"seedance-2-720p-933",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/role.png"}},
			{"type":"image_url","url":"https://example.com/scene.jpg"},
			{"type":"video_url","video_url":{"url":"https://example.com/camera.mp4"}},
			{"type":"audio_url","audio_url":{"url":"https://example.com/bgm.mp3"}},
			{"type":"text","text":"@Image1 is the lead"}
		],
		"aspect_ratio":"16:9",
		"resolution":"720p",
		"duration":6,
		"generate_audio":false,
		"seed":42,
		"watermark":false,
		"return_last_frame":false
	}`)
	info := newRelayInfo()
	info.UpstreamModelName = Seedance720p933Model
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, DefaultBaseURL+"/v1/video/generations", mustBuildURL(t, adaptor, info))

	payload := parseBuiltJSONBody(t, c, body)
	assert.Equal(t, Seedance720p933Model, payload.Model)
	assert.Equal(t, "16:9", payload.AspectRatio)
	assert.Equal(t, defaultResolution, payload.Resolution)
	assert.Equal(t, defaultDuration, payload.Duration)
	assert.Equal(t, []map[string]any{
		urlContentItem("image_url", "https://example.com/role.png"),
		urlContentItem("image_url", "https://example.com/scene.jpg"),
		urlContentItem("video_url", "https://example.com/camera.mp4"),
		urlContentItem("audio_url", "https://example.com/bgm.mp3"),
		{"type": "text", "text": "@Image1 is the lead"},
	}, payload.Content)
	require.NotNil(t, payload.GenerateAudio)
	require.NotNil(t, payload.Seed)
	require.NotNil(t, payload.Watermark)
	require.NotNil(t, payload.ReturnLastFrame)
	assert.False(t, *payload.GenerateAudio)
	assert.Equal(t, 42, *payload.Seed)
	assert.False(t, *payload.Watermark)
	assert.False(t, *payload.ReturnLastFrame)

	upstreamRequest := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, upstreamRequest, info))
	assert.Equal(t, "Bearer hm_test", upstreamRequest.Header.Get("Authorization"))
	assert.Equal(t, "application/json", upstreamRequest.Header.Get("Content-Type"))
}

func TestBuildJSONRequestConvertsTopLevelReferencesToContent(t *testing.T) {
	c := newJSONContext(t, `{
		"model":"seedance-2-720p-933",
		"prompt":"@Image1 runs through the scene",
		"images":["https://example.com/role.png"],
		"videos":["https://example.com/motion.mp4"],
		"audios":["https://example.com/music.mp3"]
	}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := parseBuiltJSONBody(t, c, body)
	assert.Equal(t, []map[string]any{
		urlContentItem("image_url", "https://example.com/role.png"),
		urlContentItem("video_url", "https://example.com/motion.mp4"),
		urlContentItem("audio_url", "https://example.com/music.mp3"),
		{"type": "text", "text": "@Image1 runs through the scene"},
	}, payload.Content)
	assert.Equal(t, defaultDuration, payload.Duration)
}

func TestBuildJSONRequestSupportsAssetReferences(t *testing.T) {
	c := newJSONContext(t, `{
		"model":"seedance-2-720p-933",
		"content":[
			{"type":"image_asset","image_asset":{"asset_id":"asset_role"}},
			{"type":"video_asset","video_asset":{"asset_id":"asset_camera"}},
			{"type":"audio_asset","audio_asset":{"asset_id":"asset_bgm"}},
			{"type":"text","text":"@Image1 is the lead; use @Video1 and @Audio1."}
		]
	}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := parseBuiltJSONBody(t, c, body)
	assert.Equal(t, []map[string]any{
		{"type": "image_asset", "image_asset": map[string]any{"asset_id": "asset_role"}},
		{"type": "video_asset", "video_asset": map[string]any{"asset_id": "asset_camera"}},
		{"type": "audio_asset", "audio_asset": map[string]any{"asset_id": "asset_bgm"}},
		{"type": "text", "text": "@Image1 is the lead; use @Video1 and @Audio1."},
	}, payload.Content)
}

func TestBuildJSONRequestNormalizesDurationTo15Seconds(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test","duration":10}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	request, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, defaultDuration, request.Duration)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, defaultDuration, parseBuiltJSONBody(t, c, body).Duration)
}

func TestBuildJSONRequestUsesExplicitModelMapping(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test"}`)
	info := newRelayInfo()
	info.IsModelMapped = true
	info.UpstreamModelName = "seedance-2-720p-mapped"
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, "seedance-2-720p-mapped", parseBuiltJSONBody(t, c, body).Model)
}

func TestBuildJSONRequestUsesSubmittedModelWithoutMapping(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test"}`)
	info := newRelayInfo()
	info.UpstreamModelName = "legacy-channel-default"
	info.ChannelMeta.UpstreamModelName = "legacy-channel-default"
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, Seedance720p933Model, parseBuiltJSONBody(t, c, body).Model)
}

func TestValidateJSONRequestRejectsMissingModel(t *testing.T) {
	c := newJSONContext(t, `{"prompt":"test"}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "missing_model", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

func TestValidateJSONRequestRejectsUnsupportedResolution(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test","resolution":"1080p"}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "resolution must be 720p")
}

func TestValidateJSONRequestRejectsReferenceAfterText(t *testing.T) {
	c := newJSONContext(t, `{
		"model":"seedance-2-720p-933",
		"content":[
			{"type":"text","text":"late reference"},
			{"type":"image_url","image_url":{"url":"https://example.com/role.png"}}
		]
	}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "reference content must appear before text content")
}

func TestRejectsMultipartRequests(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test"}`)
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "unsupported_media_type", taskErr.Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, taskErr.StatusCode)
}

func TestRejectsRemixRequests(t *testing.T) {
	c := newJSONContext(t, `{"prompt":"remix this"}`)
	info := newRelayInfo()
	info.Action = constant.TaskActionRemix
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "unsupported_action", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, constant.TaskActionRemix, info.Action)

	_, err := adaptor.BuildRequestURL(info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support video remix")
}

func TestNormalizeBillingRequestBodyUsesFixedDuration(t *testing.T) {
	body, err := (&TaskAdaptor{}).NormalizeBillingRequestBody(newRelayInfo(), []byte(`{
		"model":"seedance-2-720p-933",
		"duration":10,
		"seconds":"10"
	}`))
	require.NoError(t, err)

	var normalized map[string]any
	require.NoError(t, common.Unmarshal(body, &normalized))
	assert.Equal(t, float64(defaultDuration), normalized["duration"])
	assert.NotContains(t, normalized, "seconds")
}

func mustBuildURL(t *testing.T, adaptor *TaskAdaptor, info *relaycommon.RelayInfo) string {
	t.Helper()
	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	return url
}

func TestDoResponseUsesPublicTaskID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := newRelayInfo()
	info.PublicTaskID = "task_public"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(`{
		"id":"video_upstream",
		"model":"seedance-2-720p-933",
		"status":"submitted"
	}`))}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "video_upstream", upstreamID)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &video))
	assert.Equal(t, "task_public", video.ID)
	assert.Equal(t, "task_public", video.TaskID)
	assert.Equal(t, Seedance720p933Model, video.Model)
	assert.Equal(t, dto.VideoStatusQueued, video.Status)
}

func TestParseTaskResultMapsSuccessAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
			"id":"video_1",
			"status":"succeeded",
			"resource_list":[
				{"resource_type":"thumbnail","resource_url":"https://example.com/cover.jpg"},
				{"resource_type":"video","resource_url":"https://example.com/video.mp4"}
			]
		}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
		assert.Equal(t, "100%", result.Progress)
		assert.Equal(t, "https://example.com/video.mp4", result.Url)
	})

	t.Run("failure", func(t *testing.T) {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
			"id":"video_2",
			"status":"failed",
			"fail_reason":"reference image unavailable"
		}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusFailure), result.Status)
		assert.Equal(t, "100%", result.Progress)
		assert.Equal(t, "reference image unavailable", result.Reason)
	})
}

func TestFetchTaskUsesGenerationPathAndBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/video/generations/video_1", r.URL.Path)
		assert.Equal(t, "Bearer hm_poll", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"video_1","status":"running"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(context.Background(), server.URL, "hm_poll", map[string]any{
		"task_id": "video_1",
	}, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestConvertToOpenAIVideoIncludesStoredResultURL(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 100,
		UpdatedAt: 200,
		Properties: model.Properties{
			OriginModelName: Seedance720p933Model,
		},
		PrivateData: model.TaskPrivateData{ResultURL: "https://example.com/video.mp4"},
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &video))
	assert.Equal(t, dto.VideoStatusCompleted, video.Status)
	assert.Equal(t, "https://example.com/video.mp4", video.Metadata["result_url"])
}
