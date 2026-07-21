package yoboxcorp

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

func TestBuildRequestUsesDreaminaTopLevelVideoContract(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-fast-hc",
		"prompt":"create a cinematic portrait",
		"aspect_ratio":"9:16",
		"duration":15,
		"resolution":"720p",
		"watermark":true,
		"images":["https://example.com/face.png"],
		"videos":["https://example.com/motion.mp4"],
		"audios":["https://example.com/music.mp3"]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "dreamina-seedance-2-0-fast-hc",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "dreamina-seedance-2-0-fast-hc"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{baseURL: "https://corp.example.test"}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://corp.example.test/async/tasks", requestURL)

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
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "create a cinematic portrait"},
		map[string]any{"type": "image_url", "role": "reference_image", "image_url": map[string]any{"url": "https://example.com/face.png"}},
		map[string]any{"type": "video_url", "role": "reference_video", "video_url": map[string]any{"url": "https://example.com/motion.mp4"}},
		map[string]any{"type": "audio_url", "role": "reference_audio", "audio_url": map[string]any{"url": "https://example.com/music.mp3"}},
	}, payload["content"])
}

func TestBuildGeneratePayloadDefaultsRequiredFields(t *testing.T) {
	payload := buildGeneratePayload(relaycommon.TaskSubmitReq{Model: "dreamina-seedance-2-0-hc", Size: "720x1280"}, nil)

	assert.Equal(t, "dreamina-seedance-2-0-hc", payload["model"])
	assert.Equal(t, 4, payload["duration"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, "9:16", payload["ratio"])
	assert.Equal(t, false, payload["watermark"])
}

func TestDoResponseUsesAsyncTaskEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		OriginModelName: "dreamina-seedance-2-0-fast-hc",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"task_id":"mvt_upstream","status":"SUBMITTED"}}`))}

	upstreamTaskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "mvt_upstream", upstreamTaskID)
	assert.Contains(t, string(taskData), "mvt_upstream")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestFetchAndParseTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/async/tasks/mvt_upstream", r.URL.Path)
		assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"success":true,"data":{"task_id":"mvt_upstream","status":"SUCCESS","progress":100,"data":{"video_url":"https://example.com/result.mp4"}}}`))
	}))
	t.Cleanup(server.Close)

	response, err := (&TaskAdaptor{}).FetchTask(t.Context(), server.URL, "upstream-key", map[string]any{"task_id": "mvt_upstream"}, "")
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
	assert.Equal(t, "100%", result.Progress)
}

func TestParseTaskFailureExposesUpstreamError(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"success":true,"data":{"task_id":"mvt_upstream","status":"FAILURE","data":{"error":{"message":"invalid asset"}}}}`))

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), result.Status)
	assert.Equal(t, "invalid asset", result.Reason)
	assert.Equal(t, "100%", result.Progress)
}

func TestParseNativeTaskEnvelopeRemainsSupported(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"task":{"id":"mvt_native","status":"completed","outputs":["https://example.com/native.mp4"]}}`))

	require.NoError(t, err)
	assert.Equal(t, "mvt_native", result.TaskID)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "https://example.com/native.mp4", result.Url)
}

func TestConvertToOpenAIVideoIncludesStoredResultURL(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_public",
		Status:      model.TaskStatusSuccess,
		CreatedAt:   1,
		Properties:  model.Properties{OriginModelName: "dreamina-seedance-2-0-hc"},
		PrivateData: model.TaskPrivateData{ResultURL: "https://example.com/result.mp4"},
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &video))
	assert.Equal(t, "task_public", video.ID)
	assert.Equal(t, "https://example.com/result.mp4", video.Metadata["video_url"])
}

func TestGetModelList(t *testing.T) {
	assert.Equal(t, []string{
		"dreamina-seedance-2-0-hc",
		"dreamina-seedance-2-0-fast-hc",
		"dreamina-seedance-2-0-mini-hc",
	}, (&TaskAdaptor{}).GetModelList())
}
