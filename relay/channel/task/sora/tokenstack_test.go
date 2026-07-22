package sora

import (
	"io"
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

func TestBuildRequestBodyMapsTokenStackDurationWithoutChangingValue(t *testing.T) {
	bodyJSON := `{
		"duration":5,
		"images":["image-1","image-2","image-3","image-4"],
		"model":"sd-bak-1",
		"prompt":"animate the references",
		"ratio":"9:16",
		"resolution":"720p",
		"size":"720x1280",
		"unsupported":"drop-me"
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://www.tokenstack.cc"))
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, map[string]any{
		"images":      []any{"image-1", "image-2", "image-3", "image-4"},
		"model":       "seedance-2-0-15s-slow",
		"prompt":      "animate the references",
		"ratio":       "9:16",
		"resolution":  "720p",
		"seconds":     "5",
		"size":        "720x1280",
		"unsupported": "drop-me",
	}, got)
}

func TestBuildRequestBodyConvertsCompatibleMediaForTokenStackSora15s(t *testing.T) {
	bodyJSON := `{
		"model":"sd-bak-1",
		"prompt":"animate the references",
		"image":"image-1",
		"image_urls":["image-2"],
		"input_reference":"image-3",
		"video_url":"video-1",
		"audio_url":"audio-1",
		"content":[
			{"type":"image_url","image_url":{"url":"image-4"}},
			{"type":"video_url","video_url":{"url":"video-2"}},
			{"type":"audio_url","audio_url":{"url":"audio-2"}}
		]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://www.tokenstack.cc"))
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, []any{"image-1", "image-2", "image-3", "image-4"}, got["images"])
	assert.Equal(t, []any{"video-1", "video-2"}, got["videos"])
	assert.Equal(t, []any{"audio-1", "audio-2"}, got["audios"])
	assert.Contains(t, got, "image")
	assert.Contains(t, got, "image_urls")
	assert.Contains(t, got, "input_reference")
	assert.Contains(t, got, "video_url")
	assert.Contains(t, got, "audio_url")
	assert.Contains(t, got, "content")
}

func TestApplyTokenStackMediaFieldsPreservesRepeatedURLs(t *testing.T) {
	body := map[string]any{
		"images": []any{
			"https://example.com/repeated.png",
			"https://example.com/repeated.png",
		},
	}

	applyTokenStackMediaFields(body)

	assert.Equal(t, []string{
		"https://example.com/repeated.png",
		"https://example.com/repeated.png",
	}, body["images"])
}

func TestBuildRequestBodyAppliesTokenStackFilterAfterOriginModelProfile(t *testing.T) {
	bodyJSON := `{
		"model":"ax2.0-9tu",
		"prompt":"animate the reference",
		"duration":5,
		"images":["image-1"]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := tokenStackRelayInfo("https://www.tokenstack.cc")
	info.OriginModelName = axMultimodalVideoModel

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "seedance-2-0-15s-slow", got["model"])
	assert.Equal(t, []any{"image-1"}, got["images"])
	assert.Equal(t, "5", got["seconds"])
	assert.NotContains(t, got, "duration")
	assert.Contains(t, got, "content")
}

func TestBuildRequestBodyPreservesUnmappedTokenStackSizeFields(t *testing.T) {
	tests := []struct {
		name     string
		fields   string
		wantSize string
	}{
		{name: "portrait ratio", fields: `"ratio":"9:16","resolution":"720p"`, wantSize: "720x1280"},
		{name: "landscape aspect ratio", fields: `"aspect_ratio":"16:9","resolution":"720p"`, wantSize: "1280x720"},
		{name: "other resolution", fields: `"ratio":"9:16","resolution":"1080p"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyJSON := `{"model":"sd-bak-1","prompt":"animate",` + tt.fields + `}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://tokenstack.cc"))
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			if tt.wantSize == "" {
				assert.NotContains(t, got, "size")
			} else {
				assert.Equal(t, tt.wantSize, got["size"])
			}
			if strings.Contains(tt.fields, `"ratio"`) {
				assert.Equal(t, "9:16", got["ratio"])
			} else {
				assert.Equal(t, "16:9", got["aspect_ratio"])
			}
			assert.Contains(t, got, "resolution")
		})
	}
}

func TestBuildRequestBodyDoesNotApplyTokenStackRulesToOtherSoraChannels(t *testing.T) {
	bodyJSON := `{
		"model":"sd-bak-1",
		"prompt":"animate",
		"duration":15,
		"ratio":"9:16",
		"resolution":"720p",
		"size":"720x1280"
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://video.example.com"))
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "9:16", got["ratio"])
	assert.Equal(t, "720p", got["resolution"])
	assert.Equal(t, "15", got["seconds"])
	assert.NotContains(t, got, "duration")
}

func TestBuildRequestBodyDoesNotApplyTokenStackRulesToOtherTokenStackModels(t *testing.T) {
	tests := []struct {
		name          string
		upstreamModel string
		bodyJSON      string
		preserved     []string
	}{
		{
			name:          "multimode",
			upstreamModel: "seedance-2-0-sale",
			bodyJSON:      `{"model":"sd-bak-1","prompt":"animate","input":{"prompt":"animate","media":[{"type":"first_frame","url":"image-1"}]},"parameters":{"resolution":"1080P","duration":15}}`,
			preserved:     []string{"input", "parameters"},
		},
		{
			name:          "doubao",
			upstreamModel: "doubao-seedance-2-0-260128",
			bodyJSON:      `{"model":"sd-bak-1","prompt":"animate","mode":"reference_material","aspect_ratio":"16:9","content":[{"type":"image_url","image_url":{"url":"image-1"}}]}`,
			preserved:     []string{"mode", "aspect_ratio", "content"},
		},
		{
			name:          "multiresolution",
			upstreamModel: "seedance-2.0-720p-fast-15s",
			bodyJSON:      `{"model":"sd-bak-1","prompt":"animate","aspect_ratio":"16:9","seconds":"1","reference_image_urls":["image-1"],"reference_videos":["video-1"],"reference_audios":["audio-1"]}`,
			preserved:     []string{"aspect_ratio", "reference_image_urls", "reference_videos", "reference_audios"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(tt.bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://www.tokenstack.cc", tt.upstreamModel))
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			assert.Equal(t, tt.upstreamModel, got["model"])
			for _, field := range tt.preserved {
				assert.Contains(t, got, field)
			}
		})
	}
}

func TestValidateRequestAndSetActionDoesNotRejectNonJSONTokenStackModels(t *testing.T) {
	for _, modelName := range []string{
		"seedance-2-0-15s-slow",
		"seedance-2-0-sale",
		"doubao-seedance-2-0-260128",
		"seedance-2.0-720p-fast-15s",
	} {
		t.Run(modelName, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("model="+modelName+"&prompt=animate"))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, tokenStackRelayInfo("https://www.tokenstack.cc", modelName))

			require.Nil(t, taskErr)
		})
	}
}

func TestTokenStackSora15sTransformIsLimitedToDocumentedModels(t *testing.T) {
	for _, model := range []string{
		"seedance-2-0-15s-slow",
		"seedance-2-0-15s-high",
		"seedance-2-0-15s-fast",
	} {
		t.Run(model, func(t *testing.T) {
			profile, ok := soraModelProfileForInfo(tokenStackRelayInfo("https://www.tokenstack.cc", model))
			require.True(t, ok)
			assert.Equal(t, requestTransformTokenStackSora15s, profile.JSONFinalTransform)
		})
	}

	for _, model := range []string{
		"seedance-2-0-sale",
		"doubao-seedance-2-0-260128",
		"doubao-seedance-2-0-fast-260128",
		"seedance-2.0-720p-fast-15s",
	} {
		t.Run(model, func(t *testing.T) {
			profile, ok := soraModelProfileForInfo(tokenStackRelayInfo("https://www.tokenstack.cc", model))
			assert.False(t, ok)
			assert.Equal(t, requestTransformNone, profile.JSONFinalTransform)
		})
	}
}

func TestEstimateBillingUsesSubmittedTokenStackDuration(t *testing.T) {
	tests := []struct {
		name          string
		upstreamModel string
		bodyJSON      string
		wantSeconds   float64
	}{
		{
			name:          "multimode nested duration",
			upstreamModel: tokenStackMultiModeModel,
			bodyJSON:      `{"model":"sd-bak-1","prompt":"animate","input":{"prompt":"animate"},"parameters":{"duration":15}}`,
			wantSeconds:   15,
		},
		{
			name:          "multiresolution submitted duration",
			upstreamModel: "seedance-2.0-720p-fast-15s",
			bodyJSON:      `{"model":"sd-bak-1","prompt":"animate","aspect_ratio":"16:9","seconds":"1"}`,
			wantSeconds:   1,
		},
		{
			name:          "sora compatible submitted duration",
			upstreamModel: "seedance-2-0-15s-slow",
			bodyJSON:      `{"model":"sd-bak-1","prompt":"animate","duration":5}`,
			wantSeconds:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(tt.bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := tokenStackRelayInfo("https://www.tokenstack.cc", tt.upstreamModel)
			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))

			ratios := (&TaskAdaptor{}).EstimateBilling(c, info)

			assert.Equal(t, tt.wantSeconds, ratios["seconds"])
		})
	}
}

func TestParseTaskResultSupportsTokenStackMultimodeSuccess(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"object":"https://example.com/video.mp4",
		"seconds":15,
		"status":"SUCCEEDED"
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, string(model.TaskStatusSuccess), info.Status)
	assert.Equal(t, "https://example.com/video.mp4", info.Url)
	assert.Empty(t, extractResponseTaskVideoURL(responseTask{Object: "video"}))
}

func TestParseTaskResultSupportsTokenStackMultimodeFailure(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"object":"",
		"seconds":0,
		"status":"FAILED: content policy violation"
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "content policy violation", info.Reason)
}

func tokenStackRelayInfo(baseURL string, upstreamModels ...string) *relaycommon.RelayInfo {
	upstreamModel := "seedance-2-0-15s-slow"
	if len(upstreamModels) > 0 {
		upstreamModel = upstreamModels[0]
	}
	return &relaycommon.RelayInfo{
		OriginModelName: "sd-bak-1",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    baseURL,
			UpstreamModelName: upstreamModel,
			IsModelMapped:     true,
		},
	}
}
