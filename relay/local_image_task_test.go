package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageFetchBuilderSupportsImageGenerationFetch(t *testing.T) {
	builder, ok := fetchRespBuilders[relayconstant.RelayModeImagesGenerations]

	require.True(t, ok)
	require.NotNil(t, builder)
}

func TestLocalImageSuccessStatusAllowsReplicateCreated(t *testing.T) {
	require.True(t, isLocalImageSuccessStatus(http.StatusCreated, constant.APITypeReplicate))
	require.False(t, isLocalImageSuccessStatus(http.StatusCreated, constant.APITypeOpenAI))
}

func TestLocalImageTransientStatus(t *testing.T) {
	require.True(t, isLocalImageTransientStatus(http.StatusTooManyRequests))
	require.True(t, isLocalImageTransientStatus(http.StatusBadGateway))
	require.False(t, isLocalImageTransientStatus(http.StatusBadRequest))
}

func TestLocalImageTransientErrorClassification(t *testing.T) {
	err := newLocalImageTransientError("temporary", errors.New("upstream unavailable"))

	require.True(t, isLocalImageTransientError(err))
	require.ErrorContains(t, err, "temporary")
}

func TestLocalImageRequestModeUsesEditForOpenAIReferenceImages(t *testing.T) {
	var req dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gpt-image-2-c",
		"prompt":"edit",
		"images":["https://example.com/input.png"]
	}`), &req))

	mode, path := localImageRequestMode(constant.APITypeOpenAI, req)

	require.Equal(t, relayconstant.RelayModeImagesEdits, mode)
	require.Equal(t, localImageEditPath, path)
}

func TestLocalImageRequestModeKeepsGenerationWithoutReferenceImages(t *testing.T) {
	req := dto.ImageRequest{Model: "gpt-image-2-c", Prompt: "cat"}

	mode, path := localImageRequestMode(constant.APITypeOpenAI, req)

	require.Equal(t, relayconstant.RelayModeImagesGenerations, mode)
	require.Equal(t, localImageGenerationPath, path)
}

func TestLocalImageRequestModeKeepsNonOpenAIProviderGeneration(t *testing.T) {
	var req dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"doubao-image",
		"prompt":"edit",
		"images":["https://example.com/input.png"]
	}`), &req))

	mode, path := localImageRequestMode(constant.APITypeVolcEngine, req)

	require.Equal(t, relayconstant.RelayModeImagesGenerations, mode)
	require.Equal(t, localImageGenerationPath, path)
}

func TestBuildLocalImageRequestBodyFlattensOpenAIParameters(t *testing.T) {
	var imageReq dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"nano-banana-pro",
		"prompt":"cat",
		"n":1,
		"parameters":{
			"size":"1K",
			"n":1,
			"prompt_extend":true,
			"watermark":false,
			"aspect_ratio":"9:16"
		}
	}`), &imageReq))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, localImageGenerationPath, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesGenerations,
		RelayFormat: types.RelayFormatOpenAIImage,
		ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI},
	}

	body, err := buildLocalImageRequestBody(c, &openai.Adaptor{}, info, imageReq)
	require.NoError(t, err)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)

	var upstream map[string]any
	require.NoError(t, common.Unmarshal(bodyBytes, &upstream))
	assert.Equal(t, "1K", upstream["size"])
	assert.Equal(t, "9:16", upstream["aspect_ratio"])
	assert.Equal(t, float64(1), upstream["n"])
	assert.Equal(t, true, upstream["prompt_extend"])
	assert.Equal(t, false, upstream["watermark"])
	assert.NotContains(t, upstream, "parameters")
}

func TestBuildLocalImageRequestBodyConvertsJSONReferencesToMultipart(t *testing.T) {
	referenceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("reference-" + r.URL.Path))
	}))
	defer referenceServer.Close()

	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	referenceURL, err := url.Parse(referenceServer.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = append([]string(nil), originalFetchSetting.AllowedPorts...)
	fetchSetting.AllowedPorts = append(fetchSetting.AllowedPorts, referenceURL.Port())
	originalMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 10
	t.Cleanup(func() {
		*fetchSetting = originalFetchSetting
		constant.MaxFileDownloadMB = originalMaxFileDownloadMB
	})
	service.InitHttpClient()

	tests := []struct {
		name       string
		imagesJSON string
		fieldName  string
		wantImages []string
	}{
		{
			name:       "single reference",
			imagesJSON: `[{"image_url":"` + referenceServer.URL + `/one.png"}]`,
			fieldName:  "image",
			wantImages: []string{"reference-/one.png"},
		},
		{
			name:       "multiple references",
			imagesJSON: `[{"image_url":"` + referenceServer.URL + `/one.png"},{"image_url":"` + referenceServer.URL + `/two.png"}]`,
			fieldName:  "image[]",
			wantImages: []string{"reference-/one.png", "reference-/two.png"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var imageReq dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(`{
				"model":"gpt-image-2",
				"prompt":"combine references",
				"images":`+test.imagesJSON+`,
				"parameters":{
					"size":"1024x1024",
					"quality":"medium",
					"background":"auto",
					"output_format":"png",
					"prompt_extend":true,
					"watermark":false
				}
			}`), &imageReq))

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, localImageEditPath, nil)
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{
				RelayMode:   relayconstant.RelayModeImagesEdits,
				RelayFormat: types.RelayFormatOpenAIImage,
				ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI},
			}

			body, err := buildLocalImageRequestBody(c, &openai.Adaptor{}, info, imageReq)
			require.NoError(t, err)
			bodyBytes, err := io.ReadAll(body)
			require.NoError(t, err)
			mediaType, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
			require.NoError(t, err)
			require.Equal(t, "multipart/form-data", mediaType)
			reader := multipart.NewReader(bytes.NewReader(bodyBytes), params["boundary"])

			fields := map[string]string{}
			images := make([]string, 0, len(test.wantImages))
			for {
				part, err := reader.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				value, err := io.ReadAll(part)
				require.NoError(t, err)
				if part.FileName() != "" {
					require.Equal(t, test.fieldName, part.FormName())
					images = append(images, string(value))
					continue
				}
				fields[part.FormName()] = string(value)
			}

			require.Equal(t, test.wantImages, images)
			assert.Equal(t, "gpt-image-2", fields["model"])
			assert.Equal(t, "combine references", fields["prompt"])
			assert.Equal(t, "1024x1024", fields["size"])
			assert.Equal(t, "medium", fields["quality"])
			assert.Equal(t, "auto", fields["background"])
			assert.Equal(t, "png", fields["output_format"])
			assert.NotContains(t, fields, "prompt_extend")
			assert.NotContains(t, fields, "watermark")
		})
	}
}

func TestBuildLocalImageRequestBodyCancelsReferenceDownload(t *testing.T) {
	requestStarted := make(chan struct{})
	referenceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer referenceServer.Close()

	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	referenceURL, err := url.Parse(referenceServer.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = append([]string(nil), originalFetchSetting.AllowedPorts...)
	fetchSetting.AllowedPorts = append(fetchSetting.AllowedPorts, referenceURL.Port())
	t.Cleanup(func() { *fetchSetting = originalFetchSetting })
	service.InitHttpClient()

	var imageReq dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gpt-image-2",
		"prompt":"edit reference",
		"images":[{"image_url":"`+referenceServer.URL+`/blocked.png"}]
	}`), &imageReq))

	requestContext, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequestWithContext(requestContext, http.MethodPost, localImageEditPath, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesEdits,
		RelayFormat: types.RelayFormatOpenAIImage,
		ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI},
	}

	result := make(chan error, 1)
	go func() {
		_, err := buildLocalImageRequestBody(c, &openai.Adaptor{}, info, imageReq)
		result <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("reference download did not start")
	}
	cancel()

	select {
	case err := <-result:
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("reference download did not stop after request cancellation")
	}
}

func TestNormalizeLocalImageRequestKeepsExplicitTopLevelValues(t *testing.T) {
	var imageReq dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"nano-banana-pro",
		"prompt":"cat",
		"size":"2K",
		"aspect_ratio":"16:9",
		"parameters":{"size":"1K","aspect_ratio":"9:16"}
	}`), &imageReq))

	require.NoError(t, normalizeLocalImageRequest(constant.APITypeOpenAI, &imageReq))
	requestJSON, err := imageReq.MarshalJSONWithExtra()
	require.NoError(t, err)
	var normalized map[string]any
	require.NoError(t, common.Unmarshal(requestJSON, &normalized))
	assert.Equal(t, "2K", normalized["size"])
	assert.Equal(t, "16:9", normalized["aspect_ratio"])
	assert.NotContains(t, normalized, "parameters")
}

func TestNormalizeLocalImageRequestKeepsNativeParameters(t *testing.T) {
	var imageReq dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"wanx-v1",
		"prompt":"cat",
		"parameters":{"size":"1024*1792","aspect_ratio":"9:16"}
	}`), &imageReq))

	require.NoError(t, normalizeLocalImageRequest(constant.APITypeAli, &imageReq))
	requestJSON, err := imageReq.MarshalJSONWithExtra()
	require.NoError(t, err)
	var unchanged map[string]any
	require.NoError(t, common.Unmarshal(requestJSON, &unchanged))
	assert.Contains(t, unchanged, "parameters")
}
