package aggc

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

func TestParseTaskResultSuccess(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"code":0,"message":"OK","data":{"job_id":123,"status":"success","video_url":"https://example.com/result.mp4","video_cover_url":"https://example.com/cover.jpg"}}`)
	info, err := adaptor.ParseTaskResult(body)
	if err != nil {
		t.Fatalf("ParseTaskResult returned error: %v", err)
	}
	if info.Status != model.TaskStatusSuccess {
		t.Fatalf("unexpected status: %s", info.Status)
	}
	if info.Url != "https://example.com/result.mp4" {
		t.Fatalf("unexpected url: %s", info.Url)
	}
}

func TestParseTaskResultUsesErrorMessageForFailure(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"code":0,"message":"OK","data":{"error_message":"素材或内容包含敏感信息，未通过内容安全审核。","job_id":"task_upstream","status":"failed"}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "素材或内容包含敏感信息，未通过内容安全审核。", info.Reason)
	assert.Equal(t, "100%", info.Progress)
}

func TestParseTaskResultDoesNotUseSuccessMessageAsFailureReason(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"code":0,"message":"OK","data":{"job_id":123,"status":"failed","video_url":"","video_cover_url":"","message":"","error":"","error_message":"","fail_reason":"","progress":null}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "AGGC upstream reported task failure without error details", info.Reason)
	assert.Equal(t, "100%", info.Progress)
}

func TestParseTaskResultExtractsNestedErrorMessage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"code":0,"data":{"error_message":"{\"error\":{\"code\":\"INTERNAL_SERVER_ERROR\",\"message\":\"系统内部错误，请稍后重试\"}}","job_id":"85cbab90-29c5-4fbe-b4e3-ae30aacf9a1e","status":"failed"},"message":"OK"}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	assert.Equal(t, "系统内部错误，请稍后重试", info.Reason)
	assert.Equal(t, "100%", info.Progress)
}

func TestParseTaskResultRejectsUnknownStatus(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"code":0,"data":{"job_id":"task_unknown","status":"pausing"}}`))

	require.Error(t, err)
	assert.Nil(t, info)
}

func TestConvertToOpenAIVideoUsesSoraCompatibleResponseShape(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1782570791,
		UpdatedAt: 1782571022,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "123",
		},
		Properties: model.Properties{
			OriginModelName: "sd-bak-2",
		},
		Data: []byte(`{
			"code":0,
			"message":"OK",
			"data":{
				"job_id":123,
				"status":"success",
				"video_url":"https://example.com/result.mp4",
				"video_cover_url":"https://example.com/cover.jpg"
			}
		}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	require.Equal(t, "task_public", got["id"])
	require.Equal(t, "video", got["object"])
	require.Equal(t, "123", got["task_id"])
	require.Equal(t, "sd-bak-2", got["model"])
	require.Equal(t, "completed", got["status"])
	require.Equal(t, "https://example.com/result.mp4", got["result_url"])
	require.Equal(t, "https://example.com/result.mp4", got["url"])
	require.Equal(t, "https://example.com/result.mp4", got["video_url"])
	require.Equal(t, []any{"https://example.com/result.mp4"}, got["output"])
	require.Equal(t, "https://example.com/cover.jpg", got["video_cover_url"])

	video, ok := got["video"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://example.com/result.mp4", video["url"])
}

func TestDoResponseExtractsTaskID(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload := []byte(`{"code":0,"message":"OK","data":{"job_id":123,"status":"queued"}}`)
	var resp submitResponse
	if err := common.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if anyToString(resp.Data.JobID) != "123" {
		t.Fatalf("unexpected job id: %s", anyToString(resp.Data.JobID))
	}
	_ = adaptor
}

func TestBuildRequestBodyPreservesMediaAliases(t *testing.T) {
	testCases := []struct {
		name       string
		mediaJSON  string
		wantVideos []string
		wantAudios []string
	}{
		{
			name:       "canonical arrays",
			mediaJSON:  `"videos":["video-1.mp4"],"audios":["audio-1.mp3"]`,
			wantVideos: []string{"video-1.mp4"},
			wantAudios: []string{"audio-1.mp3"},
		},
		{
			name:       "canonical scalars",
			mediaJSON:  `"videos":"video-1.mp4","audios":"audio-1.mp3"`,
			wantVideos: []string{"video-1.mp4"},
			wantAudios: []string{"audio-1.mp3"},
		},
		{
			name:       "singular aliases",
			mediaJSON:  `"video":"video-1.mp4","audio":"audio-1.mp3"`,
			wantVideos: []string{"video-1.mp4"},
			wantAudios: []string{"audio-1.mp3"},
		},
		{
			name:       "URL aliases",
			mediaJSON:  `"video_url":"video-1.mp4","video_urls":"video-2.mp4","audio_url":"audio-1.mp3","audio_urls":"audio-2.mp3"`,
			wantVideos: []string{"video-1.mp4", "video-2.mp4"},
			wantAudios: []string{"audio-1.mp3", "audio-2.mp3"},
		},
		{
			name:      "boolean audio switch",
			mediaJSON: `"audio":true`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := buildAggcRequestPayload(t, `{
				"model":"sd-bak-1",
				"prompt":"animate the references",
				`+testCase.mediaJSON+`
			}`)
			assert.Equal(t, testCase.wantVideos, payload.Params.VideoURLs)
			assert.Equal(t, testCase.wantAudios, payload.Params.AudioURLs)
		})
	}
}

func TestBuildRequestBodyPreservesMetadataOnlyMediaEntries(t *testing.T) {
	payload := buildAggcRequestPayload(t, `{
		"model":"sd-bak-1",
		"prompt":"animate the references",
		"metadata":{
			"video_urls":["same.mp4","same.mp4"],
			"audio_urls":["same.mp3","same.mp3"]
		}
	}`)

	assert.Equal(t, []string{"same.mp4", "same.mp4"}, payload.Params.VideoURLs)
	assert.Equal(t, []string{"same.mp3", "same.mp3"}, payload.Params.AudioURLs)
}

func buildAggcRequestPayload(t *testing.T, body string) requestPayload {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		OriginModelName: "sd-bak-1",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	bodyReader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	requestBody, err := io.ReadAll(bodyReader)
	require.NoError(t, err)

	var payload requestPayload
	require.NoError(t, common.Unmarshal(requestBody, &payload))
	return payload
}

func TestConvertToRequestPayloadConvertsTopLevelSize(t *testing.T) {
	metadata := map[string]any{}
	copyAggcRawMetadata(jsonRequest{Size: "1024x1024"}, metadata)

	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "draw a cat",
		Model:    "sd-bak-2",
		Metadata: metadata,
	}, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	assert.Equal(t, "1024p", payload.Params.Resolution)
	assert.Equal(t, "1:1", payload.Params.AspectRatio)
}

func TestConvertToRequestPayloadConvertsParamsSize(t *testing.T) {
	metadata := map[string]any{}
	copyAggcRawMetadata(jsonRequest{Params: map[string]any{"size": "512x768"}}, metadata)

	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "draw a cat",
		Model:    "sd-bak-2",
		Metadata: metadata,
	}, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	assert.Equal(t, "512p", payload.Params.Resolution)
	assert.Equal(t, "2:3", payload.Params.AspectRatio)
}

func TestConvertToRequestPayloadConvertsSizeAndCommonParams(t *testing.T) {
	watermark := false
	metadata := map[string]any{}
	copyAggcRawMetadata(jsonRequest{
		Size:        "1280x720",
		Orientation: "landscape",
		Watermark:   &watermark,
	}, metadata)

	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "draw a watch",
		Model:    "sd-bak-2",
		Images:   []string{"https://example.com/input.png"},
		Duration: 5,
		Metadata: metadata,
	}, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	assert.Equal(t, "720p", payload.Params.Resolution)
	assert.Equal(t, "16:9", payload.Params.AspectRatio)
	assert.Equal(t, "landscape", payload.Params.Orientation)
	require.NotNil(t, payload.Params.Watermark)
	assert.False(t, *payload.Params.Watermark)
	assert.Equal(t, []string{"https://example.com/input.png"}, payload.Params.ImageURLs)
}

func TestConvertToRequestPayloadAcceptsTopLevelCamelAspectRatio(t *testing.T) {
	metadata := map[string]any{}
	copyAggcRawMetadata(jsonRequest{AspectRatioCamel: "9:16"}, metadata)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "draw a cat",
		Model:    "seedance-2.0",
		Metadata: metadata,
	}, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	require.Equal(t, "9:16", payload.Params.AspectRatio)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"aspectRatio":"9:16"`)
	require.NotContains(t, string(body), "aspect_ratio")
}

func TestConvertToRequestPayloadAcceptsTopLevelSnakeAspectRatio(t *testing.T) {
	metadata := map[string]any{}
	copyAggcRawMetadata(jsonRequest{AspectRatio: "16:9"}, metadata)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "draw a cat",
		Model:    "seedance-2.0",
		Metadata: metadata,
	}, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	require.Equal(t, "16:9", payload.Params.AspectRatio)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"aspectRatio":"16:9"`)
	require.NotContains(t, string(body), "aspect_ratio")
}
