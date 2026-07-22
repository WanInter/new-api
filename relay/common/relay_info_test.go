package common

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	common "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

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

func TestTaskSubmitReqUnmarshalContentAcceptsCompatibleURLShapes(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{
		"content":[
			{"type":"image_url","role":"reference_image","name":"character","image_url":{"url":"https://example.com/image.png"}},
			{"type":"video_url","role":"reference_video","video_url":"https://example.com/video.mp4"},
			{"type":"audio_url","url":"https://example.com/audio.mp3"},
			{"type":"text","text":"animate the references"}
		]
	}`), &req))

	require.Len(t, req.Content, 4)
	require.NotNil(t, req.Content[0].ImageURL)
	assert.Equal(t, "https://example.com/image.png", req.Content[0].ImageURL.URL)
	assert.Equal(t, "reference_image", req.Content[0].Role)
	assert.Equal(t, "character", req.Content[0].Name)
	require.NotNil(t, req.Content[1].VideoURL)
	assert.Equal(t, "https://example.com/video.mp4", req.Content[1].VideoURL.URL)
	assert.Equal(t, "reference_video", req.Content[1].Role)
	require.NotNil(t, req.Content[2].AudioURL)
	assert.Equal(t, "https://example.com/audio.mp3", req.Content[2].AudioURL.URL)
	assert.Equal(t, "animate the references", req.Content[3].Text)

	body, err := common.Marshal(req.Content)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"video_url":{"url":"https://example.com/video.mp4"}`)
	assert.Contains(t, string(body), `"audio_url":{"url":"https://example.com/audio.mp3"}`)
	assert.Contains(t, string(body), `"role":"reference_image"`)
	assert.Contains(t, string(body), `"name":"character"`)
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

func TestValidateBasicTaskRequestParsesMultipartMediaFields(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("model", "video-model"))
	require.NoError(t, writer.WriteField("images", "https://example.com/frame.png"))
	require.NoError(t, writer.WriteField("image_urls", "https://example.com/legacy-frame.png"))
	require.NoError(t, writer.WriteField("videos", "https://example.com/motion-1.mp4"))
	require.NoError(t, writer.WriteField("videos", "https://example.com/motion-2.mp4"))
	require.NoError(t, writer.WriteField("video_url", "https://example.com/legacy-motion.mp4"))
	require.NoError(t, writer.WriteField("audios", "https://example.com/music.mp3"))
	require.NoError(t, writer.WriteField("audio_urls", "https://example.com/legacy-music.mp3"))
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
	assert.Equal(t, []string{"https://example.com/legacy-frame.png"}, req.ImageURLs)
	assert.Equal(t, []string{"https://example.com/motion-1.mp4", "https://example.com/motion-2.mp4"}, req.Videos)
	assert.Equal(t, []string{"https://example.com/legacy-motion.mp4"}, req.VideoURLs)
	assert.Equal(t, []string{"https://example.com/music.mp3"}, req.Audios)
	assert.Equal(t, []string{"https://example.com/legacy-music.mp3"}, req.AudioURLs)
	for _, field := range []string{
		"images", "image_urls", "videos", "video_url", "audios", "audio_urls",
	} {
		assert.NotContains(t, req.Metadata, field)
	}
}

func TestValidateTaskMultipartFiles(t *testing.T) {
	testCases := []struct {
		name        string
		channelType int
		field       string
		wantError   bool
	}{
		{name: "generic image file is rejected", channelType: constant.ChannelTypeVidu, field: "images", wantError: true},
		{name: "generic video file is rejected", channelType: constant.ChannelTypeVidu, field: "videos", wantError: true},
		{name: "generic audio file is rejected", channelType: constant.ChannelTypeVidu, field: "audios", wantError: true},
		{name: "generic legacy input reference is rejected", channelType: constant.ChannelTypeAli, field: "input_reference", wantError: true},
		{name: "openai sora-compatible channel allows standard image files", channelType: constant.ChannelTypeOpenAI, field: "images"},
		{name: "sora allows standard image files", channelType: constant.ChannelTypeSora, field: "images"},
		{name: "shishi allows standard video files", channelType: constant.ChannelTypeShishi, field: "videos"},
		{name: "sora allows legacy input reference files", channelType: constant.ChannelTypeSora, field: "input_reference"},
		{name: "openai rejects unknown binary files", channelType: constant.ChannelTypeOpenAI, field: "unknown", wantError: true},
		{name: "sora rejects unknown binary files", channelType: constant.ChannelTypeSora, field: "unknown", wantError: true},
		{name: "shishi rejects unknown binary files", channelType: constant.ChannelTypeShishi, field: "unknown", wantError: true},
		{name: "gemini allows its image input field", channelType: constant.ChannelTypeGemini, field: "input_reference"},
		{name: "jimeng allows its image input field", channelType: constant.ChannelTypeJimeng, field: "input_reference"},
		{name: "dimensio allows numbered image file", channelType: constant.ChannelTypeJimengDimensio, field: "image_file_9"},
		{name: "dimensio rejects out of range image file", channelType: constant.ChannelTypeJimengDimensio, field: "image_file_10", wantError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			file, err := writer.CreateFormFile(testCase.field, "reference.bin")
			require.NoError(t, err)
			_, err = file.Write([]byte("reference"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: testCase.channelType}}

			err = ValidateTaskMultipartFiles(c, info)
			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.field)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateTaskMultipartFilesAllowsURLValues(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("images", "https://example.com/frame.png"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	require.NoError(t, ValidateTaskMultipartFiles(c, &RelayInfo{
		ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeVidu},
	}))
}
