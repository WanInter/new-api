package tencentvod

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCredentials(t *testing.T) {
	testCases := []struct {
		name    string
		key     string
		want    credentials
		wantErr string
	}{
		{
			name: "valid",
			key:  "AKIDEXAMPLE|secret-value|1500000000",
			want: credentials{SecretID: "AKIDEXAMPLE", SecretKey: "secret-value", SubAppID: 1500000000},
		},
		{name: "missing sub app", key: "AKIDEXAMPLE|secret-value", wantErr: "expected SecretId|SecretKey|SubAppId"},
		{name: "invalid sub app", key: "AKIDEXAMPLE|secret-value|not-a-number", wantErr: "positive integer"},
		{name: "empty secret", key: "AKIDEXAMPLE||1500000000", wantErr: "SecretId and SecretKey are required"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseCredentials(testCase.key)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestInitWithoutChannelMetaUsesDefaultEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{})

	assert.Equal(t, DefaultBaseURL, adaptor.baseURL)
	assert.Empty(t, adaptor.apiKey)
}

func TestSignRequestMatchesTC3Contract(t *testing.T) {
	body := []byte(`{"SubAppId":123,"ModelName":"Kling"}`)
	req, err := http.NewRequest(http.MethodPost, DefaultBaseURL, bytes.NewReader(body))
	require.NoError(t, err)
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)

	err = signRequest(req, body, credentials{
		SecretID:  "AKIDEXAMPLE",
		SecretKey: "dummy-secret-key",
		SubAppID:  123,
	}, createTaskAction, now)
	require.NoError(t, err)

	assert.Equal(t, createTaskAction, req.Header.Get("X-TC-Action"))
	assert.Equal(t, tencentCloudVersion, req.Header.Get("X-TC-Version"))
	assert.Equal(t, "1735787045", req.Header.Get("X-TC-Timestamp"))
	assert.Equal(t,
		"TC3-HMAC-SHA256 Credential=AKIDEXAMPLE/2025-01-02/vod/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=9f8e130582299d805a40efb48d5d62f369166892d1167b6e0b051f690a9dc6bc",
		req.Header.Get("Authorization"),
	)
}

func TestConvertToRequestPayloadTextToVideo(t *testing.T) {
	seed := int64(42)
	payload, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "kling-vod-3.0",
		Prompt:   "a spacecraft takes off",
		Duration: 8,
		Size:     "720x1280",
		Metadata: map[string]any{
			"negative_prompt":  "blurred",
			"resolution":       "1080p",
			"audio_generation": false,
			"seed":             seed,
		},
	}, newRelayInfo("kling-vod-3.0"), 1500000000)
	require.NoError(t, err)

	assert.Equal(t, uint64(1500000000), payload.SubAppID)
	assert.Equal(t, "Kling", payload.ModelName)
	assert.Equal(t, "3.0", payload.ModelVersion)
	assert.Equal(t, "blurred", payload.NegativePrompt)
	assert.Equal(t, "1080P", payload.OutputConfig.Resolution)
	assert.Equal(t, "9:16", payload.OutputConfig.AspectRatio)
	assert.Equal(t, "Disabled", payload.OutputConfig.AudioGeneration)
	require.NotNil(t, payload.OutputConfig.Duration)
	assert.Equal(t, float64(8), *payload.OutputConfig.Duration)
	require.NotNil(t, payload.Seed)
	assert.Equal(t, seed, *payload.Seed)
	assert.Equal(t, "task_public", payload.SessionID)
}

func TestConvertToRequestPayloadPrefersTopLevelOutputFields(t *testing.T) {
	payload, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:       "kling-vod-3.0",
		Prompt:      "a spacecraft takes off",
		AspectRatio: "16:9",
		Resolution:  "1080p",
		Metadata: map[string]any{
			"aspect_ratio": "9:16",
			"resolution":   "720p",
		},
	}, newRelayInfo("kling-vod-3.0"), 1500000000)
	require.NoError(t, err)

	assert.Equal(t, "16:9", payload.OutputConfig.AspectRatio)
	assert.Equal(t, "1080P", payload.OutputConfig.Resolution)
}

func TestConvertToRequestPayloadImageAndTailFrame(t *testing.T) {
	payload, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "kling-vod-2.1",
		Prompt:   "the subject turns around",
		Duration: 5,
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/last.png",
		},
		Metadata: map[string]any{"resolution": "1080P"},
	}, newRelayInfo("kling-vod-2.1"), 1500000000)
	require.NoError(t, err)

	require.Len(t, payload.FileInfos, 1)
	assert.Equal(t, "Url", payload.FileInfos[0].Type)
	assert.Equal(t, "Image", payload.FileInfos[0].Category)
	assert.Equal(t, "FirstFrame", payload.FileInfos[0].Usage)
	assert.Equal(t, "https://example.com/first.png", payload.FileInfos[0].URL)
	assert.Equal(t, "https://example.com/last.png", payload.LastFrameURL)
}

func TestConvertToRequestPayloadRejectsUnsupportedTailFrameCombination(t *testing.T) {
	_, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "kling-vod-3.0",
		Prompt:   "transition",
		Images:   []string{"https://example.com/first.png", "https://example.com/last.png"},
		Metadata: map[string]any{"resolution": "1080P"},
	}, newRelayInfo("kling-vod-3.0"), 1500000000)
	require.ErrorContains(t, err, "requires model version 2.1")
}

func TestConvertToRequestPayloadRejectsTailFrameWithoutFirstFrame(t *testing.T) {
	_, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "kling-vod-2.1",
		Prompt:   "transition",
		Metadata: map[string]any{"resolution": "1080P", "last_frame_url": "https://example.com/last.png"},
	}, newRelayInfo("kling-vod-2.1"), 1500000000)
	require.ErrorContains(t, err, "requires a first-frame image")
}

func TestConvertToRequestPayloadCountsPromptCharacters(t *testing.T) {
	_, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:  "kling-vod-3.0",
		Prompt: strings.Repeat("中", maxPromptLength),
	}, newRelayInfo("kling-vod-3.0"), 1500000000)
	require.NoError(t, err)

	_, err = convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:  "kling-vod-3.0",
		Prompt: strings.Repeat("中", maxPromptLength+1),
	}, newRelayInfo("kling-vod-3.0"), 1500000000)
	require.ErrorContains(t, err, "prompt must not exceed")
}

func TestConvertToRequestPayloadSupportsReferenceImages(t *testing.T) {
	payload, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:  "kling-vod-3.0-omni",
		Prompt: "use both characters",
		Images: []string{
			"https://example.com/one.png",
			"data:image/png;base64,YWJj",
		},
		Metadata: map[string]any{"image_usage": "Reference"},
	}, newRelayInfo("kling-vod-3.0-omni"), 1500000000)
	require.NoError(t, err)
	require.Len(t, payload.FileInfos, 2)
	assert.Equal(t, "Reference", payload.FileInfos[0].Usage)
	assert.Equal(t, "Base64", payload.FileInfos[1].Type)
	assert.Equal(t, "YWJj", payload.FileInfos[1].Base64)
}

func TestConvertToRequestPayloadMapsUnifiedImageAliases(t *testing.T) {
	payload, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:          "kling-vod-3.0-omni",
		Prompt:         "use the reference images",
		ImageURLs:      []string{"https://example.com/legacy.png"},
		InputReference: "https://example.com/input-reference.png",
		Content: []relaycommon.TaskContentItem{
			{Type: "image_url", ImageURL: &relaycommon.TaskContentURL{URL: "https://example.com/content.png"}},
		},
		Metadata: map[string]any{"image_usage": "Reference"},
	}, newRelayInfo("kling-vod-3.0-omni"), 1500000000)
	require.NoError(t, err)

	require.Len(t, payload.FileInfos, 3)
	assert.Equal(t, []string{
		"https://example.com/legacy.png",
		"https://example.com/input-reference.png",
		"https://example.com/content.png",
	}, []string{payload.FileInfos[0].URL, payload.FileInfos[1].URL, payload.FileInfos[2].URL})
}

func TestConvertToRequestPayloadRejectsUnsupportedUnifiedVideoAndAudioReferences(t *testing.T) {
	testCases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{
			name: "top level video",
			req:  relaycommon.TaskSubmitReq{Videos: []string{"https://example.com/reference.mp4"}},
			want: "video reference",
		},
		{
			name: "top level audio",
			req:  relaycommon.TaskSubmitReq{Audios: []string{"https://example.com/reference.mp3"}},
			want: "audio reference",
		},
		{
			name: "content video",
			req: relaycommon.TaskSubmitReq{Content: []relaycommon.TaskContentItem{
				{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "https://example.com/reference.mp4"}},
			}},
			want: "video reference",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.req.Model = "kling-vod-3.0"
			testCase.req.Prompt = "animate"
			_, err := convertToRequestPayload(&testCase.req, newRelayInfo("kling-vod-3.0"), 1500000000)

			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestConvertToRequestPayloadRejectsConflictingNativeAndPublicImageInputs(t *testing.T) {
	_, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:  "kling-vod-3.0",
		Prompt: "animate",
		Images: []string{"https://example.com/public.png"},
		Metadata: map[string]any{
			"file_infos": []map[string]any{{"Type": "Url", "Category": "Image", "Url": "https://example.com/native.png"}},
		},
	}, newRelayInfo("kling-vod-3.0"), 1500000000)

	require.ErrorContains(t, err, "cannot combine public image inputs with metadata.file_infos")
}

func TestDoResponseReturnsPublicTaskAndStoresUpstreamTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	adaptor := &TaskAdaptor{}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"Response":{"TaskId":"vod-task-1","RequestId":"request-1"}}`)),
	}
	info := newRelayInfo("kling-vod-2.6")

	taskID, data, taskErr := adaptor.DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "vod-task-1", taskID)
	require.NotEmpty(t, data)
	assert.Equal(t, http.StatusOK, recorder.Code)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &video))
	assert.Equal(t, "task_public", video.ID)
	assert.Equal(t, "kling-vod-2.6", video.Model)
	assert.Equal(t, dto.VideoStatusQueued, video.Status)
}

func TestDoResponseReturnsTencentCloudError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"Response":{"Error":{"Code":"UnauthorizedOperation","Message":"permission denied"},"RequestId":"request-1"}}`)),
	}

	_, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, newRelayInfo("kling-vod-2.6"))
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "UnauthorizedOperation")
}

func TestParseTaskResult(t *testing.T) {
	testCases := []struct {
		name           string
		body           string
		wantStatus     string
		wantProgress   string
		wantURL        string
		wantReasonPart string
	}{
		{
			name:       "processing",
			body:       `{"Response":{"TaskType":"AigcVideoTask","Status":"PROCESSING","AigcVideoTask":{"TaskId":"vod-1","Status":"PROCESSING","Progress":47,"ErrCode":0},"RequestId":"request-1"}}`,
			wantStatus: string(model.TaskStatusInProgress), wantProgress: "47%",
		},
		{
			name:       "success",
			body:       `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","AigcVideoTask":{"TaskId":"vod-1","Status":"FINISH","Progress":100,"ErrCode":0,"Output":{"FileInfos":[{"StorageMode":"Temporary","FileType":"mp4","FileUrl":"https://example.com/video.mp4"}]}},"RequestId":"request-1"}}`,
			wantStatus: string(model.TaskStatusSuccess), wantProgress: "100%", wantURL: "https://example.com/video.mp4",
		},
		{
			name:       "success with zero extended code",
			body:       `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","AigcVideoTask":{"TaskId":"vod-1","Status":"FINISH","Progress":100,"ErrCode":0,"ErrCodeExt":"0","Output":{"FileInfos":[{"FileUrl":"https://example.com/video.mp4"}]}}}}`,
			wantStatus: string(model.TaskStatusSuccess), wantProgress: "100%", wantURL: "https://example.com/video.mp4",
		},
		{
			name:       "provider failure",
			body:       `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","AigcVideoTask":{"TaskId":"vod-1","Status":"FINISH","Progress":100,"ErrCode":1,"ErrCodeExt":"InvalidParameter.VoilationContent","Message":"content rejected"},"RequestId":"request-1"}}`,
			wantStatus: string(model.TaskStatusFailure), wantProgress: "100%", wantReasonPart: "content rejected",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(testCase.body))
			require.NoError(t, err)
			assert.Equal(t, testCase.wantStatus, info.Status)
			assert.Equal(t, testCase.wantProgress, info.Progress)
			assert.Equal(t, testCase.wantURL, info.Url)
			if testCase.wantReasonPart != "" {
				assert.Contains(t, info.Reason, testCase.wantReasonPart)
			}
		})
	}
}

func TestModelListIncludesTencentVODKlingVersions(t *testing.T) {
	assert.Equal(t, []string{
		"kling-vod-1.6",
		"kling-vod-2.0",
		"kling-vod-2.1",
		"kling-vod-2.5",
		"kling-vod-2.6",
		"kling-vod-o1",
		"kling-vod-3.0",
		"kling-vod-3.0-omni",
	}, (&TaskAdaptor{}).GetModelList())
}

func newRelayInfo(modelName string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: modelName,
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
}
