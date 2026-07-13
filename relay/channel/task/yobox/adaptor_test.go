package yobox

import (
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
	require.Equal(t, []string{"seedance2", "seedance-2.0", "seedance-2.0-fast"}, (&TaskAdaptor{}).GetModelList())
}
