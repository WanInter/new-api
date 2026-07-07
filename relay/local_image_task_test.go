package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
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
