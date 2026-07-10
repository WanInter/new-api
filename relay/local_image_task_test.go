package relay

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
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

func TestBuildLocalImageRequestBodyPreservesOpenAIExtraParameters(t *testing.T) {
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
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	body, err := buildLocalImageRequestBody(c, &openai.Adaptor{}, info, imageReq)
	require.NoError(t, err)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)

	var upstream map[string]any
	require.NoError(t, common.Unmarshal(bodyBytes, &upstream))
	assert.Equal(t, map[string]any{
		"size":          "1K",
		"n":             float64(1),
		"prompt_extend": true,
		"watermark":     false,
		"aspect_ratio":  "9:16",
	}, upstream["parameters"])
}
