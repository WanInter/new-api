package image

import (
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

func TestParseTaskResultLocalImageResponse(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"status":"SUCCESS","result_url":"data:image/png;base64,abc"}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "data:image/png;base64,abc", info.Url)
}
