package jimeng

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

func TestConvertToRequestPayloadUsesCanonicalAspectRatioWithoutMetadata(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:      "animate a city",
		AspectRatio: "16:9",
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"}})

	require.NoError(t, err)
	require.Equal(t, "16:9", payload.AspectRatio)
}

func TestBuildRequestBodyMapsUnifiedJimengImageAliases(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"jimeng_vgfm_t2v_l20",
		"prompt":"animate the references",
		"image_urls":"https://example.com/legacy.png",
		"input_reference":"https://example.com/reference.png",
		"content":[{
			"type":"image_url",
			"image_url":{"url":"https://example.com/content.png"}
		}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	body, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload requestPayload
	require.NoError(t, common.Unmarshal(body, &payload))
	assert.Equal(t, []string{
		"https://example.com/legacy.png",
		"https://example.com/reference.png",
		"https://example.com/content.png",
	}, payload.ImageUrls)
	assert.Empty(t, payload.BinaryDataBase64)
}

func TestConvertToRequestPayloadNormalizesJimengDataURI(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:         "animate",
		InputReference: "data:image/png;base64,aGVsbG8=",
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"}})

	require.NoError(t, err)
	assert.Empty(t, payload.ImageUrls)
	assert.Equal(t, []string{"aGVsbG8="}, payload.BinaryDataBase64)
}

func TestValidateMappedRequestRejectsUnsupportedJimengVideoAndAudioInputs(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "top level media aliases",
			body: `{
				"model":"jimeng_vgfm_t2v_l20",
				"prompt":"animate",
				"videos":"https://example.com/reference.mp4",
				"audio_url":"https://example.com/reference.mp3"
			}`,
		},
		{
			name: "content video and audio",
			body: `{
				"model":"jimeng_vgfm_t2v_l20",
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
			assert.Equal(t, "invalid_media_input", taskErr.Code)
			assert.Contains(t, taskErr.Message, "does not support video or audio")
		})
	}
}

func TestBuildRequestBodyDefensivelyRejectsUnsupportedJimengMedia(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"jimeng_vgfm_t2v_l20",
		"prompt":"animate",
		"videos":["https://example.com/reference.mp4"],
		"content":[{
			"type":"audio_url",
			"audio_url":{"url":"https://example.com/reference.mp3"}
		}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.Error(t, err)
	assert.Nil(t, requestBody)
	assert.Contains(t, err.Error(), "does not support video or audio")
}

func TestConvertToRequestPayloadRejectsMixedJimengURLAndBase64Images(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:    "animate",
		Images:    []string{"https://example.com/start.png"},
		ImageURLs: []string{"aGVsbG8="},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"}})

	require.Error(t, err)
	assert.Nil(t, payload)
	assert.Contains(t, err.Error(), "mixing HTTP(S) image URLs with base64")
}

func TestBuildRequestBodyRejectsMixedJimengURLAndMultipartImage(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "jimeng_vgfm_t2v_l20"))
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("images", "https://example.com/start.png"))
	file, err := writer.CreateFormFile("input_reference", "reference.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("reference image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_media_input", taskErr.Code)
	assert.Contains(t, taskErr.Message, "mixing HTTP(S) image URLs with base64")

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.Error(t, err)
	assert.Nil(t, requestBody)
	assert.Contains(t, err.Error(), "mixing HTTP(S) image URLs with base64")
}
