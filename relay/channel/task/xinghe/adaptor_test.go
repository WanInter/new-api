package xinghe

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadNormalizesXingheParams(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_local"}}
	req := &relaycommon.TaskSubmitReq{
		Prompt:   "city",
		Images:   []string{"https://example.com/a.png", "https://example.com/b.png"},
		Duration: 20,
		Metadata: map[string]any{"aspect_ratio": "9:16", "resolution": "1080p"},
	}

	payload, err := adaptor.convertToRequestPayload(req, info)
	require.NoError(t, err)
	require.Equal(t, "xinghe-2.0", payload.Model)
	require.Equal(t, 15, payload.Duration)
	require.Equal(t, "9:16", payload.Ratio)
	require.Equal(t, "1080p", payload.Resolution)
	require.Equal(t, []string{"https://example.com/a.png", "https://example.com/b.png"}, payload.ImageURLs)
	require.Equal(t, "task_local", payload.ClientTaskID)
}

func TestConvertToRequestPayloadPrefersTopLevelOutputFields(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	req := &relaycommon.TaskSubmitReq{
		Prompt:      "city",
		Images:      []string{"https://example.com/a.png"},
		AspectRatio: "9:16",
		Resolution:  "1080p",
		Metadata:    map[string]any{"aspect_ratio": "16:9", "resolution": "720p"},
	}

	payload, err := adaptor.convertToRequestPayload(req, info)
	require.NoError(t, err)
	require.Equal(t, "9:16", payload.Ratio)
	require.Equal(t, "1080p", payload.Resolution)
}

func TestConvertToRequestPayloadRequiresMaterial(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-mini"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	_, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "text only"}, info)
	require.ErrorContains(t, err, "requires at least one image")
}

func TestConvertToRequestPayloadAcceptsVideoAndAudioAliases(t *testing.T) {
	adaptor := &TaskAdaptor{}
	metadata := map[string]any{
		"video_urls":           []any{"https://example.com/v1.mp4"},
		"audio_urls":           []any{"https://example.com/a1.mp3"},
		"reference_video_urls": []any{"https://example.com/ref.mp4"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-fast"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "city", Metadata: metadata}, info)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/v1.mp4"}, payload.VideoURLs)
	require.Equal(t, []string{"https://example.com/a1.mp3"}, payload.AudioURLs)
	require.Equal(t, []string{"https://example.com/ref.mp4"}, payload.ReferenceVideoURLs)
}

func TestValidateRequestForwardsCanonicalMediaFields(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"xinghe-fast",
		"prompt":"city",
		"images":"https://example.com/frame.png",
		"videos":["https://example.com/motion.mp4"],
		"video_urls":["https://example.com/motion.mp4","https://example.com/legacy-motion.mp4"],
		"audios":"https://example.com/music.mp3",
		"audio_urls":["https://example.com/music.mp3","https://example.com/legacy-music.mp3"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/frame.png"}, req.Images)
	assert.Equal(t, []string{"https://example.com/motion.mp4"}, req.Videos)
	assert.Equal(t, []string{"https://example.com/music.mp3"}, req.Audios)

	payload, err := adaptor.convertToRequestPayload(&req, &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-fast"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/motion.mp4", "https://example.com/motion.mp4", "https://example.com/legacy-motion.mp4"}, payload.VideoURLs)
	assert.Equal(t, []string{"https://example.com/music.mp3", "https://example.com/music.mp3", "https://example.com/legacy-music.mp3"}, payload.AudioURLs)
}

func TestValidateRequestForwardsContentMediaFields(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"xinghe-2.0",
		"prompt":"city",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/frame.png"}},
			{"type":"video_url","video_url":{"url":"https://example.com/motion.mp4"}},
			{"type":"audio_url","audio_url":{"url":"https://example.com/music.mp3"}}
		]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	payload, err := adaptor.convertToRequestPayload(&req, &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/frame.png"}, payload.ImageURLs)
	assert.Equal(t, []string{"https://example.com/motion.mp4"}, payload.VideoURLs)
	assert.Equal(t, []string{"https://example.com/music.mp3"}, payload.AudioURLs)
}

func TestValidateMappedRequestRejectsUnsupportedXingheInputs(t *testing.T) {
	testCases := []struct {
		name    string
		request relaycommon.TaskSubmitReq
		model   string
		code    string
		message string
	}{
		{
			name:    "too many images",
			request: relaycommon.TaskSubmitReq{Prompt: "city", Images: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}},
			model:   "xinghe-fast",
			code:    "invalid_media_input",
			message: "at most 9 image",
		},
		{
			name: "too many content videos",
			request: relaycommon.TaskSubmitReq{Prompt: "city", Content: []relaycommon.TaskContentItem{
				{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "1"}},
				{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "2"}},
				{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "3"}},
				{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "4"}},
			}},
			model:   "xinghe-fast",
			code:    "invalid_media_input",
			message: "at most 3 video",
		},
		{
			name:    "unsupported ratio",
			request: relaycommon.TaskSubmitReq{Prompt: "city", AspectRatio: "1:1"},
			model:   "xinghe-fast",
			code:    "invalid_video_output",
			message: "supports only 16:9 and 9:16",
		},
		{
			name:    "unsupported resolution",
			request: relaycommon.TaskSubmitReq{Prompt: "city", Resolution: "4k"},
			model:   "xinghe-2.0",
			code:    "invalid_video_output",
			message: "does not support resolution",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("task_request", testCase.request)
			info := &relaycommon.RelayInfo{
				ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: testCase.model},
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			}

			taskErr := (&TaskAdaptor{}).ValidateMappedRequest(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, testCase.code, taskErr.Code)
			assert.Contains(t, taskErr.Message, testCase.message)
		})
	}
}

func TestConvertToRequestPayloadKeepsMappedModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0", IsModelMapped: true}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	req := &relaycommon.TaskSubmitReq{Model: "public-xinghe", Prompt: "city", Images: []string{"https://example.com/a.png"}}

	payload, err := adaptor.convertToRequestPayload(req, info)
	require.NoError(t, err)
	require.Equal(t, "xinghe-2.0", payload.Model)
}

func TestValidateRequestNormalizesVideoOutputAliases(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"xinghe-2.0",
		"prompt":"city",
		"images":["https://example.com/a.png"],
		"aspectRatio":"32:18",
		"resolution":"1080P"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "16:9", req.AspectRatio)
	assert.Equal(t, "1080p", req.Resolution)
}

func TestValidateRequestRejectsConflictingVideoOutputAliases(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"xinghe-2.0",
		"prompt":"city",
		"images":["https://example.com/a.png"],
		"ratio":"16:9",
		"aspectRatio":"9:16"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, "conflicts with aspect_ratio")
}

func TestParseTaskResultExtractsNestedURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{"task_status":"completed","data":{"metadata":{"result_urls":["https://example.com/v.mp4"]}}}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", info.Status)
	require.Equal(t, "https://example.com/v.mp4", info.Url)
}
