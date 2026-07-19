package seventhframe

import (
	"context"
	"io"
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

func TestBuildRequestBodyUploadsAssetsAndPreservesFileObjects(t *testing.T) {
	disableSSRFProtection(t)
	gin.SetMode(gin.TestMode)

	uploaded := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/source/first.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image-one"))
		case "/source/second.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("image-two"))
		case "/source/voice.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("audio-one"))
		case "/api/v1/files":
			require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
			require.NoError(t, r.ParseMultipartForm(1024*1024))
			file, header, err := r.FormFile("file")
			require.NoError(t, err)
			defer file.Close()
			contents, err := io.ReadAll(file)
			require.NoError(t, err)
			uploaded = append(uploaded, header.Filename+":"+string(contents))
			_, _ = w.Write([]byte(`{"file":{"object":"file","id":"file-` + strings.TrimSuffix(header.Filename, ".png") + `","name":"` + header.Filename + `","url":"https://files.example/` + header.Filename + `","custom":{"retained":true}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := `{
		"model":"public-video-model",
		"prompt":"animate the references",
		"duration":4,
		"aspectRatio":"16:9",
		"resolution":"720p",
		"seed":"0",
		"images":["` + server.URL + `/source/first.png"],
		"image":"` + server.URL + `/source/first.png",
		"audios":["` + server.URL + `/source/voice.mp3"],
		"content":[{"type":"image_url","image_url":{"url":"` + server.URL + `/source/second.jpg"}}]
	}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "Seedance-2.0-719",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "upstream-key",
			ChannelBaseUrl:    server.URL + "/api/v1",
			UpstreamModelName: "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78",
			IsModelMapped:     true,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload generationRequest
	require.NoError(t, common.Unmarshal(encoded, &payload))
	assert.Equal(t, upstreamChannel, payload.Channel)
	assert.Equal(t, "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78", payload.Model)
	assert.Equal(t, "animate the references", payload.Prompt)
	require.NotNil(t, payload.Duration)
	assert.Equal(t, 4, *payload.Duration)
	assert.Equal(t, "16:9", payload.AspectRatio)
	assert.Equal(t, "720p", payload.Resolution)
	assert.Equal(t, "0", payload.Seed)
	require.Len(t, payload.Assets, 3)
	assert.Equal(t, []string{"first.png:image-one", "voice.mp3:audio-one", "second.jpg:image-two"}, uploaded)

	for _, asset := range payload.Assets {
		var file map[string]any
		require.NoError(t, common.Unmarshal(asset, &file))
		assert.Equal(t, "file", file["object"])
		assert.Equal(t, map[string]any{"retained": true}, file["custom"])
	}
}

func TestBuildRequestBodyUsesASCIIFilenameForUnicodeAssetURL(t *testing.T) {
	disableSSRFProtection(t)
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/source/示例人脸.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image-one"))
		case "/api/v1/files":
			reader, err := r.MultipartReader()
			require.NoError(t, err)
			part, err := reader.NextPart()
			require.NoError(t, err)
			defer part.Close()
			assert.Equal(t, `form-data; name="file"; filename="asset.png"`, part.Header.Get("Content-Disposition"))
			assert.Equal(t, "image/png", part.Header.Get("Content-Type"))
			contents, err := io.ReadAll(part)
			require.NoError(t, err)
			assert.Equal(t, "image-one", string(contents))
			_, _ = w.Write([]byte(`{"file":{"object":"file","id":"file-1","url":"https://files.example/asset.png"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := `{"model":"public-video-model","prompt":"animate the reference","images":["` + server.URL + `/source/%E7%A4%BA%E4%BE%8B%E4%BA%BA%E8%84%B8.png"]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "Seedance-2.0-719",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "upstream-key",
			ChannelBaseUrl:    server.URL + "/api/v1",
			UpstreamModelName: "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78",
			IsModelMapped:     true,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var payload generationRequest
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Len(t, payload.Assets, 1)
}

func TestValidateRequestRejectsUnsupportedAssetsAndOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "duration too short",
			body: `{"model":"video","prompt":"test","duration":3}`,
			want: "duration must be between 4 and 15 seconds",
		},
		{
			name: "unsupported ratio",
			body: `{"model":"video","prompt":"test","aspectRatio":"2:1"}`,
			want: "aspect_ratio must be one of",
		},
		{
			name: "video reference",
			body: `{"model":"video","prompt":"test","videos":["https://example.com/input.mp4"]}`,
			want: "video assets are not supported",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			})
			require.NotNil(t, taskErr)
			assert.Contains(t, taskErr.Message, testCase.want)
		})
	}
}

func TestValidateRequestNormalizesSecondsToDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"video","prompt":"test","seconds":"15"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{OriginModelName: "video", TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, 15, req.Duration)
}

func TestParseTaskResultMapsUpstreamStatuses(t *testing.T) {
	testCases := []struct {
		name         string
		body         string
		wantStatus   model.TaskStatus
		wantProgress string
		wantURL      string
		wantReason   string
	}{
		{
			name:         "queued",
			body:         `{"generation":{"id":"generation-1","status":"queued","progress":0}}`,
			wantStatus:   model.TaskStatusQueued,
			wantProgress: "20%",
		},
		{
			name:         "running",
			body:         `{"generation":{"id":"generation-1","status":"running","progress":57}}`,
			wantStatus:   model.TaskStatusInProgress,
			wantProgress: "57%",
		},
		{
			name:         "succeeded",
			body:         `{"generation":{"id":"generation-1","status":"succeeded","progress":100,"outputVideoUrl":"https://example.com/result.mp4"}}`,
			wantStatus:   model.TaskStatusSuccess,
			wantProgress: "100%",
			wantURL:      "https://example.com/result.mp4",
		},
		{
			name:         "blocked",
			body:         `{"generation":{"id":"generation-1","status":"blocked","errorMessage":"upstream unavailable"}}`,
			wantStatus:   model.TaskStatusFailure,
			wantProgress: "100%",
			wantReason:   "upstream unavailable",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(testCase.body))
			require.NoError(t, err)
			assert.Equal(t, string(testCase.wantStatus), result.Status)
			assert.Equal(t, testCase.wantProgress, result.Progress)
			assert.Equal(t, testCase.wantURL, result.Url)
			assert.Equal(t, testCase.wantReason, result.Reason)
		})
	}
}

func TestDoResponseReturnsPublicTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	response := &http.Response{Body: io.NopCloser(strings.NewReader(`{
		"generation":{"id":"upstream-generation","status":"queued","progress":0}
	}`))}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, response, &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	})
	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-generation", upstreamID)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &video))
	assert.Equal(t, "task_public", video.ID)
	assert.Equal(t, "task_public", video.TaskID)
	assert.Equal(t, "public-model", video.Model)
	assert.Equal(t, dto.VideoStatusQueued, video.Status)
}

func TestFetchTaskUsesGenerationEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/video/generations/generation-1", r.URL.EscapedPath())
		assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"generation":{"id":"generation-1","status":"queued"}}`))
	}))
	defer server.Close()

	resp, err := (&TaskAdaptor{}).FetchTask(context.Background(), server.URL+"/api/v1", "upstream-key", map[string]any{
		"task_id": "generation-1",
	}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBuildPrivateDataStoresSelectedKey(t *testing.T) {
	privateData, err := (&TaskAdaptor{}).BuildPrivateData(nil, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "selected-upstream-key"},
	})
	require.NoError(t, err)
	require.NotNil(t, privateData)
	assert.Equal(t, "selected-upstream-key", privateData.Key)
}

func disableSSRFProtection(t *testing.T) {
	t.Helper()
	settings := system_setting.GetFetchSetting()
	original := *settings
	settings.EnableSSRFProtection = false
	t.Cleanup(func() { *settings = original })
}
