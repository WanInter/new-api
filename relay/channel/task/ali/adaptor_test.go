package ali

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

func TestConvertToAliRequestUsesCanonicalResolutionOverMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{}
	request, err := adaptor.convertToAliRequest(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "wan2.5-i2v-preview", IsModelMapped: true},
	}, relaycommon.TaskSubmitReq{
		Model:      "public-wan",
		Prompt:     "animate",
		Resolution: "720p",
		Metadata: map[string]any{
			"parameters": map[string]any{"resolution": "1080P"},
		},
	})

	require.NoError(t, err)
	require.Equal(t, "wan2.5-i2v-preview", request.Model)
	require.Equal(t, "720P", request.Parameters.Resolution)
	require.Empty(t, request.Parameters.Size)
}

func TestConvertToAliRequestMapsCanonicalTextToVideoOutput(t *testing.T) {
	request, err := (&TaskAdaptor{}).convertToAliRequest(&relaycommon.RelayInfo{}, relaycommon.TaskSubmitReq{
		Model:       "wan2.5-t2v-preview",
		Prompt:      "animate",
		AspectRatio: "9:16",
		Resolution:  "720p",
	})

	require.NoError(t, err)
	require.Equal(t, "720*1280", request.Parameters.Size)
	require.Empty(t, request.Parameters.Resolution)
}

func TestConvertToAliRequestAppliesAspectRatioToDefaultTextToVideoResolution(t *testing.T) {
	request, err := (&TaskAdaptor{}).convertToAliRequest(&relaycommon.RelayInfo{}, relaycommon.TaskSubmitReq{
		Model:       "wan2.5-t2v-preview",
		Prompt:      "animate",
		AspectRatio: "9:16",
	})

	require.NoError(t, err)
	assert.Equal(t, "1080*1920", request.Parameters.Size)
	assert.Empty(t, request.Parameters.Resolution)
}

func TestConvertToAliRequestRejectsUnknownPixelSize(t *testing.T) {
	_, err := (&TaskAdaptor{}).convertToAliRequest(&relaycommon.RelayInfo{}, relaycommon.TaskSubmitReq{
		Model:  "wan2.5-i2v-preview",
		Prompt: "animate",
		Size:   "960x540",
	})

	require.ErrorContains(t, err, "unsupported Ali resolution")
}

func TestConvertToAliRequestMapsCanonicalMedia(t *testing.T) {
	testCases := []struct {
		name       string
		model      string
		images     []string
		audios     []string
		wantImgURL string
		wantFirst  string
		wantLast   string
		wantAudio  string
	}{
		{
			name:       "image and audio for wan 2.5 image to video",
			model:      "wan2.5-i2v-preview",
			images:     []string{"https://example.com/frame.png"},
			audios:     []string{"https://example.com/audio.mp3"},
			wantImgURL: "https://example.com/frame.png",
			wantAudio:  "https://example.com/audio.mp3",
		},
		{
			name:      "keyframe pair",
			model:     "wan2.2-kf2v-flash",
			images:    []string{"https://example.com/first.png", "https://example.com/last.png"},
			wantFirst: "https://example.com/first.png",
			wantLast:  "https://example.com/last.png",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := (&TaskAdaptor{}).convertToAliRequest(&relaycommon.RelayInfo{}, relaycommon.TaskSubmitReq{
				Model:  testCase.model,
				Prompt: "animate",
				Images: testCase.images,
				Audios: testCase.audios,
			})

			require.NoError(t, err)
			assert.Equal(t, testCase.wantImgURL, request.Input.ImgURL)
			assert.Equal(t, testCase.wantFirst, request.Input.FirstFrameURL)
			assert.Equal(t, testCase.wantLast, request.Input.LastFrameURL)
			assert.Equal(t, testCase.wantAudio, request.Input.AudioURL)
		})
	}
}

func TestConvertToAliRequestMapsCompatibleImageInputs(t *testing.T) {
	testCases := []struct {
		name      string
		request   relaycommon.TaskSubmitReq
		wantImg   string
		wantAudio string
	}{
		{
			name:    "singular image",
			request: relaycommon.TaskSubmitReq{Image: "https://example.com/image.png"},
			wantImg: "https://example.com/image.png",
		},
		{
			name:    "image urls alias",
			request: relaycommon.TaskSubmitReq{ImageURLs: []string{"https://example.com/image-url.png"}},
			wantImg: "https://example.com/image-url.png",
		},
		{
			name:    "input reference",
			request: relaycommon.TaskSubmitReq{InputReference: "https://example.com/input-reference.png"},
			wantImg: "https://example.com/input-reference.png",
		},
		{
			name:    "input start frames",
			request: relaycommon.TaskSubmitReq{InputStartFrames: []string{"https://example.com/start.png"}},
			wantImg: "https://example.com/start.png",
		},
		{
			name:    "input image references",
			request: relaycommon.TaskSubmitReq{InputImageReferences: []string{"https://example.com/reference.png"}},
			wantImg: "https://example.com/reference.png",
		},
		{
			name:    "metadata start frames",
			request: relaycommon.TaskSubmitReq{MetadataStartFrames: []string{"https://example.com/metadata-start.png"}},
			wantImg: "https://example.com/metadata-start.png",
		},
		{
			name: "content image",
			request: relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{
				{Type: "image_url", ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/content.png"}},
			}},
			wantImg: "https://example.com/content.png",
		},
		{
			name: "content audio",
			request: relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{
				{Type: "audio_url", AudioURL: &relaycommon.TaskContentURL{URL: "https://example.com/content.mp3"}},
			}},
			wantAudio: "https://example.com/content.mp3",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.request.Model = "wan2.5-i2v-preview"
			testCase.request.Prompt = "animate"
			request, err := (&TaskAdaptor{}).convertToAliRequest(&relaycommon.RelayInfo{}, testCase.request)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantImg, request.Input.ImgURL)
			assert.Equal(t, testCase.wantAudio, request.Input.AudioURL)
		})
	}
}

func TestValidateMappedRequestRejectsUnsupportedMediaBeforeBilling(t *testing.T) {
	testCases := []struct {
		name    string
		model   string
		request relaycommon.TaskSubmitReq
		message string
	}{
		{
			name:    "video references",
			model:   "wan2.5-i2v-preview",
			request: relaycommon.TaskSubmitReq{Videos: []string{"https://example.com/reference.mp4"}},
			message: "does not support video",
		},
		{
			name:  "content video references",
			model: "wan2.5-i2v-preview",
			request: relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{
				{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "https://example.com/reference.mp4"}},
			}},
			message: "does not support video",
		},
		{
			name:    "audio for silent model",
			model:   "wan2.2-i2v-flash",
			request: relaycommon.TaskSubmitReq{Audios: []string{"https://example.com/reference.mp3"}},
			message: "does not support audio",
		},
		{
			name:  "content audio for silent model",
			model: "wan2.2-i2v-flash",
			request: relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{
				{Type: "audio_url", AudioURL: &relaycommon.TaskContentURL{URL: "https://example.com/reference.mp3"}},
			}},
			message: "does not support audio",
		},
		{
			name:  "multiple images for image to video",
			model: "wan2.5-i2v-preview",
			request: relaycommon.TaskSubmitReq{Images: []string{
				"https://example.com/first.png",
				"https://example.com/last.png",
			}},
			message: "supports only one image",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("task_request", testCase.request)
			info := &relaycommon.RelayInfo{
				ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: testCase.model, IsModelMapped: true},
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			}

			taskErr := (&TaskAdaptor{}).ValidateMappedRequest(c, info)

			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "invalid_request", taskErr.Code)
			assert.Contains(t, taskErr.Message, testCase.message)
		})
	}
}

func TestValidateMappedRequestRejectsUnsupportedTextToVideoAspectRatioBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"wan2.5-t2v-preview",
		"prompt":"animate",
		"aspect_ratio":"3:4"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "wan2.5-t2v-preview"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, "unsupported Ali text-to-video aspect_ratio")
}

func TestValidateMappedRequestRejectsInvalidAliLegacyOutputBeforeBilling(t *testing.T) {
	testCases := []struct {
		name    string
		request relaycommon.TaskSubmitReq
		message string
	}{
		{
			name: "unsupported legacy size",
			request: relaycommon.TaskSubmitReq{
				Model:  "wan2.5-t2v-preview",
				Prompt: "animate",
				Size:   "999*999",
			},
			message: "invalid size",
		},
		{
			name: "legacy size conflicts with aspect ratio",
			request: relaycommon.TaskSubmitReq{
				Model:       "wan2.5-t2v-preview",
				Prompt:      "animate",
				Size:        "1920*1080",
				AspectRatio: "9:16",
			},
			message: "conflicts with aspect_ratio",
		},
		{
			name: "metadata output size",
			request: relaycommon.TaskSubmitReq{
				Model:  "wan2.5-t2v-preview",
				Prompt: "animate",
				Metadata: map[string]any{
					"parameters": map[string]any{"size": "999*999"},
				},
			},
			message: "invalid size",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("task_request", testCase.request)
			info := &relaycommon.RelayInfo{
				ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "wan2.5-t2v-preview", IsModelMapped: true},
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			}

			taskErr := (&TaskAdaptor{}).ValidateMappedRequest(c, info)

			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "invalid_video_output", taskErr.Code)
			assert.Contains(t, taskErr.Message, testCase.message)
		})
	}
}
