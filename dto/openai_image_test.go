package dto

import (
	"encoding/json"
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

func TestImageRequestNativeGeminiRoundTrip(t *testing.T) {
	input := []byte(`{
		"model":"nano-banana-2",
		"contents":[{"role":"user","parts":[
			{"inlineData":{"mimeType":"image/jpeg","data":"cmVmZXJlbmNl"}},
			{"text":"change the background"}
		]}],
		"generationConfig":{"candidateCount":1,"responseModalities":["IMAGE"]},
		"customProviderField":{"keep":true}
	}`)
	var request ImageRequest
	require.NoError(t, common.Unmarshal(input, &request))
	require.NotNil(t, request.GeminiNative)
	assert.NotContains(t, request.Extra, "contents")
	assert.NotContains(t, request.Extra, "generationConfig")

	output, err := request.MarshalJSONWithExtra()
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(output, &raw))
	assert.Contains(t, raw, "contents")
	assert.Contains(t, raw, "generationConfig")
	assert.Contains(t, raw, "customProviderField")

	var roundTripped ImageRequest
	require.NoError(t, common.Unmarshal(output, &roundTripped))
	native, ok, err := roundTripped.ParseGeminiNativeRequest()
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, native.Contents, 1)
	require.Len(t, native.Contents[0].Parts, 2)
	require.NotNil(t, native.Contents[0].Parts[0].InlineData)
	assert.Equal(t, "image/jpeg", native.Contents[0].Parts[0].InlineData.MimeType)
	assert.Equal(t, "cmVmZXJlbmNl", native.Contents[0].Parts[0].InlineData.Data)
	assert.Equal(t, []string{"IMAGE"}, native.GenerationConfig.ResponseModalities)
}

func TestImageRequestHasImageReferencesUsesJSONStructure(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "absent", body: `{}`, want: false},
		{name: "null", body: `{"images":null}`, want: false},
		{name: "empty string", body: `{"images":"  "}`, want: false},
		{name: "formatted empty array", body: `{"images":[ ]}`, want: false},
		{name: "formatted empty object", body: `{"image":{ }}`, want: false},
		{name: "array reference", body: `{"images":[{"image_url":"https://example.com/reference.png"}]}`, want: true},
		{name: "singular reference", body: `{"image":{"url":"https://example.com/reference.png"}}`, want: true},
		{name: "nonempty invalid collection", body: `{"images":[null]}`, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request ImageRequest
			require.NoError(t, common.Unmarshal([]byte(test.body), &request))

			assert.Equal(t, test.want, request.HasImageReferences())
		})
	}
}
