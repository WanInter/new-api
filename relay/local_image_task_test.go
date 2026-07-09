package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
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
