package axmgc

import (
	"bytes"
	"context"
	"fmt"
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

type remoteMediaFixture struct {
	ContentType string
	Body        string
	StatusCode  int
	ContentSize int64
}

func stubRemoteMediaDownloads(t *testing.T, fixtures map[string]remoteMediaFixture) {
	t.Helper()
	previous := downloadRemoteMedia
	downloadRemoteMedia = func(_ context.Context, mediaURL string, _ ...string) (*http.Response, error) {
		fixture, ok := fixtures[mediaURL]
		if !ok {
			return nil, fmt.Errorf("unexpected media URL %q", mediaURL)
		}
		statusCode := fixture.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		contentLength := fixture.ContentSize
		if contentLength == 0 {
			contentLength = int64(len(fixture.Body))
		}
		return &http.Response{
			StatusCode:    statusCode,
			ContentLength: contentLength,
			Header:        http.Header{"Content-Type": []string{fixture.ContentType}},
			Body:          io.NopCloser(strings.NewReader(fixture.Body)),
		}, nil
	}
	t.Cleanup(func() { downloadRemoteMedia = previous })
}

func parseBuiltMultipartBody(t *testing.T, c *gin.Context, body io.Reader) *multipart.Form {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(body, params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form
}

func readMultipartFile(t *testing.T, header *multipart.FileHeader) string {
	t.Helper()
	file, err := header.Open()
	require.NoError(t, err)
	defer file.Close()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	return string(data)
}

func TestBuildJSONRequestSupportsDocumentedURLFormsAndMappedModel(t *testing.T) {
	stubRemoteMediaDownloads(t, map[string]remoteMediaFixture{
		"https://example.com/role.png":   {ContentType: "image/png", Body: "role image"},
		"https://example.com/scene.jpg":  {ContentType: "image/jpeg", Body: "scene image"},
		"https://example.com/camera.mp4": {ContentType: "video/mp4", Body: "camera video"},
		"https://example.com/bgm.mp3":    {ContentType: "audio/mpeg", Body: "background audio"},
	})
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
	info.UpstreamModelName = seedance720pUpstreamModel
	info.IsModelMapped = true
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, DefaultBaseURL+"/v1/video/generations/multipart", mustBuildURL(t, adaptor, info))

	form := parseBuiltMultipartBody(t, c, body)
	assert.Equal(t, []string{seedance720pUpstreamModel}, form.Value["model"])
	assert.Equal(t, []string{"@Image1 is the lead"}, form.Value["prompt"])
	assert.Equal(t, []string{"16:9"}, form.Value["aspect_ratio"])
	assert.Equal(t, []string{defaultResolution}, form.Value["resolution"])
	assert.Equal(t, []string{"15"}, form.Value["duration"])
	require.Len(t, form.File["images"], 2)
	require.Len(t, form.File["videos"], 1)
	require.Len(t, form.File["audios"], 1)
	assert.Equal(t, "image-1.png", form.File["images"][0].Filename)
	assert.Equal(t, "image-2.jpg", form.File["images"][1].Filename)
	assert.Equal(t, "role image", readMultipartFile(t, form.File["images"][0]))
	assert.Equal(t, "scene image", readMultipartFile(t, form.File["images"][1]))
	assert.Equal(t, "camera video", readMultipartFile(t, form.File["videos"][0]))
	assert.Equal(t, "background audio", readMultipartFile(t, form.File["audios"][0]))
	upstreamRequest := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, upstreamRequest, info))
	assert.Equal(t, "Bearer hm_test", upstreamRequest.Header.Get("Authorization"))
	assert.Contains(t, upstreamRequest.Header.Get("Content-Type"), "multipart/form-data")

	request, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/role.png", "https://example.com/scene.jpg"}, request.Images)
	assert.Equal(t, []string{"https://example.com/camera.mp4"}, request.Videos)
	assert.Equal(t, []string{"https://example.com/bgm.mp3"}, request.Audios)
}

func TestBuildJSONRequestConvertsTopLevelReferencesToContent(t *testing.T) {
	stubRemoteMediaDownloads(t, map[string]remoteMediaFixture{
		"https://example.com/role.png":   {ContentType: "image/png", Body: "image"},
		"https://example.com/motion.mp4": {ContentType: "video/mp4", Body: "video"},
		"https://example.com/music.mp3":  {ContentType: "audio/mpeg", Body: "audio"},
	})
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
	form := parseBuiltMultipartBody(t, c, body)
	assert.Equal(t, []string{seedance720pUpstreamModel}, form.Value["model"])
	assert.Equal(t, []string{"@Image1 runs through the scene"}, form.Value["prompt"])
	assert.Equal(t, []string{"15"}, form.Value["duration"])
	require.Len(t, form.File["images"], 1)
	require.Len(t, form.File["videos"], 1)
	require.Len(t, form.File["audios"], 1)
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
	form := parseBuiltMultipartBody(t, c, body)
	assert.Equal(t, []string{seedance720pUpstreamModel}, form.Value["model"])
	assert.Equal(t, []string{"15"}, form.Value["duration"])
}

func TestBuildJSONRequestPreservesExplicitUpstreamModelMapping(t *testing.T) {
	c := newJSONContext(t, `{"model":"seedance-2-720p-933","prompt":"test"}`)
	info := newRelayInfo()
	info.UpstreamModelName = "seedance-2-720p-custom"
	info.IsModelMapped = true
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	form := parseBuiltMultipartBody(t, c, body)
	assert.Equal(t, []string{"seedance-2-720p-custom"}, form.Value["model"])
}

func TestBuildJSONMultipartRejectsUnexpectedRemoteMediaType(t *testing.T) {
	stubRemoteMediaDownloads(t, map[string]remoteMediaFixture{
		"https://example.com/not-image": {ContentType: "text/plain", Body: "not an image"},
	})
	c := newJSONContext(t, `{
		"model":"seedance-2-720p-933",
		"prompt":"animate",
		"images":["https://example.com/not-image"]
	}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	_, err := adaptor.BuildRequestBody(c, info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `images item 1 returned unsupported content type "text/plain"`)
}

func TestBuildJSONMultipartRejectsOversizedRemoteMedia(t *testing.T) {
	stubRemoteMediaDownloads(t, map[string]remoteMediaFixture{
		"https://example.com/large.png": {
			ContentType: "image/png",
			ContentSize: downloadLimitBytes(constant.MaxFileDownloadMB, 64) + 1,
		},
	})
	c := newJSONContext(t, `{
		"model":"seedance-2-720p-933",
		"prompt":"animate",
		"images":["https://example.com/large.png"]
	}`)
	info := newRelayInfo()
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	_, err := adaptor.BuildRequestBody(c, info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "images item 1 exceeds the maximum allowed size")
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
	assert.Equal(t, []string{seedance720pUpstreamModel}, parsed.Value["model"])
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
