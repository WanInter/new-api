package hailuo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadPrefersTopLevelResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:     "a cat walks through a city",
		Size:       "720x1280",
		Resolution: "1080p",
		Metadata:   map[string]any{"resolution": "720P"},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"}})

	require.NoError(t, err)
	assert.Equal(t, Resolution1080P, payload.Resolution)
}

func TestConvertToRequestPayloadMapsCanonicalImageToHailuoFirstFrame(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "a cat walks through a city",
		Images: []string{"https://example.com/first-frame.png"},
		Metadata: map[string]any{
			"first_frame_image": "https://example.com/legacy-frame.png",
		},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"}})

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/first-frame.png", payload.FirstFrameImage)
}

func TestConvertToRequestPayloadMapsContentImageToHailuoFirstFrame(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "a cat walks through a city",
		Content: []relaycommon.TaskContentItem{{
			Type:     "image_url",
			ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/content-frame.png"},
		}},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"}})

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/content-frame.png", payload.FirstFrameImage)
}

func TestConvertToRequestPayloadMapsHailuoImageCompatibilityAliases(t *testing.T) {
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
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"},
			})

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, payload.FirstFrameImage)
		})
	}
}

func TestConvertToRequestPayloadMapsCanonicalImageToHailuoSubjectReference(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "a character turns to the camera",
		Images: []string{"https://example.com/character.png"},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "S2V-01"}})

	require.NoError(t, err)
	require.Equal(t, []SubjectReference{{
		Type:  "character",
		Image: []string{"https://example.com/character.png"},
	}}, payload.SubjectReference)
}

func TestConvertToRequestPayloadValidatesHailuoOutputResolution(t *testing.T) {
	testCases := []struct {
		name       string
		model      string
		request    relaycommon.TaskSubmitReq
		resolution string
		errText    string
	}{
		{
			name:       "accepts supported top-level resolution",
			model:      "MiniMax-Hailuo-2.3",
			request:    relaycommon.TaskSubmitReq{Prompt: "test", Resolution: "1080p"},
			resolution: Resolution1080P,
		},
		{
			name:    "rejects resolution unsupported by Hailuo 2.3",
			model:   "MiniMax-Hailuo-2.3",
			request: relaycommon.TaskSubmitReq{Prompt: "test", Resolution: "720p"},
			errText: "not supported",
		},
		{
			name:    "rejects resolution unsupported by T2V 01",
			model:   "T2V-01",
			request: relaycommon.TaskSubmitReq{Prompt: "test", Resolution: "1080p"},
			errText: "not supported",
		},
		{
			name:       "keeps default without output fields",
			model:      "MiniMax-Hailuo-2.3",
			request:    relaycommon.TaskSubmitReq{Prompt: "test"},
			resolution: Resolution768P,
		},
		{
			name:       "accepts exact legacy pixel alias",
			model:      "MiniMax-Hailuo-2.3",
			request:    relaycommon.TaskSubmitReq{Prompt: "test", Size: "1920x1080"},
			resolution: Resolution1080P,
		},
		{
			name:       "accepts exact legacy asterisk pixel alias",
			model:      "T2V-01",
			request:    relaycommon.TaskSubmitReq{Prompt: "test", Size: "1280*720"},
			resolution: Resolution720P,
		},
		{
			name:    "rejects unknown legacy pixel size",
			model:   "MiniMax-Hailuo-2.3",
			request: relaycommon.TaskSubmitReq{Prompt: "test", Size: "960x540"},
			errText: "960x540",
		},
		{
			name:    "rejects 4k instead of falling back",
			model:   "MiniMax-Hailuo-2.3",
			request: relaycommon.TaskSubmitReq{Prompt: "test", Resolution: "4k"},
			errText: "4k",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&testCase.request, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: testCase.model},
			})
			if testCase.errText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errText)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.resolution, payload.Resolution)
		})
	}
}

func TestValidateMappedRequestRejectsUnsupportedResolutionBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-hailuo-model",
		"prompt":"test",
		"resolution":"4k"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "4k")
}

func TestValidateMappedRequestRejectsUnsupportedHailuoImageInputBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-hailuo-model",
		"prompt":"test",
		"images":["https://example.com/first-frame.png"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "T2V-01", IsModelMapped: true},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_request", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "does not support image inputs")
}

func TestValidateMappedRequestRejectsMultipleHailuoImagesBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-hailuo-model",
		"prompt":"test",
		"images":["https://example.com/first.png","https://example.com/second.png"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3", IsModelMapped: true},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_request", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "exactly one first-frame image")
}

func TestConvertToRequestPayloadCountsDuplicateHailuoContentImages(t *testing.T) {
	_, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "animate",
		Images: []string{
			"https://example.com/first.png",
		},
		Content: []relaycommon.TaskContentItem{{
			Type:     "image_url",
			ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/first.png"},
		}},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one first-frame image")
}

func TestValidateMappedRequestRejectsUnsupportedHailuoVideoAndAudioInputs(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "top level canonical and legacy media",
			body: `{
				"model":"public-hailuo-model",
				"prompt":"test",
				"videos":["https://example.com/reference.mp4"],
				"audio_urls":["https://example.com/reference.mp3"]
			}`,
		},
		{
			name: "content media",
			body: `{
				"model":"public-hailuo-model",
				"prompt":"test",
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

			info := &relaycommon.RelayInfo{
				ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3", IsModelMapped: true},
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			}
			adaptor := &TaskAdaptor{}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			taskErr := adaptor.ValidateMappedRequest(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_request", taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Contains(t, taskErr.Message, "does not support video or audio")
		})
	}
}

func TestParseTaskResultWithContextCancelsFileLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	adaptor := &TaskAdaptor{apiKey: "test-key", baseURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := adaptor.ParseTaskResultWithContext(ctx, []byte(`{
		"task_id":"upstream-task",
		"status":"Success",
		"file_id":"file-id",
		"base_resp":{"status_code":0}
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.EqualValues(t, model.TaskStatusSuccess, result.Status)
	assert.Empty(t, result.Url)
}
