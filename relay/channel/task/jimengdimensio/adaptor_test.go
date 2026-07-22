package jimengdimensio

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateMultipartRequestConsumesCanonicalImages(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "jimeng-video-seedance-2.0-vip"))
	require.NoError(t, writer.WriteField("prompt", "cat"))
	require.NoError(t, writer.WriteField("images", "https://example.com/frame.png"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/frame.png"}, req.Images)
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req, info)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/frame.png", payload.ImageFile1)
}

func TestValidateRequestRejectsUnsupportedVideoAndAudioReferences(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"jimeng-video-seedance-2.0-vip",
		"prompt":"cat",
		"videos":["https://example.com/reference.mp4"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_request", taskErr.Code)
	require.Contains(t, taskErr.Message, "does not support video or audio")
}

func TestConvertToRequestPayloadKeepsMappedUpstreamModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{Model: "Seedance2.0-jimeng", Prompt: "cat"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "Seedance2.0-jimeng",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "jimeng-video-seedance-2.0-vip",
			IsModelMapped:     true,
		},
	}

	payload, err := adaptor.convertToRequestPayload(&req, info)
	require.NoError(t, err)
	require.Equal(t, "jimeng-video-seedance-2.0-vip", payload.Model)
}

func TestConvertToRequestPayloadSelectsFunctionModeByImageCount(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "text only"}, info)
	require.NoError(t, err)
	require.Empty(t, payload.FunctionMode)

	payload, err = adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "two images", Images: []string{"https://example.com/1.jpg", "https://example.com/2.jpg"}}, info)
	require.NoError(t, err)
	require.Equal(t, "first_last_frames", payload.FunctionMode)
	require.Equal(t, "https://example.com/1.jpg", payload.ImageFile1)
	require.Equal(t, "https://example.com/2.jpg", payload.ImageFile2)
	require.Empty(t, payload.FilePaths)

	payload, err = adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "many images", Images: []string{"1", "2", "3"}}, info)
	require.NoError(t, err)
	require.Equal(t, "omni_reference", payload.FunctionMode)
}

func TestConvertToRequestPayloadUsesAspectRatioMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "cat",
		Metadata: map[string]any{"aspect_ratio": "16:9"},
	}, info)
	require.NoError(t, err)
	require.Equal(t, "16:9", payload.Ratio)
}

func TestValidateRequestPrefersTopLevelOutputFields(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"jimeng-video-seedance-2.0-vip",
		"prompt":"cat",
		"ratio":"9:16",
		"aspect_ratio":"9:16",
		"resolution":"1080p",
		"metadata":{"aspect_ratio":"16:9","resolution":"720p"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "jimeng-video-seedance-2.0-vip",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Equal(t, "9:16", req.AspectRatio)
	require.Empty(t, req.Ratio)
	require.Equal(t, "1080p", req.Resolution)

	payload, err := adaptor.convertToRequestPayload(&req, info)
	require.NoError(t, err)
	require.Equal(t, "9:16", payload.Ratio)
	require.Equal(t, "1080p", payload.Resolution)
}

func TestValidateRequestRejectsConflictingVideoOutputAliases(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"jimeng-video-seedance-2.0-vip",
		"prompt":"cat",
		"ratio":"16:9",
		"aspectRatio":"9:16"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_video_output", taskErr.Code)
	require.Contains(t, taskErr.Message, "conflicts with aspect_ratio")
}

func TestValidateRequestRejectsUnsupportedPixelSize(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"jimeng-video-seedance-2.0-vip",
		"prompt":"cat",
		"size":"960x540",
		"resolution":"720p"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})

	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_video_output", taskErr.Code)
	require.Contains(t, taskErr.Message, `size "960x540" is not supported`)
}

func TestConvertToRequestPayloadMapsDocumentedLegacySize(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "cat",
		Size:   "1280x720",
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"}})

	require.NoError(t, err)
	require.Equal(t, "720p", payload.Resolution)
}

func TestConvertToOpenAIVideoUsesSoraCompatibleResponseShape(t *testing.T) {
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
			OriginModelName: "Seedance2.0-jimeng",
		},
		Data: []byte(`{
			"task_id":"task_upstream",
			"status":"completed",
			"progress":100,
			"result":{"url":"https://example.com/video.mp4"}
		}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	require.Equal(t, "task_public", got["id"])
	require.Equal(t, "video", got["object"])
	require.Equal(t, "task_upstream", got["task_id"])
	require.Equal(t, "Seedance2.0-jimeng", got["model"])
	require.Equal(t, "completed", got["status"])
	require.Equal(t, "https://example.com/video.mp4", got["result_url"])
	require.Equal(t, "https://example.com/video.mp4", got["url"])
	require.Equal(t, "https://example.com/video.mp4", got["video_url"])
	require.Equal(t, []any{"https://example.com/video.mp4"}, got["output"])

	video, ok := got["video"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://example.com/video.mp4", video["url"])
}
