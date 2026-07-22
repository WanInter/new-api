package gemini

import (
	"bytes"
	"io"
	"mime/multipart"
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

func TestBuildRequestBodyPrefersPublicOutputSpec(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:      "animate",
		Size:        "1280x720",
		AspectRatio: "16:9",
		Resolution:  "4K",
		Metadata: map[string]any{
			"aspectRatio": "9:16",
			"resolution":  "720p",
		},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload VeoRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	require.NotNil(t, payload.Parameters)
	assert.Equal(t, "16:9", payload.Parameters.AspectRatio)
	assert.Equal(t, "4k", payload.Parameters.Resolution)
}

func TestResolveVeoRequestOutputUsesOnlyRegisteredLegacySizes(t *testing.T) {
	testCases := []struct {
		name       string
		request    relaycommon.TaskSubmitReq
		model      string
		resolution string
		aspect     string
		errText    string
	}{
		{
			name:       "maps registered 720p landscape alias",
			request:    relaycommon.TaskSubmitReq{Size: "1280x720"},
			resolution: "720p",
			aspect:     "16:9",
		},
		{
			name:       "maps registered 4k portrait alias",
			request:    relaycommon.TaskSubmitReq{Size: "2160x3840"},
			model:      "veo-3.1-generate-preview",
			resolution: "4k",
			aspect:     "9:16",
		},
		{
			name:    "rejects arbitrary pixel size",
			request: relaycommon.TaskSubmitReq{Size: "960x540"},
			errText: "960x540",
		},
		{
			name:       "explicit resolution wins legacy size",
			request:    relaycommon.TaskSubmitReq{Size: "1280x720", Resolution: "1080p"},
			resolution: "1080p",
			aspect:     "16:9",
		},
		{
			name:       "explicit resolution ignores unknown legacy size",
			request:    relaycommon.TaskSubmitReq{Size: "960x540", Resolution: "720p"},
			resolution: "720p",
		},
		{
			name:    "rejects 4k on veo 3.0",
			request: relaycommon.TaskSubmitReq{Resolution: "4k"},
			model:   "veo-3.0-generate-001",
			errText: "Veo 3.1",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := ResolveVeoRequestOutput(&testCase.request, testCase.model)
			if testCase.errText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errText)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.resolution, output.Resolution)
			assert.Equal(t, testCase.aspect, output.AspectRatio)
		})
	}
}

func TestValidateMappedRequestRejectsUnknownSizeBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"veo-3.1-generate-preview",
		"prompt":"test",
		"size":"960x540"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "960x540")
}

func TestValidateMappedRequestRejectsHTTPImageURLBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"veo-3.1-generate-preview",
		"prompt":"test",
		"images":["https://example.com/ref.png"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_media_input", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Contains(t, taskErr.Message, "HTTP(S) URLs")
}

func TestValidateMappedRequestRejectsRepeatedImageInputs(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"veo-3.1-generate-preview",
		"prompt":"test",
		"images":["aGVsbG8=", "aGVsbG8="]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_media_input", taskErr.Code)
	assert.Contains(t, taskErr.Message, "at most one image")
}

func TestValidateMappedRequestAcceptsLegacyImageAlias(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"veo-3.1-generate-preview",
		"prompt":"test",
		"image":"iVBORw0KGgo="
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	assert.Equal(t, "generate", info.Action)
}

func TestValidateMappedRequestConsumesMultipartInputReferenceBeforeBilling(t *testing.T) {
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	require.NoError(t, writer.WriteField("model", "veo-3.1-generate-preview"))
	require.NoError(t, writer.WriteField("prompt", "test"))
	file, err := writer.CreateFormFile("input_reference", "reference.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("\x89PNG\r\n\x1a\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &requestBody)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	assert.Equal(t, "generate", info.Action)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload VeoRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Len(t, payload.Instances, 1)
	require.NotNil(t, payload.Instances[0].Image)
	assert.Equal(t, "image/png", payload.Instances[0].Image.MimeType)
}

func TestBuildRequestBodyConsumesImageURLsAlias(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:    "animate",
		ImageURLs: []string{"data:image/png;base64,iVBORw0KGgo="},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload VeoRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Len(t, payload.Instances, 1)
	require.NotNil(t, payload.Instances[0].Image)
	assert.Equal(t, "image/png", payload.Instances[0].Image.MimeType)
	assert.Equal(t, "iVBORw0KGgo=", payload.Instances[0].Image.BytesBase64Encoded)
}

func TestEstimateBillingPrefersPublicResolution(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Size:       "1280x720",
		Resolution: "4k",
		Metadata:   map[string]any{"resolution": "720p"},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-fast-generate-preview"},
	})
	assert.InDelta(t, 2.333333, ratios["resolution"], 0.000001)
}
