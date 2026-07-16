package common

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	common "github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestTaskSubmitReqUnmarshalDurationWithSecondsSuffix(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"prompt":"cat","model":"grok-video-1.5","duration":"15s"}`), &req))
	require.Equal(t, 15, req.Duration)
}

func TestTaskSubmitReqUnmarshalMediaFieldsAcceptsScalarAndArray(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{
		"images":"https://example.com/image.png",
		"videos":["https://example.com/1.mp4","https://example.com/2.mp4"],
		"audios":"https://example.com/audio.mp3"
	}`), &req))

	assert.Equal(t, []string{"https://example.com/image.png"}, req.Images)
	assert.Equal(t, []string{"https://example.com/1.mp4", "https://example.com/2.mp4"}, req.Videos)
	assert.Equal(t, []string{"https://example.com/audio.mp3"}, req.Audios)
}

func TestTaskSubmitReqUnmarshalKeepsMediaAliasesSeparateWithoutDeduplication(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{
		"images":["same.png","same.png"],
		"image_urls":"alias.png",
		"input":{"start_frames":["start.png"],"image_references":["string-reference.png",{"url":"object-reference.png"}]},
		"metadata":{"start_frames":["metadata.png"]},
		"video":"video-1.mp4",
		"video_urls":["video-2.mp4"],
		"audio":"audio-1.mp3",
		"audio_url":"audio-1.mp3"
	}`), &req))

	assert.Equal(t, []string{"same.png", "same.png"}, req.Images)
	assert.Equal(t, []string{"alias.png"}, req.ImageURLs)
	assert.Equal(t, []string{"start.png"}, req.InputStartFrames)
	assert.Equal(t, []string{"string-reference.png", "object-reference.png"}, req.InputImageReferences)
	assert.Equal(t, []string{"metadata.png"}, req.MetadataStartFrames)
	assert.Empty(t, req.Videos)
	assert.Equal(t, []string{"video-1.mp4", "video-2.mp4"}, req.VideoURLs)
	assert.Empty(t, req.Audios)
	assert.Equal(t, []string{"audio-1.mp3", "audio-1.mp3"}, req.AudioURLs)
}

func TestTaskSubmitReqUnmarshalIgnoresBooleanAudioAlias(t *testing.T) {
	for _, audio := range []string{"true", "false"} {
		t.Run(audio, func(t *testing.T) {
			var req TaskSubmitReq
			require.NoError(t, common.Unmarshal([]byte(`{"prompt":"animate","audio":`+audio+`}`), &req))
			assert.Empty(t, req.AudioURLs)
		})
	}
}

func TestValidateBasicTaskRequestDoesNotDuplicateMultipartImages(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("model", "vidu"))
	require.NoError(t, writer.WriteField("images", "https://example.com/frame.png"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}

	require.Nil(t, ValidateBasicTaskRequest(c, info, "generate"))
	req, err := GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/frame.png"}, req.Images)
}
