package vidu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadPrefersTopLevelResolution(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:     "a cat walks through a city",
		Size:       "960x540",
		Resolution: "1080p",
		Metadata:   map[string]any{"resolution": "720p"},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"}})

	require.NoError(t, err)
	assert.Equal(t, "1080p", payload.Resolution)
}

func TestConvertToRequestPayloadAcceptsRegisteredLegacySizes(t *testing.T) {
	testCases := []struct {
		name           string
		size           string
		wantResolution string
	}{
		{name: "landscape 720p alias", size: "1280x720", wantResolution: "720p"},
		{name: "portrait 720p alias", size: "720x1280", wantResolution: "720p"},
		{name: "landscape 1080p alias", size: "1920x1080", wantResolution: "1080p"},
		{name: "portrait 1080p alias", size: "1080x1920", wantResolution: "1080p"},
		{name: "legacy quality label", size: "720P", wantResolution: "720p"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
				Prompt: "a cat walks through a city",
				Size:   testCase.size,
			}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"}})

			require.NoError(t, err)
			assert.Equal(t, testCase.wantResolution, payload.Resolution)
		})
	}
}

func TestConvertToRequestPayloadRejectsUnknownPixelSize(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "a cat walks through a city",
		Size:   "960x540",
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"}})

	require.Error(t, err)
	assert.Nil(t, payload)
	assert.Contains(t, err.Error(), `size "960x540" is not supported by Vidu`)
}

func TestValidateMappedRequestRejectsUnknownPixelSizeBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"viduq2",
		"prompt":"a cat walks through a city",
		"size":"960x540"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, `size "960x540" is not supported by Vidu`)
}

func TestConvertToRequestPayloadMapsViduImageCompatibilityAliases(t *testing.T) {
	testCases := []struct {
		name    string
		request relaycommon.TaskSubmitReq
		url     string
	}{
		{name: "single image", request: relaycommon.TaskSubmitReq{Image: "https://example.com/image.png"}, url: "https://example.com/image.png"},
		{name: "image URLs", request: relaycommon.TaskSubmitReq{ImageURLs: []string{"https://example.com/image-urls.png"}}, url: "https://example.com/image-urls.png"},
		{name: "input reference", request: relaycommon.TaskSubmitReq{InputReference: "https://example.com/input-reference.png"}, url: "https://example.com/input-reference.png"},
		{name: "input start frames", request: relaycommon.TaskSubmitReq{InputStartFrames: []string{"https://example.com/start-frame.png"}}, url: "https://example.com/start-frame.png"},
		{name: "input image references", request: relaycommon.TaskSubmitReq{InputImageReferences: []string{"https://example.com/image-reference.png"}}, url: "https://example.com/image-reference.png"},
		{name: "metadata start frames", request: relaycommon.TaskSubmitReq{MetadataStartFrames: []string{"https://example.com/metadata-start.png"}}, url: "https://example.com/metadata-start.png"},
		{name: "content image", request: relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{{Type: "image_url", ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/content-image.png"}}}}, url: "https://example.com/content-image.png"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&testCase.request, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"},
			})

			require.NoError(t, err)
			assert.Equal(t, []string{testCase.url}, payload.Images)
		})
	}
}

func TestValidateRequestUsesViduImageAliasesForAction(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"viduq2",
		"prompt":"animate the two frames",
		"image_urls":["https://example.com/start.png", "https://example.com/end.png"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeVidu,
			UpstreamModelName: "viduq2",
		},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, constant.TaskActionFirstTailGenerate, info.Action)

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	var payload requestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, []string{"https://example.com/start.png", "https://example.com/end.png"}, payload.Images)
}

func TestValidateRequestUsesInputImageReferencesForViduReferenceAction(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"viduq2",
		"prompt":"animate the subject",
		"input":{"image_references":["https://example.com/subject.png"]}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeVidu,
			UpstreamModelName: "viduq2",
		},
	}
	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	assert.Equal(t, constant.TaskActionReferenceGenerate, info.Action)
}

func TestValidateMappedRequestRejectsUnsupportedViduMediaBeforeBilling(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "top level video and audio aliases",
			body: `{
				"model":"viduq2",
				"prompt":"animate",
				"video_url":"https://example.com/reference.mp4",
				"audios":["https://example.com/reference.mp3"]
			}`,
		},
		{
			name: "content video and audio",
			body: `{
				"model":"viduq2",
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

			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"}}
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
