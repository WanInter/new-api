package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageRequestMarshalJSONWithExtraIsOptIn(t *testing.T) {
	var request ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"nano-banana-pro",
		"prompt":"cat",
		"parameters":{
			"size":"1K",
			"aspect_ratio":"9:16"
		}
	}`), &request))

	defaultBody, err := common.Marshal(request)
	require.NoError(t, err)
	var defaultPayload map[string]any
	require.NoError(t, common.Unmarshal(defaultBody, &defaultPayload))
	assert.NotContains(t, defaultPayload, "parameters")

	preservedBody, err := request.MarshalJSONWithExtra()
	require.NoError(t, err)
	var preservedPayload map[string]any
	require.NoError(t, common.Unmarshal(preservedBody, &preservedPayload))
	assert.Equal(t, map[string]any{
		"size":         "1K",
		"aspect_ratio": "9:16",
	}, preservedPayload["parameters"])
}
