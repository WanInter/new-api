package yobox

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadSeedance2UsesInputReference(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:   "seedance2",
		Prompt:  "dance",
		Seconds: "12",
		Size:    "720x1280",
		Images:  []string{"https://example.com/ref.png"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"input_reference":"https://example.com/ref.png"`)
	require.Contains(t, string(body), `"seconds":"12"`)
}

func TestConvertToRequestPayloadSeedance20AlwaysUsesImageReferences(t *testing.T) {
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
			expected: []map[string]any{{"url": "https://example.com/1.png", "strength": "MID"}},
		},
		{
			name:   "two images",
			model:  "seedance-2.0",
			images: []string{"https://example.com/1.png", "https://example.com/2.png"},
			expected: []map[string]any{
				{"url": "https://example.com/1.png", "strength": "MID"},
				{"url": "https://example.com/2.png", "strength": "MID"},
			},
		},
		{
			name:   "three images with fast model",
			model:  "seedance-2.0-fast",
			images: []string{"https://example.com/1.png", "https://example.com/2.png", "https://example.com/3.png"},
			expected: []map[string]any{
				{"url": "https://example.com/1.png", "strength": "MID"},
				{"url": "https://example.com/2.png", "strength": "MID"},
				{"url": "https://example.com/3.png", "strength": "MID"},
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
		{"url": "https://example.com/reference.mp4", "strength": "MID"},
		{"url": "https://example.com/legacy-reference.mp4", "strength": "MID"},
	}, input["video_references"])
	assert.Equal(t, []map[string]any{
		{"url": "https://example.com/reference.mp3", "strength": "MID"},
		{"url": "https://example.com/legacy-reference.mp3", "strength": "MID"},
	}, input["audio_references"])
	assert.Equal(t, true, input["audio"])
}

func TestConvertToRequestPayloadDefaultsSeedance20Resolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0",
		Prompt:   "run",
		Duration: 15,
		Metadata: map[string]any{"aspect_ratio": "9:16"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	input, ok := body["input"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "9:16", input["aspect_ratio"])
	require.Equal(t, "720p", input["resolution"])
}

func TestDreaminaSeedance20BuildRequestUsesTopLevelVideoContract(t *testing.T) {
	bodyJSON := `{
		"model":"dreamina-seedance-2-0-fast-hc",
		"prompt":"create a cinematic portrait",
		"aspect_ratio":"9:16",
		"duration":15,
		"resolution":"720p",
		"watermark":true,
		"images":["https://example.com/face.png","https://example.com/scene.jpg"]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "dreamina-seedance-2-0-fast-hc",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "dreamina-seedance-2-0-fast-hc",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{baseURL: "https://corp.example.test"}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://corp.example.test/v1/video/generate", requestURL)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(data, &payload))
	assert.Equal(t, "dreamina-seedance-2-0-fast-hc", payload["model"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, "9:16", payload["ratio"])
	assert.Equal(t, float64(15), payload["duration"])
	assert.Equal(t, true, payload["watermark"])
	assert.NotContains(t, payload, "input")
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "create a cinematic portrait"},
		map[string]any{
			"type":      "image_url",
			"role":      "reference_image",
			"image_url": map[string]any{"url": "https://example.com/face.png"},
		},
		map[string]any{
			"type":      "image_url",
			"role":      "reference_image",
			"image_url": map[string]any{"url": "https://example.com/scene.jpg"},
		},
	}, payload["content"])
}

func TestDreaminaSeedance20DoResponseUsesTaskEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		OriginModelName: "dreamina-seedance-2-0-fast-hc",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "dreamina-seedance-2-0-fast-hc",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(`{
		"task":{"id":"mvt_upstream","status":"pending","model":"dreamina-seedance-2-0-fast-hc","outputs":[]}
	}`))}

	upstreamTaskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "mvt_upstream", upstreamTaskID)
	assert.Contains(t, string(taskData), "mvt_upstream")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestDreaminaSeedance20FetchAndParseTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/video/tasks/mvt_upstream", r.URL.Path)
		assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{
			"task":{"id":"mvt_upstream","status":"completed","outputs":["https://example.com/result.mp4"]}
		}`))
	}))
	t.Cleanup(server.Close)

	response, err := (&TaskAdaptor{}).FetchTask(t.Context(), server.URL, "upstream-key", map[string]any{
		"task_id": "mvt_upstream",
		"model":   "dreamina-seedance-2-0-fast-hc",
	}, "")
	require.NoError(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	result, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, "mvt_upstream", result.TaskID)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "https://example.com/result.mp4", result.Url)
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

func TestConvertToRequestPayloadHappyHorseUsesOnlyFirstStartFrame(t *testing.T) {
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
	assert.Equal(t, []string{"https://example.com/start.png"}, input["start_frames"])
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
	assert.Equal(t, []string{"https://example.com/start.png"}, input["start_frames"])
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

func TestParseTaskResultRejectsUnknownStatus(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"task_id":"task_unknown","status":"pausing"}`))

	require.Error(t, err)
	assert.Nil(t, info)
}

func TestDoResponseRedactsImageReferencesLimit(t *testing.T) {
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
	assert.Equal(t, yoboxGenericProcessingError, taskErr.Message)
	assert.NotContains(t, taskErr.Message, "image_references")
}

func TestSanitizeTaskUpstreamErrorRedactsNestedImageReferencesLimit(t *testing.T) {
	body := []byte(`{"success":false,"message":"{\"error\":\"sd-bak-3 最多支持 4 张 image_references\"}"}`)

	message := (&TaskAdaptor{}).SanitizeTaskUpstreamError(body)

	assert.Equal(t, yoboxGenericProcessingError, message)
	assert.NotContains(t, message, "image_references")
	assert.NotContains(t, message, "sd-bak-3")
}

func TestParseTaskResultRedactsImageReferencesLimit(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status": "FAILURE",
		"fail_reason": "最多支持 4 张 image_references"
	}`))
	require.NoError(t, err)

	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, yoboxGenericProcessingError, info.Reason)
	assert.NotContains(t, info.Reason, "image_references")
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

func TestMergeYoboxRequestMetadataExtractsContentImages(t *testing.T) {
	req := &relaycommon.TaskSubmitReq{
		Metadata: map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "prompt"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/1.png"}},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/2.png"}},
			},
		},
	}
	req.Images = mergeYoboxImages(req.Images, extractYoboxContentImages(req.Metadata["content"]))
	require.Equal(t, []string{"https://example.com/1.png", "https://example.com/2.png"}, req.Images)
}

func TestModelListIncludesSupportedModels(t *testing.T) {
	require.Equal(t, []string{
		"seedance2",
		"seedance-2.0",
		"seedance-2.0-fast",
		"happy-horse-1.1",
		"dreamina-seedance-2-0-hc",
		"dreamina-seedance-2-0-fast-hc",
		"dreamina-seedance-2-0-mini-hc",
	}, (&TaskAdaptor{}).GetModelList())
}
