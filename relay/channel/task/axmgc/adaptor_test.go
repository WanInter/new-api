package axmgc

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
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

func TestBuildJSONRequestSupportsDocumentedURLFormsAndMappedModel(t *testing.T) {
	c := newJSONContext(t, `{
		"model":"public-seedance",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/role.png"}},
			{"type":"image_url","image_url":"https://example.com/scene.jpg"},
			{"type":"video_url","url":"https://example.com/camera.mp4"},
			{"type":"audio_url","audio_url":{"url":"https://example.com/bgm.mp3"}},
			{"type":"text","text":"@Image1 is the lead"}
		],
		"aspect_ratio":"16:9",
		"resolution":"720p",
		"duration":6
	}`)
	info := newRelayInfo()
	info.OriginModelName = "public-seedance"
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = Seedance720p933Model
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload requestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, Seedance720p933Model, payload.Model)
	assert.Equal(t, "16:9", payload.AspectRatio)
	assert.Equal(t, defaultResolution, payload.Resolution)
	assert.Equal(t, defaultDuration, payload.Duration)
	require.Len(t, payload.Content, 5)
	assert.Equal(t, "https://example.com/role.png", payload.Content[0].ImageURL.URL)
	assert.Equal(t, "https://example.com/scene.jpg", payload.Content[1].ImageURL.URL)
	assert.Equal(t, "https://example.com/camera.mp4", payload.Content[2].VideoURL.URL)
	assert.Equal(t, "https://example.com/bgm.mp3", payload.Content[3].AudioURL.URL)
	assert.Equal(t, "@Image1 is the lead", payload.Content[4].Text)

	request, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/role.png", "https://example.com/scene.jpg"}, request.Images)
	assert.Equal(t, []string{"https://example.com/camera.mp4"}, request.Videos)
	assert.Equal(t, []string{"https://example.com/bgm.mp3"}, request.Audios)
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
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload requestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Len(t, payload.Content, 4)
	assert.Equal(t, "image_url", payload.Content[0].Type)
	assert.Equal(t, "video_url", payload.Content[1].Type)
	assert.Equal(t, "audio_url", payload.Content[2].Type)
	assert.Equal(t, "text", payload.Content[3].Type)
	assert.Equal(t, defaultResolution, payload.Resolution)
	assert.Equal(t, defaultDuration, payload.Duration)
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
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload requestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, defaultDuration, payload.Duration)
}

func TestValidateJSONRequestRejectsUnsupportedResolution(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test","duration":15,"resolution":"1080p"}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "resolution must be 720p")
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

func TestBuildMultipartRequestCanonicalizesFileFieldsAndForwardsIdempotencyKey(t *testing.T) {
	var incoming bytes.Buffer
	writer := multipart.NewWriter(&incoming)
	require.NoError(t, writer.WriteField("model", Seedance720p933Model))
	require.NoError(t, writer.WriteField("prompt", "@Image1 is the lead"))
	require.NoError(t, writer.WriteField("aspect_ratio", "16:9"))
	require.NoError(t, writer.WriteField("resolution", "720p"))
	require.NoError(t, writer.WriteField("duration", "8"))
	imagePart, err := writer.CreateFormFile("image", "role.png")
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("image data"))
	require.NoError(t, err)
	videoPart, err := writer.CreateFormFile("videos", "camera.mp4")
	require.NoError(t, err)
	_, err = videoPart.Write([]byte("video data"))
	require.NoError(t, err)
	audioPart, err := writer.CreateFormFile("audios", "bgm.mp3")
	require.NoError(t, err)
	_, err = audioPart.Write([]byte("audio data"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader(incoming.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request.Header.Set("X-Idempotency-Key", "scene-001")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, DefaultBaseURL+"/v1/video/generations/multipart", mustBuildURL(t, adaptor, info))

	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/form-data", mediaType)
	parsed, err := multipart.NewReader(body, params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = parsed.RemoveAll() })
	assert.Equal(t, []string{Seedance720p933Model}, parsed.Value["model"])
	assert.Equal(t, []string{"@Image1 is the lead"}, parsed.Value["prompt"])
	assert.Equal(t, []string{"15"}, parsed.Value["duration"])
	require.Len(t, parsed.File["images"], 1)
	require.Len(t, parsed.File["videos"], 1)
	require.Len(t, parsed.File["audios"], 1)
	assert.Equal(t, "role.png", parsed.File["images"][0].Filename)

	upstreamRequest := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, upstreamRequest, info))
	assert.Equal(t, "Bearer hm_test", upstreamRequest.Header.Get("Authorization"))
	assert.Equal(t, "scene-001", upstreamRequest.Header.Get("X-Idempotency-Key"))
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
