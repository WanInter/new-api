package shishi

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyNormalizesSoraContentAndPreservesDuplicateReferences(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-video-model",
		"prompt":"animate the references",
		"ratio":"16:9",
		"reference_image_urls":["https://example.com/image.png", "https://example.com/image.png"],
		"reference_videos":["https://example.com/video.mp4", "https://example.com/video.mp4"],
		"reference_audios":["https://example.com/audio.mp3", "https://example.com/audio.mp3"],
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/image.png"}},
			{"type":"video_url","video_url":"https://example.com/video.mp4"},
			{"type":"audio_url","url":"https://example.com/audio.mp3"}
		]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "sd-720-pro"},
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "sd-720-pro", got["model"])
	assert.Equal(t, "16:9", got["aspect_ratio"])
	assert.NotContains(t, got, "content")
	assert.Equal(t, []any{
		"https://example.com/image.png",
		"https://example.com/image.png",
		"https://example.com/image.png",
	}, got["reference_image_urls"])
	assert.Equal(t, []any{
		"https://example.com/video.mp4",
		"https://example.com/video.mp4",
		"https://example.com/video.mp4",
	}, got["reference_videos"])
	assert.Equal(t, []any{
		"https://example.com/audio.mp3",
		"https://example.com/audio.mp3",
		"https://example.com/audio.mp3",
	}, got["reference_audios"])
}

func TestBuildRequestBodyMapsSecondsAliasToDurationWithoutChangingExplicitZero(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		request      string
		wantDuration any
	}{
		{
			name:         "seconds alias",
			request:      `{"model":"public-video-model","prompt":"animate","seconds":0}`,
			wantDuration: float64(0),
		},
		{
			name:         "duration takes precedence",
			request:      `{"model":"public-video-model","prompt":"animate","duration":15,"seconds":4}`,
			wantDuration: float64(15),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.request))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "shishi-upstream-model"},
			})
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			assert.Equal(t, testCase.wantDuration, got["duration"])
			assert.NotContains(t, got, "seconds")
		})
	}
}

func TestBuildRequestBodyUsesVeoIngredientsForMoreThanTwoImages(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"veo-omni-flash",
		"prompt":"animate the references",
		"duration":10,
		"images":[
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png"
		]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "veo-omni-flash",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "veo-omni-flash"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, float64(10), got["duration"])
	assert.NotContains(t, got, "seconds")
	assert.NotContains(t, got, "images")
	assert.Equal(t, []any{
		"https://example.com/1.png",
		"https://example.com/2.png",
		"https://example.com/3.png",
	}, got["Ingredients_images"])
}

func TestBuildRequestBodyNormalizesVideoOutputAliases(t *testing.T) {
	testCases := []struct {
		name            string
		request         string
		wantAspectRatio string
		wantResolution  string
	}{
		{
			name:            "ratio alias",
			request:         `{"model":"public-video-model","prompt":"animate","ratio":"32:18","resolution":"720P"}`,
			wantAspectRatio: "16:9",
			wantResolution:  "720p",
		},
		{
			name:            "camel aspect ratio alias",
			request:         `{"model":"public-video-model","prompt":"animate","aspectRatio":"9:16","resolution":"1080P"}`,
			wantAspectRatio: "9:16",
			wantResolution:  "1080p",
		},
		{
			name:            "metadata fallback",
			request:         `{"model":"public-video-model","prompt":"animate","metadata":{"ratio":"4:3","resolution":"720P"}}`,
			wantAspectRatio: "4:3",
			wantResolution:  "720p",
		},
		{
			name:            "encoded metadata fallback",
			request:         `{"model":"public-video-model","prompt":"animate","metadata":"{\"ratio\":\"4:3\",\"resolution\":\"720P\",\"provider_option\":true}"}`,
			wantAspectRatio: "4:3",
			wantResolution:  "720p",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.request))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			adaptor := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-video-model",
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "shishi-upstream-model"},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			req, err := relaycommon.GetTaskRequest(c)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantAspectRatio, req.AspectRatio)
			assert.Equal(t, testCase.wantResolution, req.Resolution)

			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			assert.Equal(t, testCase.wantAspectRatio, got["aspect_ratio"])
			assert.Equal(t, testCase.wantResolution, got["resolution"])
			assert.NotContains(t, got, "ratio")
			assert.NotContains(t, got, "aspectRatio")
			if testCase.name == "encoded metadata fallback" {
				rawMetadata, ok := got["metadata"].(string)
				require.True(t, ok)
				var metadata map[string]any
				require.NoError(t, common.UnmarshalJsonStr(rawMetadata, &metadata))
				assert.Equal(t, "4:3", metadata["aspect_ratio"])
				assert.Equal(t, "720p", metadata["resolution"])
				assert.Equal(t, true, metadata["provider_option"])
				assert.NotContains(t, metadata, "ratio")
			}
		})
	}
}

func TestBuildRequestBodyNormalizesMultipartVideoOutputAndMetadata(t *testing.T) {
	var input bytes.Buffer
	inputWriter := multipart.NewWriter(&input)
	require.NoError(t, inputWriter.WriteField("model", "public-video-model"))
	require.NoError(t, inputWriter.WriteField("prompt", "animate"))
	require.NoError(t, inputWriter.WriteField("ratio", "32:18"))
	require.NoError(t, inputWriter.WriteField("aspectRatio", "16:9"))
	require.NoError(t, inputWriter.WriteField("size", "0960X0540"))
	require.NoError(t, inputWriter.WriteField("resolution", "1080P"))
	require.NoError(t, inputWriter.WriteField("metadata", `{
		"ratio":"9:16",
		"aspectRatio":"9:16",
		"aspect_ratio":"9:16",
		"size":"720x1280",
		"resolution":"720P",
		"provider_option":{"keep":true}
	}`))
	require.NoError(t, inputWriter.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", inputWriter.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "shishi-upstream-model"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()

	require.Equal(t, []string{"shishi-upstream-model"}, form.Value["model"])
	assert.Equal(t, []string{"960x540"}, form.Value["size"])
	assert.Equal(t, []string{"16:9"}, form.Value["aspect_ratio"])
	assert.Equal(t, []string{"1080p"}, form.Value["resolution"])
	assert.NotContains(t, form.Value, "ratio")
	assert.NotContains(t, form.Value, "aspectRatio")

	var metadata map[string]any
	require.NoError(t, common.UnmarshalJsonStr(form.Value["metadata"][0], &metadata))
	assert.Equal(t, "960x540", metadata["size"])
	assert.Equal(t, "16:9", metadata["aspect_ratio"])
	assert.Equal(t, "1080p", metadata["resolution"])
	assert.NotContains(t, metadata, "ratio")
	assert.NotContains(t, metadata, "aspectRatio")
	assert.Equal(t, map[string]any{"keep": true}, metadata["provider_option"])
}

func TestValidateRequestRejectsConflictingSizeAndRatio(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"public-video-model","prompt":"animate","size":"960x540","ratio":"9:16"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{
		OriginModelName: "public-video-model",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	})

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_video_output", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "conflicts with aspect_ratio")
}

func TestTaskRequestFromPayloadPrefersCanonicalDuration(t *testing.T) {
	req, _, err := taskRequestFromPayload(map[string]any{
		"model":    "shishi-upstream-model",
		"prompt":   "animate",
		"duration": 15,
		"seconds":  4,
	}, "")

	require.NoError(t, err)
	assert.Equal(t, 15, req.Duration)
}

func TestParseTaskResultNormalizesNestedUniversalResponse(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"data":{
			"taskId":"upstream-task",
			"status":"succeeded",
			"outputs":[{"download_url":"https://example.com/result.mp4"}]
		}
	}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "upstream-task", result.TaskID)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, "https://example.com/result.mp4", result.Url)
}

func TestMapTaskStatusSupportsDocumentedAliases(t *testing.T) {
	for _, testCase := range []struct {
		status string
		want   model.TaskStatus
	}{
		{status: "submitted", want: model.TaskStatusQueued},
		{status: "generating", want: model.TaskStatusInProgress},
		{status: "finished", want: model.TaskStatusSuccess},
		{status: "canceled", want: model.TaskStatusFailure},
	} {
		t.Run(testCase.status, func(t *testing.T) {
			assert.Equal(t, testCase.want, mapTaskStatus(testCase.status))
		})
	}
}

func TestDoResponseReturnsPublicOpenAIVideoTaskID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-video-model",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	response := &http.Response{Body: io.NopCloser(strings.NewReader(`{
		"data":{"taskId":"upstream-task","status":"queued","progress":0}
	}`))}

	upstreamTaskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, response, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-task", upstreamTaskID)
	require.Equal(t, http.StatusOK, recorder.Code)

	var got dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &got))
	assert.Equal(t, "task_public", got.ID)
	assert.Equal(t, "task_public", got.TaskID)
	assert.Equal(t, "public-video-model", got.Model)
	assert.Equal(t, dto.VideoStatusQueued, got.Status)
	assert.Zero(t, got.Progress)
	assert.Positive(t, got.CreatedAt)
	assert.NotContains(t, recorder.Body.String(), "upstream-task")
}

func TestConvertToOpenAIVideoUsesContentProxy(t *testing.T) {
	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.test"
	t.Cleanup(func() { system_setting.ServerAddress = oldServerAddress })

	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1782570791,
		UpdatedAt: 1782571022,
		Properties: model.Properties{
			OriginModelName: "public-video-model",
		},
		Data: []byte(`{
			"data":{
				"taskId":"upstream-task",
				"status":"completed",
				"outputs":[{"download_url":"https://example.com/result.mp4"}]
			}
		}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	proxyURL := "https://api.example.test/v1/videos/task_public/content"
	assert.Equal(t, "task_public", got["id"])
	assert.Equal(t, "task_public", got["task_id"])
	assert.Equal(t, "public-video-model", got["model"])
	assert.Equal(t, dto.VideoStatusCompleted, got["status"])
	assert.Equal(t, proxyURL, got["url"])
	assert.Equal(t, proxyURL, got["video_url"])
	assert.Equal(t, proxyURL, got["result_url"])
	assert.Equal(t, []any{proxyURL}, got["output"])
	assert.NotContains(t, got, "data")
}

func TestConvertToOpenAIVideoFailureUsesStoredReason(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "model capacity is unavailable",
		Properties: model.Properties{
			OriginModelName: "public-video-model",
		},
		Data: []byte(`{"data":{"status":"failed"}}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &got))
	require.NotNil(t, got.Error)
	assert.Equal(t, dto.VideoStatusFailed, got.Status)
	assert.Equal(t, "model capacity is unavailable", got.Error.Message)
}

func TestBuildPrivateDataStoresSelectedKey(t *testing.T) {
	privateData, err := (&TaskAdaptor{}).BuildPrivateData(nil, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "selected-key"},
	})

	require.NoError(t, err)
	require.NotNil(t, privateData)
	assert.Equal(t, "selected-key", privateData.Key)
}
