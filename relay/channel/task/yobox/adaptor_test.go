package yobox

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertToRequestPayloadSeedance2UsesInputReferenceAlias(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:          "seedance2",
		Prompt:         "dance",
		Seconds:        "12",
		Size:           "720x1280",
		InputReference: "https://example.com/ref.png",
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"input_reference":"https://example.com/ref.png"`)
	require.Contains(t, string(body), `"seconds":"12"`)
}

func TestConvertToRequestPayloadSeedance20UsesImageReferencesWithoutAssumedStrength(t *testing.T) {
	testCases := []struct {
		name     string
		model    string
		images   []string
		expected []map[string]any
	}{
		{
			name:     "one image",
			model:    "seedance-2.0",
			images:   []string{"https://example.com/1.png"},
			expected: []map[string]any{{"url": "https://example.com/1.png"}},
		},
		{
			name:   "two images",
			model:  "seedance-2.0",
			images: []string{"https://example.com/1.png", "https://example.com/2.png"},
			expected: []map[string]any{
				{"url": "https://example.com/1.png"},
				{"url": "https://example.com/2.png"},
			},
		},
		{
			name:   "three images with fast model",
			model:  "seedance-2.0-fast",
			images: []string{"https://example.com/1.png", "https://example.com/2.png", "https://example.com/3.png"},
			expected: []map[string]any{
				{"url": "https://example.com/1.png"},
				{"url": "https://example.com/2.png"},
				{"url": "https://example.com/3.png"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
				Model:    testCase.model,
				Prompt:   "run",
				Duration: 6,
				Images:   testCase.images,
			}, &relaycommon.RelayInfo{})
			require.NoError(t, err)

			body, ok := payload.(map[string]any)
			require.True(t, ok)
			input, ok := body["input"].(map[string]any)
			require.True(t, ok)

			assert.Equal(t, testCase.expected, input["image_references"])
			assert.NotContains(t, input, "start_frames")
			assert.NotContains(t, input, "end_frames")
		})
	}
}

func TestConvertToRequestPayloadSeedance20ForwardsVideoAndAudioReferences(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:     "seedance-2.0",
		Prompt:    "animate the cat",
		Duration:  5,
		Videos:    []string{"https://example.com/reference.mp4"},
		VideoURLs: []string{"https://example.com/legacy-reference.mp4"},
		Audios:    []string{"https://example.com/reference.mp3"},
		AudioURLs: []string{"https://example.com/legacy-reference.mp3"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []map[string]any{
		{"url": "https://example.com/reference.mp4"},
		{"url": "https://example.com/legacy-reference.mp4"},
	}, input["video_references"])
	assert.Equal(t, []map[string]any{
		{"url": "https://example.com/reference.mp3"},
		{"url": "https://example.com/legacy-reference.mp3"},
	}, input["audio_references"])
	assert.NotContains(t, input, "audio")
}

func TestConvertToRequestPayloadSeedance20ForwardsContentMediaReferences(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0",
		Prompt:   "animate the character",
		Duration: 5,
		Content: []relaycommon.TaskContentItem{
			{Type: "image_url", ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/reference.png"}},
			{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "https://example.com/motion.mp4"}},
			{Type: "audio_url", AudioURL: &relaycommon.TaskContentURL{URL: "https://example.com/music.mp3"}},
		},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []map[string]any{{"url": "https://example.com/reference.png"}}, input["image_references"])
	assert.Equal(t, []map[string]any{{"url": "https://example.com/motion.mp4"}}, input["video_references"])
	assert.Equal(t, []map[string]any{{"url": "https://example.com/music.mp3"}}, input["audio_references"])
}

func TestValidateMappedRequestRejectsVideoAudioForMappedSeedance2(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "top level media",
			body: `{
				"model":"public-seedance",
				"prompt":"animate",
				"videos":["https://example.com/reference.mp4"],
				"audios":["https://example.com/reference.mp3"]
			}`,
		},
		{
			name: "content media",
			body: `{
				"model":"public-seedance",
				"prompt":"animate",
				"content":[{"type":"video_url","video_url":{"url":"https://example.com/reference.mp4"}}]
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
				OriginModelName: "public-seedance",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}
			adaptor := &TaskAdaptor{}

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "seedance2", IsModelMapped: true}

			taskErr := adaptor.ValidateMappedRequest(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "invalid_request", taskErr.Code)
			assert.Contains(t, taskErr.Message, "does not support video or audio")
		})
	}
}

func TestValidateMappedRequestAllowsVideoAudioForMappedSeedance20(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-seedance",
		"prompt":"animate",
		"videos":["https://example.com/reference.mp4"],
		"audios":["https://example.com/reference.mp3"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "seedance-2.0", IsModelMapped: true}

	assert.Nil(t, adaptor.ValidateMappedRequest(c, info))
}

func TestValidateMappedRequestRejectsUnsupportedSizeForMappedSeedance20(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-seedance",
		"prompt":"animate",
		"size":"960x540"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "seedance-2.0", IsModelMapped: true}

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, `960x540`)
}

func TestConvertToRequestPayloadNoFaceOmitsMediaStrength(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0-fast-noface",
		Prompt:   "animate the cat",
		Duration: 5,
		Images:   []string{"https://example.com/reference.png"},
		Videos:   []string{"https://example.com/reference.mp4"},
		Audios:   []string{"https://example.com/reference.mp3"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []map[string]any{{"url": "https://example.com/reference.png"}}, input["image_references"])
	assert.Equal(t, []map[string]any{{"url": "https://example.com/reference.mp4"}}, input["video_references"])
	assert.Equal(t, []map[string]any{{"url": "https://example.com/reference.mp3"}}, input["audio_references"])
}

func TestConvertToRequestPayloadSeedance20DefaultsRequiredResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0",
		Prompt:   "run",
		Metadata: map[string]any{"aspect_ratio": "9:16"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "9:16", input["aspect_ratio"])
	assert.NotContains(t, input, "duration")
	assert.Equal(t, "720p", input["resolution"])
	assert.NotContains(t, input, "audio")
	assert.NotContains(t, input, "n")
}

func TestConvertToRequestPayloadSeedance20CanonicalizesAspectRatioAliases(t *testing.T) {
	testCases := []struct {
		name     string
		metadata map[string]any
		expected string
	}{
		{name: "legacy ratio", metadata: map[string]any{"ratio": "9:16"}, expected: "9:16"},
		{name: "camel case alias", metadata: map[string]any{"aspectRatio": "16:9"}, expected: "16:9"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
				Model:    "seedance-2.0-noface",
				Prompt:   "run",
				Metadata: testCase.metadata,
			}, &relaycommon.RelayInfo{})
			require.NoError(t, err)

			body, ok := payload.(map[string]any)
			require.True(t, ok)
			input, ok := body["input"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, testCase.expected, input["aspect_ratio"])
			assert.NotContains(t, input, "ratio")
			assert.NotContains(t, input, "aspectRatio")
		})
	}
}

func TestValidateSeedance20RejectsUnsupportedSize(t *testing.T) {
	testCases := []struct {
		name     string
		bodyJSON string
	}{
		{
			name:     "no explicit parameters",
			bodyJSON: `{"model":"seedance-2.0-noface","prompt":"test","seconds":"5","size":"960x540"}`,
		},
		{
			name:     "with matching explicit output fields",
			bodyJSON: `{"model":"seedance-2.0-noface","prompt":"test","size":"960x540","aspect_ratio":"16:9","resolution":"540p"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: "seedance-2.0-noface",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_video_output", taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Contains(t, taskErr.Message, `960x540`)
			assert.Contains(t, taskErr.Message, "not supported")
		})
	}
}

func TestValidateSeedance20RejectsUnknownSizeWithExplicitResolution(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-2.0-noface",
		"prompt":"test",
		"seconds":"5",
		"size":"960x540",
		"aspect_ratio":"16:9",
		"resolution":"720p"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-noface",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, `960x540`)
	assert.Contains(t, taskErr.Message, "not supported")
}

func TestValidateSeedance20RejectsUnknownNestedSizeWithoutExplicitParameters(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-2.0-noface",
		"prompt":"test",
		"input":{"size":"960x540"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-noface",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, `960x540`)
	assert.Contains(t, taskErr.Message, "not supported")
}

func TestValidateSeedance20ConvertsKnownSizeAliases(t *testing.T) {
	testCases := []struct {
		name        string
		size        string
		nested      bool
		aspectRatio string
		resolution  string
	}{
		{name: "portrait", size: "720x1280", aspectRatio: "9:16", resolution: "720p"},
		{name: "landscape", size: "1280x720", aspectRatio: "16:9", resolution: "720p"},
		{name: "square", size: "720x720", aspectRatio: "1:1", resolution: "720p"},
		{name: "nested portrait", size: "720x1280", nested: true, aspectRatio: "9:16", resolution: "720p"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			bodyJSON := fmt.Sprintf(`{"model":"seedance-2.0-noface","prompt":"test","size":"%s"}`, testCase.size)
			if testCase.nested {
				bodyJSON = fmt.Sprintf(`{"model":"seedance-2.0-noface","prompt":"test","input":{"size":"%s"}}`, testCase.size)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: "seedance-2.0-noface",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
			req, err := relaycommon.GetTaskRequest(c)
			require.NoError(t, err)
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req, info)
			require.NoError(t, err)

			body, ok := payload.(map[string]any)
			require.True(t, ok)
			input, ok := body["input"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, testCase.aspectRatio, input["aspect_ratio"])
			assert.Equal(t, testCase.resolution, input["resolution"])
		})
	}
}

func TestConvertToRequestPayloadHappyHorseSupportsNineReferences(t *testing.T) {
	images := make([]string, 9)
	for i := range images {
		images[i] = fmt.Sprintf("https://example.com/%d.png", i+1)
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "characters interact",
		Duration: 15,
		Images:   images,
		Metadata: map[string]any{
			"aspect_ratio":   "9:16",
			"resolution":     "1080p",
			"prompt_enhance": "AUTO",
		},
	}, &relaycommon.RelayInfo{OriginModelName: "happy-horse-1.1"})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "happy-horse-1.1", body["model"])
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	refs, ok := input["image_references"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, refs, 9)
	assert.Equal(t, "1080p", input["resolution"])
	assert.Equal(t, "AUTO", input["prompt_enhance"])
}

func TestConvertToRequestPayloadPreservesAllStartFrames(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "character smiles",
		Duration: 6,
		Metadata: map[string]any{
			"start_frames": []any{"https://example.com/start.png", "https://example.com/ignored.png"},
		},
	}, &relaycommon.RelayInfo{OriginModelName: "happy-horse-1.1"})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"https://example.com/start.png", "https://example.com/ignored.png"}, input["start_frames"])
	assert.NotContains(t, input, "image_references")
}

func TestValidateHappyHorseMetadataStartFramesPreservesDedicatedPayload(t *testing.T) {
	bodyJSON := `{
		"model":"happy-horse-1.1",
		"prompt":"character smiles",
		"metadata":{"start_frames":["https://example.com/start.png","https://example.com/ignored.png"]}
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "happy-horse-1.1",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Empty(t, req.Images)
	assert.Equal(t, []string{"https://example.com/start.png", "https://example.com/ignored.png"}, req.MetadataStartFrames)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req, info)
	require.NoError(t, err)
	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"https://example.com/start.png", "https://example.com/ignored.png"}, input["start_frames"])
	assert.NotContains(t, input, "image_references")
}

func TestValidateSeedance20PreservesBooleanAudioSwitch(t *testing.T) {
	for _, audio := range []bool{true, false} {
		t.Run(fmt.Sprintf("audio_%t", audio), func(t *testing.T) {
			bodyJSON := fmt.Sprintf(`{"model":"seedance-2.0","prompt":"dance","audio":%t}`, audio)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: "seedance-2.0",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
			req, err := relaycommon.GetTaskRequest(c)
			require.NoError(t, err)
			assert.Empty(t, req.Audios)
			assert.Empty(t, req.AudioURLs)

			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req, info)
			require.NoError(t, err)
			body, ok := payload.(map[string]any)
			require.True(t, ok)
			input, ok := body["input"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, audio, input["audio"])
		})
	}
}

func TestValidateNoFacePreservesFramesMediaAndExtensions(t *testing.T) {
	bodyJSON := `{
		"model":"seedance-2.0-fast-noface",
		"prompt":"animate every reference",
		"duration":15,
		"aspect_ratio":"9:16",
		"resolution":"4k",
		"images":["https://example.com/1.png","https://example.com/2.png","https://example.com/3.png","https://example.com/4.png","https://example.com/5.png","https://example.com/6.png","https://example.com/7.png","https://example.com/8.png","https://example.com/9.png"],
		"videos":["https://example.com/1.mp4","https://example.com/2.mp4","https://example.com/3.mp4","https://example.com/4.mp4"],
		"audios":["https://example.com/1.mp3","https://example.com/2.mp3","https://example.com/3.mp3","https://example.com/4.mp3"],
		"metadata":{"custom_extension":{"enabled":true}},
		"input":{
			"start_frames":["https://example.com/start-1.png","https://example.com/start-2.png"],
			"end_frames":["https://example.com/end-1.png","https://example.com/end-2.png"],
			"audio":false,
			"n":0
		}
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-fast-noface",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req, info)
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 15, input["duration"])
	assert.Equal(t, "9:16", input["aspect_ratio"])
	assert.Equal(t, "4k", input["resolution"])
	assert.Len(t, input["image_references"], 9)
	assert.Len(t, input["video_references"], 4)
	assert.Len(t, input["audio_references"], 4)
	assert.Equal(t, []any{"https://example.com/start-1.png", "https://example.com/start-2.png"}, input["start_frames"])
	assert.Equal(t, []any{"https://example.com/end-1.png", "https://example.com/end-2.png"}, input["end_frames"])
	assert.Equal(t, false, input["audio"])
	assert.Equal(t, float64(0), input["n"])
	assert.Equal(t, map[string]any{"enabled": true}, input["custom_extension"])

	for _, reference := range input["video_references"].([]map[string]any) {
		assert.NotContains(t, reference, "strength")
	}
}

func TestValidateSeedance20PreservesExplicitZeroDuration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-2.0",
		"prompt":"run",
		"duration":0
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req, info)
	require.NoError(t, err)

	body := payload.(map[string]any)
	input := body["input"].(map[string]any)
	assert.EqualValues(t, 0, input["duration"])
}

func TestConvertToRequestPayloadUsesMappedUpstreamModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0-yo",
		Prompt:   "run",
		Duration: 15,
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		UpstreamModelName: "seedance-2.0",
		IsModelMapped:     true,
	}})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "seedance-2.0", body["model"])
	require.Contains(t, body, "input")
}

func TestParseTaskResultExtractsOutputsVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{
		"task_id":"task_1",
		"status":"SUCCESS",
		"data":{
			"video_url":"https://example.com/out.mp4",
			"progress":100
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, info.Status)
	require.Equal(t, "https://example.com/out.mp4", info.Url)
}

func TestParseTaskResultExtractsNestedSeedance20Outputs(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{
		"success": true,
		"message": "",
		"data": {
			"task_id": "task_nested",
			"status": "SUCCESS",
			"progress": 100,
			"fail_reason": "",
			"data": {
				"id": "task_nested",
				"status": "completed",
				"phase": "completed",
				"outputs": ["https://example.com/out.mp4"]
			}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, "task_nested", info.TaskID)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/out.mp4", info.Url)
	require.Equal(t, "100%", info.Progress)
}

func TestParseTaskResultExtractsNestedFailureReason(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{
		"success": true,
		"message": "",
		"data": {
			"task_id": "task_failed",
			"status": "FAILURE",
			"progress": 100,
			"fail_reason": "下载图片失败，HTTP 404",
			"data": {
				"status": "failed",
				"phase": "failed",
				"error": "下载图片失败，HTTP 404"
			}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, "task_failed", info.TaskID)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Equal(t, "下载图片失败，HTTP 404", info.Reason)
	require.Equal(t, "100%", info.Progress)
}

func TestParseTaskResultExtractsNestedObjectFailureReason(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"success": true,
		"message": "",
		"data": {
			"task_id": "task_failed",
			"status": "FAILURE",
			"progress": 100,
			"data": {
				"status": "failed",
				"phase": "failed",
				"error": {
					"code": "upstream_error",
					"message": "reference video could not be processed"
				}
			}
		}
	}`))
	require.NoError(t, err)

	assert.Equal(t, "task_failed", info.TaskID)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "reference video could not be processed", info.Reason)
	assert.Equal(t, "100%", info.Progress)
}

func TestParseTaskResultRejectsUnknownStatus(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"task_id":"task_unknown","status":"pausing"}`))

	require.Error(t, err)
	assert.Nil(t, info)
}

func TestDoResponsePreservesImageReferencesLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(`{
			"success": false,
			"message": "最多支持 4 张 image_references"
		}`)),
	}

	_, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{})
	require.NotNil(t, taskErr)
	assert.Equal(t, "yobox submit failed: 最多支持 4 张 image_references", taskErr.Message)
}

func TestSanitizeTaskUpstreamErrorPreservesUpstreamBody(t *testing.T) {
	body := []byte(`{"success":false,"message":"{\"error\":\"sd-bak-3 最多支持 4 张 image_references\"}"}`)

	message := (&TaskAdaptor{}).SanitizeTaskUpstreamError(body)

	assert.Equal(t, string(body), message)
}

func TestParseTaskResultPreservesImageReferencesLimit(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status": "FAILURE",
		"fail_reason": "最多支持 4 张 image_references"
	}`))
	require.NoError(t, err)

	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "最多支持 4 张 image_references", info.Reason)
}

func TestConvertToOpenAIVideoIncludesResultURL(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  100,
		UpdatedAt:  200,
		Properties: model.Properties{OriginModelName: "seedance-2.0-yo"},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://example.com/out.mp4",
		},
	}
	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &video))
	require.Equal(t, "task_public", video.ID)
	require.Equal(t, dto.VideoStatusCompleted, video.Status)
	require.Equal(t, "https://example.com/out.mp4", video.Metadata["url"])
	require.Equal(t, "https://example.com/out.mp4", video.Metadata["video_url"])
	require.Equal(t, "https://example.com/out.mp4", video.Metadata["result_url"])
}

func TestConvertToOpenAIVideoExtractsNestedOutputFallback(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  100,
		UpdatedAt:  200,
		Properties: model.Properties{OriginModelName: "seedance-2.0-yo"},
		Data:       []byte(`{"success":true,"data":{"data":{"outputs":["https://example.com/nested.mp4"]}}}`),
	}
	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &video))
	require.Equal(t, "https://example.com/nested.mp4", video.Metadata["url"])
}

func TestConvertToRequestPayloadSeedance2PreservesExplicitContentAndExtensions(t *testing.T) {
	content := []any{
		map[string]any{"type": "text", "text": "prompt", "custom_text_option": true},
		map[string]any{"type": "image_url", "role": "reference_image", "image_url": map[string]any{"url": "https://example.com/1.png"}},
	}
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:  "seedance2",
		Prompt: "prompt",
		Images: []string{
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png",
		},
		Metadata: map[string]any{
			"content":        content,
			"generate_audio": true,
			"custom_option":  map[string]any{"enabled": true},
		},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, content, body["content"])
	assert.Equal(t, true, body["generate_audio"])
	assert.Equal(t, map[string]any{"enabled": true}, body["custom_option"])
}

func TestBuildBillingInputMatchesSeedance20ResolvedPayload(t *testing.T) {
	testCases := []struct {
		name               string
		newContext         func(t *testing.T) *gin.Context
		upstreamDuration   any
		upstreamResolution string
		duration           int
		resolution         string
	}{
		{
			name: "json nested input overrides metadata duration",
			newContext: func(t *testing.T) *gin.Context {
				body := `{
					"model":"seedance-2.0-noface",
					"prompt":"animate",
					"metadata":{"duration":5},
					"input":{"duration":15,"resolution":"1080P"}
				}`
				return newYoboxBillingTestContext(t, "application/json", strings.NewReader(body))
			},
			upstreamDuration:   float64(15),
			upstreamResolution: "1080p",
			duration:           15,
			resolution:         "1080p",
		},
		{
			name: "json duration string with unit",
			newContext: func(t *testing.T) *gin.Context {
				body := `{"model":"seedance-2.0-fast-noface","prompt":"animate","duration":" 15 seconds ","resolution":"720P"}`
				return newYoboxBillingTestContext(t, "application/json", strings.NewReader(body))
			},
			upstreamDuration:   " 15 seconds ",
			upstreamResolution: "720p",
			duration:           15,
			resolution:         "720p",
		},
		{
			name: "url encoded form",
			newContext: func(t *testing.T) *gin.Context {
				values := url.Values{
					"model":      {"seedance-2.0-noface"},
					"prompt":     {"animate"},
					"duration":   {"15"},
					"resolution": {"720P"},
				}
				return newYoboxBillingTestContext(t, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
			},
			upstreamDuration:   "15",
			upstreamResolution: "720p",
			duration:           15,
			resolution:         "720p",
		},
		{
			name: "multipart encoded input object",
			newContext: func(t *testing.T) *gin.Context {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("model", "seedance-2.0-noface"))
				require.NoError(t, writer.WriteField("prompt", "animate"))
				require.NoError(t, writer.WriteField("input", `{"duration":15,"resolution":"480P"}`))
				require.NoError(t, writer.Close())
				return newYoboxBillingTestContext(t, writer.FormDataContentType(), bytes.NewReader(body.Bytes()))
			},
			upstreamDuration:   float64(15),
			upstreamResolution: "480p",
			duration:           15,
			resolution:         "480p",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := testCase.newContext(t)
			info := &relaycommon.RelayInfo{
				OriginModelName: "seedance-2.0-noface",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}
			adaptor := &TaskAdaptor{}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			billingInput, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			req, err := relaycommon.GetTaskRequest(c)
			require.NoError(t, err)
			payload, err := adaptor.convertToRequestPayload(&req, info)
			require.NoError(t, err)
			upstreamInput := payload.(map[string]any)["input"].(map[string]any)

			assert.Equal(t, testCase.upstreamDuration, upstreamInput["duration"])
			assert.Equal(t, testCase.upstreamResolution, upstreamInput["resolution"])
			assert.EqualValues(t, testCase.duration, gjson.GetBytes(billingInput.Body, "billing.duration_seconds").Int())
			assert.Equal(t, testCase.resolution, gjson.GetBytes(billingInput.Body, "billing.resolution").String())
			assert.Equal(t, map[string]float64{"seconds": float64(testCase.duration)}, adaptor.EstimateBilling(c, info))
		})
	}
}

func TestBuildBillingInputDefaultsRequiredResolutionForMappedSeedance20(t *testing.T) {
	c := newYoboxBillingTestContext(t, "application/json", strings.NewReader(`{
		"model":"seedance-2.0-fast-yo",
		"prompt":"animate",
		"duration":15,
		"aspect_ratio":"9:16"
	}`))
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-fast-yo",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "seedance-2.0-fast",
			IsModelMapped:     true,
		},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	billingInput, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	payload, err := adaptor.convertToRequestPayload(&req, info)
	require.NoError(t, err)
	upstreamInput := payload.(map[string]any)["input"].(map[string]any)

	assert.Equal(t, "seedance-2.0-fast", payload.(map[string]any)["model"])
	assert.EqualValues(t, 15, upstreamInput["duration"])
	assert.Equal(t, "9:16", upstreamInput["aspect_ratio"])
	assert.Equal(t, "720p", upstreamInput["resolution"])
	assert.EqualValues(t, 15, gjson.GetBytes(billingInput.Body, "billing.duration_seconds").Int())
	assert.Equal(t, "720p", gjson.GetBytes(billingInput.Body, "billing.resolution").String())
	assert.Equal(t, map[string]float64{"seconds": 15}, adaptor.EstimateBilling(c, info))
}

func TestBuildBillingInputTreatsZeroDurationAsInvalidForBilling(t *testing.T) {
	testCases := []struct {
		name             string
		contentType      string
		body             string
		expectedDuration any
	}{
		{
			name:             "numeric JSON zero",
			contentType:      "application/json",
			body:             `{"model":"seedance-2.0-noface","prompt":"animate","duration":0,"resolution":"720p"}`,
			expectedDuration: float64(0),
		},
		{
			name:             "url encoded string zero",
			contentType:      "application/x-www-form-urlencoded",
			body:             "model=seedance-2.0-noface&prompt=animate&duration=0&resolution=720p",
			expectedDuration: "0",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newYoboxBillingTestContext(t, testCase.contentType, strings.NewReader(testCase.body))
			info := &relaycommon.RelayInfo{
				OriginModelName: "seedance-2.0-noface",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}
			adaptor := &TaskAdaptor{}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			billingInput, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			req, err := relaycommon.GetTaskRequest(c)
			require.NoError(t, err)
			payload, err := adaptor.convertToRequestPayload(&req, info)
			require.NoError(t, err)
			upstreamInput := payload.(map[string]any)["input"].(map[string]any)

			assert.Equal(t, testCase.expectedDuration, upstreamInput["duration"])
			assert.False(t, gjson.GetBytes(billingInput.Body, "billing.duration_seconds").Exists())
			assert.Equal(t, "720p", gjson.GetBytes(billingInput.Body, "billing.resolution").String())
			assert.Equal(t, map[string]float64{"seconds": 4}, adaptor.EstimateBilling(c, info))
		})
	}
}

func newYoboxBillingTestContext(t *testing.T, contentType string, body io.Reader) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", body)
	c.Request.Header.Set("Content-Type", contentType)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestModelListIncludesSupportedModels(t *testing.T) {
	require.Equal(t, []string{"seedance2", "seedance2-pro", "seedance-2.0", "seedance-2.0-fast", "seedance-2.0-noface", "seedance-2.0-fast-noface", "happy-horse-1.1"}, (&TaskAdaptor{}).GetModelList())
}
