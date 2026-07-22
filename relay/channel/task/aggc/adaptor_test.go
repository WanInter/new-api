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

func TestBuildRequestBodyAcceptsScalarAndArrayImageInputs(t *testing.T) {
	testCases := []struct {
		name       string
		imageField string
		expected   []string
	}{
		{name: "images scalar", imageField: `"images":"https://example.com/one.png"`, expected: []string{"https://example.com/one.png"}},
		{name: "images array", imageField: `"images":["https://example.com/one.png","https://example.com/two.png"]`, expected: []string{"https://example.com/one.png", "https://example.com/two.png"}},
		{name: "image URLs scalar", imageField: `"image_urls":"https://example.com/alias.png"`, expected: []string{"https://example.com/alias.png"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := buildAggcRequestPayload(t, `{
				"model":"seedance-2.0",
				"prompt":"animate the image",
				`+testCase.imageField+`
			}`)

			assert.Equal(t, testCase.expected, payload.Params.ImageURLs)
		})
	}
}

func TestBuildRequestBodyMapsAggcUnifiedMediaAliases(t *testing.T) {
	payload := buildAggcRequestPayload(t, `{
		"model":"seedance-2.0",
		"prompt":"combine the references",
		"input_reference":"https://example.com/input-reference.png",
		"input":{"start_frames":["https://example.com/start-frame.png"],"image_references":[{"url":"https://example.com/image-reference.png"}]},
		"metadata":{"start_frames":["https://example.com/metadata-start.png"]},
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/content-image.png"}},
			{"type":"video_url","video_url":{"url":"https://example.com/content-video.mp4"}},
			{"type":"audio_url","audio_url":{"url":"https://example.com/content-audio.mp3"}}
		]
	}`)

	assert.Equal(t, []string{
		"https://example.com/input-reference.png",
		"https://example.com/start-frame.png",
		"https://example.com/image-reference.png",
		"https://example.com/metadata-start.png",
		"https://example.com/content-image.png",
	}, payload.Params.ImageURLs)
	assert.Equal(t, []string{"https://example.com/content-video.mp4"}, payload.Params.VideoURLs)
	assert.Equal(t, []string{"https://example.com/content-audio.mp3"}, payload.Params.AudioURLs)
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
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

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

func TestConvertToRequestPayloadAcceptsRegisteredLegacySizes(t *testing.T) {
	testCases := []struct {
		name           string
		size           string
		wantResolution string
		wantAspect     string
	}{
		{name: "landscape 720p alias", size: "1280x720", wantResolution: "720p", wantAspect: "16:9"},
		{name: "portrait 720p alias", size: "720x1280", wantResolution: "720p", wantAspect: "9:16"},
		{name: "landscape 1080p alias", size: "1920x1080", wantResolution: "1080p", wantAspect: "16:9"},
		{name: "portrait 1080p alias", size: "1080x1920", wantResolution: "1080p", wantAspect: "9:16"},
		{name: "legacy quality label", size: "720P", wantResolution: "720p"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
				Prompt: "draw a cat",
				Model:  "seedance-2.0",
				Size:   testCase.size,
			}, &relaycommon.RelayInfo{})

			require.NoError(t, err)
			assert.Equal(t, testCase.wantResolution, payload.Params.Resolution)
			assert.Equal(t, testCase.wantAspect, payload.Params.AspectRatio)
		})
	}
}

func TestConvertToRequestPayloadRejectsUnknownPixelSize(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "draw a cat",
		Model:  "seedance-2.0",
		Size:   "960x540",
	}, &relaycommon.RelayInfo{})

	require.Error(t, err)
	assert.Nil(t, payload)
	assert.Contains(t, err.Error(), `size "960x540" is not supported by AGGC`)
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

func TestBuildRequestBodyPrefersCanonicalResolutionOverLegacySize(t *testing.T) {
	payload := buildAggcRequestPayload(t, `{
		"model":"seedance-2.0",
		"prompt":"draw a cat",
		"size":"960x540",
		"resolution":"1080p",
		"metadata":{"resolution":"720p"}
	}`)

	assert.Equal(t, "16:9", payload.Params.AspectRatio)
	assert.Equal(t, "1080p", payload.Params.Resolution)
}

func TestValidateRequestNormalizesVideoOutputAliases(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-2.0",
		"prompt":"draw a cat",
		"aspectRatio":"32:18",
		"resolution":"1080P"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{OriginModelName: "seedance-2.0", TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "16:9", req.AspectRatio)
	assert.Equal(t, "1080p", req.Resolution)
}

func TestValidateRequestRejectsConflictingVideoOutputAliases(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-2.0",
		"prompt":"draw a cat",
		"ratio":"16:9",
		"aspectRatio":"9:16"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, "conflicts with aspect_ratio")
}

func TestValidateMappedRequestRejectsUnknownPixelSizeBeforeBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"seedance-2.0",
		"prompt":"draw a cat",
		"size":"960x540"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Contains(t, taskErr.Message, `size "960x540" is not supported by AGGC`)
}
