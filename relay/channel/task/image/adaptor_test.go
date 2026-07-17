package image

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultSuccessUsesFirstImageURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"SUCCEEDED","results":[{"url":"https://example.com/image.png"}]}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/image.png", info.Url)
}

func TestParseTaskResultFailureUsesOutputMessage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"FAILED","message":"bad prompt"}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Equal(t, "bad prompt", info.Reason)
}

func TestParseTaskResultSuccessPreservesDataURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"SUCCEEDED","results":[{"b64_image":"data:image/jpeg;base64,/9j/abc"}]}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "data:image/jpeg;base64,/9j/abc", info.Url)
}

func TestParseTaskResultSuccessWrapsBareBase64(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"SUCCEEDED","results":[{"b64_image":"iVBORw0KGgo="}]}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "data:image/png;base64,iVBORw0KGgo=", info.Url)
}

func TestValidateRequestAllowsNonAliLocalAsync(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeOpenAI}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	require.Equal(t, constant.TaskActionImageGenerate, info.Action)
}

func TestValidateRequestAllowsNativeGeminiImageTask(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{
		"model":"nano-banana-2",
		"contents":[{"parts":[
			{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}},
			{"text":"edit this image"}
		]}],
		"generationConfig":{"candidateCount":1,"responseModalities":["IMAGE"]}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	require.Equal(t, constant.TaskActionImageGenerate, info.Action)
	ratios := adaptor.EstimateBilling(c, info)
	require.Equal(t, 1.0, ratios["n"])
}

func TestValidateRequestRejectsNativeGeminiMultipleCandidates(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{
		"model":"nano-banana-2",
		"contents":[{"parts":[{"text":"draw a cat"}]}],
		"generationConfig":{"candidateCount":2,"responseModalities":["IMAGE"]}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "invalid_request", taskErr.Code)
	require.ErrorContains(t, taskErr.Error, "candidateCount")
}

func TestValidateRequestRejectsNativeGeminiWithoutImageOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{
		"model":"nano-banana-2",
		"contents":[{"parts":[{"text":"describe a cat"}]}],
		"generationConfig":{"responseModalities":["TEXT"]}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.ErrorContains(t, taskErr.Error, "must include IMAGE")
}

func TestValidateRequestRejectsExplicitEmptyNativeGeminiOutputModalities(t *testing.T) {
	for _, modalities := range []string{"[]", "null"} {
		t.Run(modalities, func(t *testing.T) {
			body := fmt.Sprintf(`{
				"model":"nano-banana-2",
				"contents":[{"parts":[{"text":"draw a cat"}]}],
				"generationConfig":{"responseModalities":%s}
			}`, modalities)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
			adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

			taskErr := adaptor.ValidateRequestAndSetAction(c, info)

			require.NotNil(t, taskErr)
			require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			require.ErrorContains(t, taskErr.Error, "must include IMAGE")
		})
	}
}

func TestValidateRequestRejectsTooManyNativeGeminiImageInputs(t *testing.T) {
	parts := make([]string, maxNativeGeminiImageInputs+1)
	for index := range parts {
		parts[index] = `{"inlineData":{"mimeType":"image/png","data":"aQ=="}}`
	}
	body := fmt.Sprintf(`{"model":"nano-banana-2","contents":[{"parts":[%s]}]}`, strings.Join(parts, ","))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.ErrorContains(t, taskErr.Error, "at most 16 image inputs")
}

func TestValidateRequestRejectsOversizedLocalImageTask(t *testing.T) {
	originalLimit := constant.LocalImageTaskMaxInputMB
	constant.LocalImageTaskMaxInputMB = 1
	t.Cleanup(func() {
		constant.LocalImageTaskMaxInputMB = originalLimit
	})

	body := `{"model":"nano-banana-2","contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + strings.Repeat("a", 1<<20) + `"}}]}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, taskErr.StatusCode)
	require.Equal(t, "request_too_large", taskErr.Code)
}

func TestValidateRequestRejectsNativeGeminiPayloadForOpenAIChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{
		"model":"gpt-image-1",
		"contents":[{"parts":[{"text":"draw a cat"}]}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeOpenAI}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "unsupported_request_format", taskErr.Code)
}

func TestValidateMappedRequestRejectsNativeGeminiPayloadForImagenModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{
		"model":"nano-banana-2",
		"contents":[{"parts":[{"text":"draw a cat"}]}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "imagen-4.0-generate-001"}

	taskErr := adaptor.ValidateMappedRequest(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "unsupported_request_format", taskErr.Code)
	require.ErrorContains(t, taskErr.Error, "Imagen")
}

func TestValidateRequestUsesLocalAsyncForAliSyncImageModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{"model":"qwen-image","prompt":"cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeAli}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	require.False(t, adaptor.nativeAsync)
}

func TestValidateRequestUsesNativeAsyncForAliAsyncImageModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{"model":"wanx2.1-t2i-turbo","prompt":"cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeAli}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	require.True(t, adaptor.nativeAsync)
}

func TestBuildRequestBodyRecomputesAliLocalAsyncAfterModelMapping(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{"model":"alias-model","prompt":"cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeAli}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	require.True(t, adaptor.nativeAsync)

	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "qwen-image"}
	_, err := adaptor.BuildRequestBody(c, info)

	require.NoError(t, err)
	require.False(t, adaptor.nativeAsync)
}

func TestEstimateBillingIgnoresAliPromptExtendForNonAliChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat","parameters":{"prompt_extend":true},"n":2}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeOpenAI}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)

	ratios := adaptor.EstimateBilling(c, info)

	require.Equal(t, 2.0, ratios["n"])
	require.NotContains(t, ratios, "prompt_extend")
}

func TestBuildPrivateDataStoresLocalImageSnapshot(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	var imageReq dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"origin-model",
		"prompt":"cat",
		"parameters":{
			"size":"1K",
			"n":1,
			"prompt_extend":true,
			"watermark":false,
			"aspect_ratio":"9:16"
		}
	}`), &imageReq))
	c.Set(imageTaskRequestKey, &imageReq)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ApiType:           constant.APITypeOpenAI,
			ChannelBaseUrl:    "https://upstream.example.com",
			ApiKey:            "test-key",
			UpstreamModelName: "mapped-model",
		},
	}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeOpenAI}

	privateData, err := adaptor.BuildPrivateData(c, info)

	require.NoError(t, err)
	require.NotNil(t, privateData)
	require.Equal(t, "test-key", privateData.Key)
	require.NotNil(t, privateData.LocalImageTask)
	require.Equal(t, constant.ChannelTypeOpenAI, privateData.LocalImageTask.ChannelType)
	require.Equal(t, constant.APITypeOpenAI, privateData.LocalImageTask.APIType)
	require.Equal(t, "https://upstream.example.com", privateData.LocalImageTask.BaseURL)
	var stored map[string]any
	require.NoError(t, common.Unmarshal(privateData.LocalImageTask.Request, &stored))
	assert.Equal(t, "mapped-model", stored["model"])
	assert.Equal(t, "cat", stored["prompt"])
	assert.Equal(t, map[string]any{
		"size":          "1K",
		"n":             float64(1),
		"prompt_extend": true,
		"watermark":     false,
		"aspect_ratio":  "9:16",
	}, stored["parameters"])
}

func TestBuildPrivateDataPreservesNativeGeminiImageRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	var imageReq dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"nano-banana-2",
		"contents":[{"role":"user","parts":[
			{"inlineData":{"mimeType":"image/jpeg","data":"cmVmZXJlbmNl"}},
			{"text":"keep the subject and change the background"}
		]}],
		"generationConfig":{"responseModalities":["IMAGE"]}
	}`), &imageReq))
	c.Set(imageTaskRequestKey, &imageReq)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			ApiType:           constant.APITypeGemini,
			ChannelBaseUrl:    "https://upstream.example.com",
			ApiKey:            "test-key",
			UpstreamModelName: "gemini-3.1-flash-image-preview",
		},
	}
	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeGemini}

	privateData, err := adaptor.BuildPrivateData(c, info)

	require.NoError(t, err)
	require.NotNil(t, privateData.LocalImageTask)
	var stored dto.ImageRequest
	require.NoError(t, common.Unmarshal(privateData.LocalImageTask.Request, &stored))
	require.Equal(t, "gemini-3.1-flash-image-preview", stored.Model)
	nativeRequest, native, err := stored.ParseGeminiNativeRequest()
	require.NoError(t, err)
	require.True(t, native)
	require.Len(t, nativeRequest.Contents, 1)
	require.Len(t, nativeRequest.Contents[0].Parts, 2)
	require.NotNil(t, nativeRequest.Contents[0].Parts[0].InlineData)
	require.Equal(t, "cmVmZXJlbmNl", nativeRequest.Contents[0].Parts[0].InlineData.Data)
}

func TestParseTaskResultLocalImageResponse(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"status":"SUCCESS","result_url":"data:image/png;base64,abc"}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "data:image/png;base64,abc", info.Url)
}
