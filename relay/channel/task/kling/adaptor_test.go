package kling

import (
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

func TestConvertToRequestPayloadUsesPublicAspectRatioWithoutGuessingUnknownSize(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1"}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:      "animate",
		Size:        "960x540",
		AspectRatio: "16:9",
		Metadata:    map[string]any{"aspect_ratio": "9:16"},
	}, info)
	require.NoError(t, err)
	assert.Equal(t, "16:9", payload.AspectRatio)

	payload, err = adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "animate",
		Size:   "960x540",
	}, info)
	require.NoError(t, err)
	assert.Empty(t, payload.AspectRatio)
}

func TestConvertToRequestPayloadMapsCanonicalImagesToKlingFrames(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "animate",
		Images: []string{
			"https://example.com/start.png",
			"https://example.com/end.png",
		},
		Metadata: map[string]any{
			"image":      "https://example.com/legacy-start.png",
			"image_tail": "https://example.com/legacy-end.png",
		},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1-6"}})

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/start.png", payload.Image)
	assert.Equal(t, "https://example.com/end.png", payload.ImageTail)
}

func TestConvertToRequestPayloadMapsContentImageToKlingFrame(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "animate",
		Content: []relaycommon.TaskContentItem{{
			Type:     "image_url",
			ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/content-frame.png"},
		}},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1"}})

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/content-frame.png", payload.Image)
}

func TestConvertToRequestPayloadMapsKlingImageCompatibilityAliases(t *testing.T) {
	testCases := []struct {
		name     string
		request  relaycommon.TaskSubmitReq
		expected string
	}{
		{
			name:     "input reference",
			request:  relaycommon.TaskSubmitReq{Prompt: "animate", InputReference: "https://example.com/input-reference.png"},
			expected: "https://example.com/input-reference.png",
		},
		{
			name:     "input start frames",
			request:  relaycommon.TaskSubmitReq{Prompt: "animate", InputStartFrames: []string{"https://example.com/start-frame.png"}},
			expected: "https://example.com/start-frame.png",
		},
		{
			name:     "input image references",
			request:  relaycommon.TaskSubmitReq{Prompt: "animate", InputImageReferences: []string{"https://example.com/reference.png"}},
			expected: "https://example.com/reference.png",
		},
		{
			name:     "metadata start frames",
			request:  relaycommon.TaskSubmitReq{Prompt: "animate", MetadataStartFrames: []string{"https://example.com/metadata-start.png"}},
			expected: "https://example.com/metadata-start.png",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&testCase.request, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1"},
			})

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, payload.Image)
		})
	}
}

func TestValidateMappedRequestRejectsTooManyCanonicalKlingImages(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-kling-model",
		"prompt":"animate",
		"images":["https://example.com/1.png","https://example.com/2.png","https://example.com/3.png"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_request", taskErr.Code)
	assert.Contains(t, taskErr.Message, "at most two image inputs")
}

func TestConvertToRequestPayloadCountsDuplicateKlingContentImages(t *testing.T) {
	_, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "animate",
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/second.png",
		},
		Content: []relaycommon.TaskContentItem{{
			Type:     "image_url",
			ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/first.png"},
		}},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most two image inputs")
}

func TestValidateMappedRequestRejectsUnsupportedKlingVideoAndAudioInputs(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "top level canonical and legacy media",
			body: `{
				"model":"public-kling-model",
				"prompt":"animate",
				"videos":["https://example.com/reference.mp4"],
				"audio_urls":["https://example.com/reference.mp3"]
			}`,
		},
		{
			name: "content media",
			body: `{
				"model":"public-kling-model",
				"prompt":"animate",
				"content":[
					{"type":"video_url","video_url":{"url":"https://example.com/reference.mp4"}},
					{"type":"audio_url","audio_url":{"url":"https://example.com/reference.mp3"}}
				]
			}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor := &TaskAdaptor{}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			taskErr := adaptor.ValidateMappedRequest(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "invalid_request", taskErr.Code)
			assert.Contains(t, taskErr.Message, "does not support video or audio")
		})
	}
}

func TestConvertToOpenAIVideoKeepsKlingTimestampUnitsConsistent(t *testing.T) {
	var payload responsePayload
	payload.Data.CreatedAt = 1712345678000
	payload.Data.UpdatedAt = 1712345689000
	payload.Data.TaskStatus = "succeed"
	taskData, err := common.Marshal(payload)
	require.NoError(t, err)

	task := &model.Task{
		TaskID:     "task_kling_timestamp",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FinishTime: 1712345689,
		Data:       taskData,
	}
	responseBody, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(responseBody, &video))
	assert.EqualValues(t, payload.Data.CreatedAt, video.CreatedAt)
	assert.EqualValues(t, payload.Data.UpdatedAt, video.CompletedAt)
	assert.GreaterOrEqual(t, video.CompletedAt, video.CreatedAt)

	task.Status = model.TaskStatusInProgress
	responseBody, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	video = dto.OpenAIVideo{}
	require.NoError(t, common.Unmarshal(responseBody, &video))
	assert.Zero(t, video.CompletedAt)
}
