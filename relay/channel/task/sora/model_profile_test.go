package sora

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyConvertsLegacyMediaForProfiledContentModel(t *testing.T) {
	bodyJSON := `{
		"model":"ax2.0-9tu",
		"prompt":"use the references",
		"duration":15,
		"aspect_ratio":"9:16",
		"generate_audio":false,
		"seed":0,
		"watermark":false,
		"images":["https://example.com/1.png","https://example.com/2.png"],
		"videos":["https://example.com/1.mp4"],
		"audios":["https://example.com/1.mp3"]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: "ax2.0-9tu",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "ax2.0-9tu"},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.NotContains(t, got, "images")
	assert.NotContains(t, got, "videos")
	assert.NotContains(t, got, "audios")
	assert.NotContains(t, got, "seconds")
	assert.Equal(t, float64(15), got["duration"])
	assert.Equal(t, false, got["generate_audio"])
	assert.Equal(t, float64(0), got["seed"])
	assert.Equal(t, false, got["watermark"])

	content, ok := got["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 5)
	assert.Equal(t, map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": "https://example.com/1.png"},
	}, content[0])
	assert.Equal(t, map[string]any{
		"type":      "video_url",
		"video_url": map[string]any{"url": "https://example.com/1.mp4"},
	}, content[2])
	assert.Equal(t, map[string]any{
		"type":      "audio_url",
		"audio_url": map[string]any{"url": "https://example.com/1.mp3"},
	}, content[3])
	assert.Equal(t, map[string]any{
		"type": "text",
		"text": "use the references",
	}, content[4])
}

func TestBuildRequestBodyPreservesExplicitProfiledContent(t *testing.T) {
	bodyJSON := `{
		"model":"sdquan-2",
		"prompt":"animate the image",
		"duration":15,
		"seconds":"15",
		"images":["https://example.com/ignored.png"],
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/content.png"}},
			{"type":"text","text":"animate the image"}
		]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: "sdquan-2",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "sdquan-2"},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.NotContains(t, got, "images")
	assert.NotContains(t, got, "seconds")
	content, ok := got["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 2)
	imageItem, ok := content[0].(map[string]any)
	require.True(t, ok)
	imageURL, ok := imageItem["image_url"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/content.png", imageURL["url"])
}

func TestBuildRequestBodyAppliesRegisteredJSONTransforms(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		body       string
		assertBody func(t *testing.T, got map[string]any)
	}{
		{
			name:  "otoy reference request",
			model: otoySeedanceMiniReferenceModel,
			body: `{
				"model":"otoy-image-to-video-seedance-2-0-mini-reference-to-video",
				"prompt":"animate",
				"duration":15,
				"seconds":"15",
				"images":["https://example.com/1.png"],
				"response_format":"url"
			}`,
			assertBody: func(t *testing.T, got map[string]any) {
				assert.NotContains(t, got, "seconds")
				assert.NotContains(t, got, "response_format")
				assert.Equal(t, "image-to-video", got["type"])
				assert.Equal(t, []any{"https://example.com/1.png"}, got["image_urls"])
			},
		},
		{
			name:  "veo reference images",
			model: veoOmniFlashModel,
			body: `{
				"model":"veo-omni-flash",
				"prompt":"animate",
				"duration":5,
				"images":[
					"https://example.com/1.png",
					"https://example.com/2.png",
					"https://example.com/3.png"
				]
			}`,
			assertBody: func(t *testing.T, got map[string]any) {
				assert.NotContains(t, got, "images")
				assert.Equal(t, []any{
					"https://example.com/1.png",
					"https://example.com/2.png",
					"https://example.com/3.png",
				}, got["Ingredients_images"])
				assert.Equal(t, "5", got["seconds"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
				OriginModelName: tt.model,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: tt.model},
			})
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			tt.assertBody(t, got)
		})
	}
}

func TestBuildRequestBodyPrefersMappedUpstreamTransform(t *testing.T) {
	bodyJSON := `{
		"model":"veo-omni-flash",
		"prompt":"animate",
		"duration":15,
		"images":[
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png"
		]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: veoOmniFlashModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: otoySeedanceMiniReferenceModel},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, otoySeedanceMiniReferenceModel, got["model"])
	assert.Equal(t, "image-to-video", got["type"])
	assert.Equal(t, []any{
		"https://example.com/1.png",
		"https://example.com/2.png",
		"https://example.com/3.png",
	}, got["image_urls"])
	assert.NotContains(t, got, "Ingredients_images")
	assert.NotContains(t, got, "images")
}

func TestBuildRequestBodyAppliesRegisteredMultipartTransform(t *testing.T) {
	var input bytes.Buffer
	inputWriter := multipart.NewWriter(&input)
	require.NoError(t, inputWriter.WriteField("model", otoySeedanceMiniReferenceModel))
	require.NoError(t, inputWriter.WriteField("prompt", "animate"))
	require.NoError(t, inputWriter.WriteField("seconds", "15"))
	require.NoError(t, inputWriter.WriteField("images", "https://example.com/1.png"))
	require.NoError(t, inputWriter.WriteField("response_format", "url"))
	require.NoError(t, inputWriter.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", inputWriter.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: otoySeedanceMiniReferenceModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: otoySeedanceMiniReferenceModel},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	assert.Equal(t, []string{"15"}, form.Value["duration"])
	assert.Equal(t, []string{"https://example.com/1.png"}, form.Value["image_urls"])
	assert.Equal(t, []string{"image-to-video"}, form.Value["type"])
	assert.NotContains(t, form.Value, "seconds")
	assert.NotContains(t, form.Value, "response_format")
}

func TestValidateProfiledContentRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         relaycommon.TaskSubmitReq
		wantMessage string
	}{
		{
			name: "valid legacy ax request",
			req: relaycommon.TaskSubmitReq{
				Model:  "ax2.0-9tu",
				Prompt: "make a video",
				Images: []string{"https://example.com/1.png"},
				Videos: []string{"https://example.com/1.mp4"},
				Audios: []string{"https://example.com/1.mp3"},
			},
		},
		{
			name: "sdquan requires image",
			req: relaycommon.TaskSubmitReq{
				Model:  "sdquan-2",
				Prompt: "make a video",
			},
			wantMessage: "requires at least one image reference",
		},
		{
			name: "explicit content requires text",
			req: relaycommon.TaskSubmitReq{
				Model:  "ax2.0-9tu",
				Prompt: "top-level prompt does not replace explicit content text",
				Content: []relaycommon.TaskContentItem{{
					Type:     "image_url",
					ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/1.png"},
				}},
			},
			wantMessage: "at least one non-empty text item",
		},
		{
			name: "image limit",
			req: relaycommon.TaskSubmitReq{
				Model:  "ax2.0-9tu",
				Prompt: "make a video",
				Images: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
			},
			wantMessage: "at most 9 image references",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("task_request", tt.req)

			taskErr := validateSoraModelRequest(c, &relaycommon.RelayInfo{})
			if tt.wantMessage == "" {
				assert.Nil(t, taskErr)
				return
			}
			require.NotNil(t, taskErr)
			assert.Contains(t, taskErr.Message, tt.wantMessage)
		})
	}
}

func TestTaskSubmitReqHasImageFromContent(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{{
		Type:     "image_url",
		ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/1.png"},
	}}}

	assert.True(t, req.HasImage())
}

func TestValidateRequestAndSetActionDetectsContentImage(t *testing.T) {
	bodyJSON := `{
		"model":"sdquan-2",
		"prompt":"animate the image",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/1.png"}},
			{"type":"text","text":"animate the image"}
		]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	assert.Nil(t, taskErr)
	assert.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestValidateRequestAndSetActionUsesProfiledSizeConstraints(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		size        string
		wantMessage string
	}{
		{name: "sora standard accepts landscape", model: sora2Model, size: "1280x720"},
		{name: "sora standard rejects pro size", model: sora2Model, size: "1792x1024", wantMessage: "sora-2 size is invalid"},
		{name: "sora pro accepts pro size", model: sora2ProModel, size: "1792x1024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyJSON := fmt.Sprintf(`{"model":%q,"prompt":"animate","size":%q}`, tt.model, tt.size)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.Nil(t, taskErr)
			taskErr = (&TaskAdaptor{}).ValidateMappedRequest(c, info)

			if tt.wantMessage == "" {
				assert.Nil(t, taskErr)
				return
			}
			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_size", taskErr.Code)
			assert.Contains(t, taskErr.Message, tt.wantMessage)
		})
	}
}

func TestValidateMappedRequestUsesMappedUpstreamProfile(t *testing.T) {
	tests := []struct {
		name          string
		upstreamModel string
		req           relaycommon.TaskSubmitReq
		wantMessage   string
	}{
		{
			name:          "sdquan alias still requires image",
			upstreamModel: sdquanImageVideoModel,
			req: relaycommon.TaskSubmitReq{
				Model:  "public-sdquan-alias",
				Prompt: "animate",
			},
			wantMessage: "requires at least one image reference",
		},
		{
			name:          "ax alias still enforces image limit",
			upstreamModel: axMultimodalVideoModel,
			req: relaycommon.TaskSubmitReq{
				Model:  "public-ax-alias",
				Prompt: "animate",
				Images: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
			},
			wantMessage: "at most 9 image references",
		},
		{
			name:          "ax alias still requires content text",
			upstreamModel: axMultimodalVideoModel,
			req: relaycommon.TaskSubmitReq{
				Model:  "public-ax-alias",
				Prompt: "top-level prompt",
				Content: []relaycommon.TaskContentItem{{
					Type:     "image_url",
					ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/1.png"},
				}},
			},
			wantMessage: "at least one non-empty text item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("task_request", tt.req)
			info := &relaycommon.RelayInfo{
				OriginModelName: tt.req.Model,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: tt.upstreamModel},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			taskErr := (&TaskAdaptor{}).ValidateMappedRequest(c, info)

			require.NotNil(t, taskErr)
			assert.Contains(t, taskErr.Message, tt.wantMessage)
		})
	}
}

func TestValidateMappedRequestRejectsMultipartForJSONOnlyModels(t *testing.T) {
	for _, modelName := range []string{axMultimodalVideoModel, sdquanImageVideoModel} {
		t.Run(modelName, func(t *testing.T) {
			var input bytes.Buffer
			writer := multipart.NewWriter(&input)
			require.NoError(t, writer.WriteField("model", modelName))
			require.NoError(t, writer.WriteField("prompt", "animate"))
			require.NoError(t, writer.WriteField("duration", "5"))
			require.NoError(t, writer.WriteField("images", "https://example.com/1.png"))
			require.NoError(t, writer.WriteField("content", `[{"type":"text","text":"animate"}]`))
			require.NoError(t, writer.Close())

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: modelName,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: modelName},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusUnsupportedMediaType, taskErr.StatusCode)
			assert.Equal(t, "unsupported_content_type", taskErr.Code)
			assert.Contains(t, taskErr.Message, "only supports application/json")
		})
	}
}

func TestValidateMappedRequestCountsAllLegacyImageFields(t *testing.T) {
	bodyJSON := `{
		"model":"ax2.0-9tu",
		"prompt":"animate",
		"images":["1","2","3","4","5","6","7","8","9"],
		"input_reference":"10"
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: axMultimodalVideoModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: axMultimodalVideoModel},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	taskErr = (&TaskAdaptor{}).ValidateMappedRequest(c, info)

	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "at most 9 image references")
}

func TestFixedDurationProfilesAlwaysEstimateFifteenSeconds(t *testing.T) {
	for _, modelName := range []string{axMultimodalVideoModel, sdquanImageVideoModel} {
		for _, requestedDuration := range []int{0, 5, 30} {
			t.Run(fmt.Sprintf("%s_duration_%d", modelName, requestedDuration), func(t *testing.T) {
				info := &relaycommon.RelayInfo{
					OriginModelName: modelName,
					ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: modelName},
				}

				seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{
					Model:    modelName,
					Duration: requestedDuration,
				}, info)

				assert.Equal(t, 15, seconds)
			})
		}
	}
}

func TestBuildRequestBodyForcesFixedDuration(t *testing.T) {
	bodyJSON := `{
		"model":"ax2.0-9tu",
		"prompt":"animate",
		"duration":5,
		"images":["https://example.com/1.png"]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: axMultimodalVideoModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: axMultimodalVideoModel},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, float64(15), got["duration"])
	assert.NotContains(t, got, "seconds")
}

func TestNormalizeBillingRequestBodyUsesEffectiveFixedDuration(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-ax-alias",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: axMultimodalVideoModel},
	}
	normalizedBody, err := (&TaskAdaptor{}).NormalizeBillingRequestBody(info, []byte(`{
		"model":"public-ax-alias",
		"prompt":"animate",
		"duration":5,
		"seconds":"5"
	}`))
	require.NoError(t, err)

	var normalized map[string]any
	require.NoError(t, common.Unmarshal(normalizedBody, &normalized))
	assert.Equal(t, float64(15), normalized["duration"])
	assert.NotContains(t, normalized, "seconds")

	cost, trace, err := billingexpr.RunExprWithRequest(
		`param("duration") == 15 ? tier("fixed_15s", 15) : tier("wrong_duration", 5)`,
		billingexpr.TokenParams{},
		billingexpr.RequestInput{Body: normalizedBody},
	)
	require.NoError(t, err)
	assert.Equal(t, float64(15), cost)
	assert.Equal(t, "fixed_15s", trace.MatchedTier)
}

func TestMappedProfilesCombineDefaultAndMaximumDuration(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: seedanceGatewayModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: canvasStandardSeedanceModel},
	}

	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{Model: seedanceGatewayModel}, info)

	assert.Equal(t, canvasStandardMaxVideoSeconds, seconds)
}
