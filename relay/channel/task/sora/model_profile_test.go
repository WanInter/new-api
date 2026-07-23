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

func TestBuildRequestBodyConvertsMediaAliasesForProfiledContentModel(t *testing.T) {
	bodyJSON := `{
		"model":"sdquan-2",
		"prompt":"use every reference",
		"image_urls":["image.png"],
		"input":{"image_references":[{"url":"reference.png"}]},
		"video_url":"video.mp4",
		"audio_url":"audio.mp3"
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
	assert.NotContains(t, got, "image_urls")
	assert.NotContains(t, got, "input")
	assert.NotContains(t, got, "video_url")
	assert.NotContains(t, got, "audio_url")
	content, ok := got["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 5)
}

func TestBuildRequestBodyMergesExplicitProfiledContentWithCompatibilityMedia(t *testing.T) {
	bodyJSON := `{
		"model":"sdquan-2",
		"prompt":"animate the image",
		"duration":15,
		"seconds":"15",
		"images":["https://example.com/legacy-image.png"],
		"videos":["https://example.com/legacy-video.mp4"],
		"audios":["https://example.com/legacy-audio.mp3"],
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
	assert.NotContains(t, got, "videos")
	assert.NotContains(t, got, "audios")
	assert.NotContains(t, got, "seconds")
	content, ok := got["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 5)
	imageItem, ok := content[0].(map[string]any)
	require.True(t, ok)
	imageURL, ok := imageItem["image_url"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/legacy-image.png", imageURL["url"])
	assert.Equal(t, map[string]any{
		"type":      "video_url",
		"video_url": map[string]any{"url": "https://example.com/legacy-video.mp4"},
	}, content[1])
	assert.Equal(t, map[string]any{
		"type":      "audio_url",
		"audio_url": map[string]any{"url": "https://example.com/legacy-audio.mp3"},
	}, content[2])
	assert.Equal(t, map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": "https://example.com/content.png"},
	}, content[3])
	assert.Equal(t, map[string]any{"type": "text", "text": "animate the image"}, content[4])
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
				"response_format":"url",
				"metadata":{"provider_option":true}
			}`,
			assertBody: func(t *testing.T, got map[string]any) {
				assert.NotContains(t, got, "seconds")
				assert.Equal(t, "url", got["response_format"])
				assert.Equal(t, map[string]any{"provider_option": true}, got["metadata"])
				assert.NotContains(t, got, "type")
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
		{
			name:  "veo video edit reference images",
			model: veoOmniFlashVideoEditModel,
			body: `{
				"model":"veo-omni-flash-video-edit",
				"prompt":"edit the reference video",
				"duration":10,
				"images":[
					"https://example.com/1.png",
					"https://example.com/2.png",
					"https://example.com/3.png"
				],
				"videos":["https://example.com/reference.mp4"]
			}`,
			assertBody: func(t *testing.T, got map[string]any) {
				assert.NotContains(t, got, "images")
				assert.Equal(t, []any{
					"https://example.com/1.png",
					"https://example.com/2.png",
					"https://example.com/3.png",
				}, got["Ingredients_images"])
				assert.Equal(t, []any{"https://example.com/reference.mp4"}, got["videos"])
				assert.Equal(t, "10", got["seconds"])
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
	assert.NotContains(t, got, "type")
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
	require.NoError(t, inputWriter.WriteField("metadata", `{"provider_option":true}`))
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
	assert.NotContains(t, form.Value, "seconds")
	assert.NotContains(t, form.Value, "type")
	assert.Equal(t, []string{"url"}, form.Value["response_format"])
	assert.Equal(t, []string{`{"provider_option":true}`}, form.Value["metadata"])
}

func TestProfiledModelConstraintsAreNotRejectedLocally(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "ax accepts videos and more than nine images",
			body: `{"model":"ax2.0-9tu","prompt":"animate","images":["1","2","3","4","5","6","7","8","9","10"],"videos":["video-1","video-2","video-3","video-4"]}`,
		},
		{
			name: "sdquan does not require an image locally",
			body: `{"model":"sdquan-2","prompt":"animate","audios":["audio-1","audio-2","audio-3","audio-4"]}`,
		},
		{
			name: "explicit content does not require a text item locally",
			body: `{"model":"ax2.0-9tu","prompt":"animate","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
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

func TestValidateRequestAndSetActionDoesNotApplyModelSizeConstraints(t *testing.T) {
	tests := []struct {
		name  string
		model string
		size  string
	}{
		{name: "sora standard accepts landscape", model: sora2Model, size: "1280x720"},
		{name: "sora standard forwards pro size", model: sora2Model, size: "1792x1024"},
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

			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
		})
	}
}

func TestBuildRequestBodyUsesMappedContentTransformWithoutModelLimits(t *testing.T) {
	bodyJSON := `{
		"model":"public-ax-alias",
		"prompt":"animate",
		"images":["1","2","3","4","5","6","7","8","9","10"],
		"videos":["video-1","video-2","video-3","video-4"],
		"audios":["audio-1","audio-2","audio-3","audio-4"]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: "public-ax-alias",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: axMultimodalVideoModel},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, axMultimodalVideoModel, got["model"])
	content, ok := got["content"].([]any)
	require.True(t, ok)
	assert.Len(t, content, 19)
}

func TestProfiledModelsDoNotRejectMultipartLocally(t *testing.T) {
	for _, modelName := range []string{axMultimodalVideoModel, sdquanImageVideoModel} {
		t.Run(modelName, func(t *testing.T) {
			var input bytes.Buffer
			writer := multipart.NewWriter(&input)
			require.NoError(t, writer.WriteField("model", modelName))
			require.NoError(t, writer.WriteField("prompt", "animate"))
			require.NoError(t, writer.WriteField("duration", "5"))
			require.NoError(t, writer.WriteField("images", "https://example.com/1.png"))
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
			require.Nil(t, taskErr)
		})
	}
}

func TestBuildRequestBodyConvertsAllLegacyImageFieldsWithoutLimit(t *testing.T) {
	bodyJSON := `{
		"model":"ax2.0-9tu",
		"prompt":"animate",
		"images":["1","2","3","4","5"],
		"image_urls":["6"],
		"input_reference":"7",
		"input":{"start_frames":["8"],"image_references":[{"url":"9"}]},
		"metadata":{"start_frames":["10"]}
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

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	content, ok := got["content"].([]any)
	require.True(t, ok)
	assert.Len(t, content, 11)
}

func TestProfiledFixedModelsEstimateDocumentedDuration(t *testing.T) {
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

func TestBuildRequestBodyMaterializesProfiledFixedDuration(t *testing.T) {
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
	assert.Equal(t, false, got["generate_audio"])
}

func TestBuildRequestBodyReplacesProfiledSecondsAliasWithFixedDuration(t *testing.T) {
	tests := []struct {
		name    string
		seconds string
	}{
		{name: "string value", seconds: `"5"`},
		{name: "explicit numeric zero", seconds: "0"},
		{name: "fractional numeric value", seconds: "14.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyJSON := `{
				"model":"sdquan-2",
				"prompt":"animate",
				"seconds":` + tt.seconds + `,
				"images":["https://example.com/1.png"]
			}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
				OriginModelName: sdquanImageVideoModel,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: sdquanImageVideoModel},
			})
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			assert.Equal(t, float64(15), got["duration"])
			assert.NotContains(t, got, "seconds")
		})
	}
}

func TestEstimateBillingUsesProfiledFixedDuration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "public-ax-alias",
		Duration: 5,
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-ax-alias",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: axMultimodalVideoModel},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)

	assert.Equal(t, float64(15), ratios["seconds"])
}

func TestCanvasProfileOverridesRequestedDuration(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: seedanceGatewayModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: canvasStandardSeedanceModel},
	}

	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{Model: seedanceGatewayModel, Duration: 20}, info)

	assert.Equal(t, 15, seconds)
}
