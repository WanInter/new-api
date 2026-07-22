package common

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTaskSubmitVideoOutputCanonicalizesAliases(t *testing.T) {
	request := &TaskSubmitReq{
		Size:             "1920X1080",
		Ratio:            "32:18",
		AspectRatioAlias: "16:9",
		Resolution:       "1080P",
	}

	spec, err := NormalizeTaskSubmitVideoOutput(request)

	require.NoError(t, err)
	assert.Equal(t, "1920x1080", spec.Size)
	assert.Equal(t, "16:9", spec.AspectRatio)
	assert.Equal(t, "1080p", spec.Resolution)
	assert.Equal(t, "16:9", request.AspectRatio)
	assert.Equal(t, "1080p", request.Resolution)
	assert.Empty(t, request.Ratio)
	assert.Empty(t, request.AspectRatioAlias)
	encoded, err := basecommon.Marshal(request)
	require.NoError(t, err)
	var marshaled map[string]any
	require.NoError(t, basecommon.Unmarshal(encoded, &marshaled))
	assert.Equal(t, "16:9", marshaled["aspect_ratio"])
	assert.NotContains(t, marshaled, "ratio")
}

func TestNormalizeTaskSubmitVideoOutputRejectsConflictingOutputFields(t *testing.T) {
	testCases := []struct {
		name    string
		request TaskSubmitReq
		message string
	}{
		{
			name:    "ratio aliases disagree",
			request: TaskSubmitReq{AspectRatio: "16:9", Ratio: "9:16"},
			message: "conflicts with aspect_ratio",
		},
		{
			name:    "size disagrees with ratio",
			request: TaskSubmitReq{Size: "960x540", AspectRatio: "9:16"},
			message: "conflicts with aspect_ratio",
		},
		{
			name:    "adaptive cannot include concrete size",
			request: TaskSubmitReq{Size: "960x540", AspectRatio: "adaptive"},
			message: "conflicts with adaptive",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NormalizeTaskSubmitVideoOutput(&testCase.request)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.message)
		})
	}
}

func TestNormalizeTaskSubmitVideoOutputPrefersTopLevelFieldsOverMetadata(t *testing.T) {
	request := &TaskSubmitReq{
		AspectRatio: "9:16",
		Resolution:  "720p",
		Metadata: map[string]interface{}{
			"aspect_ratio": "16:9",
			"ratio":        "16:9",
			"aspectRatio":  "16:9",
			"resolution":   "1080p",
		},
	}

	spec, err := NormalizeTaskSubmitVideoOutput(request)

	require.NoError(t, err)
	assert.Equal(t, "9:16", spec.AspectRatio)
	assert.Equal(t, "720p", spec.Resolution)
	assert.Equal(t, "9:16", request.Metadata["aspect_ratio"])
	assert.Equal(t, "720p", request.Metadata["resolution"])
	assert.NotContains(t, request.Metadata, "ratio")
	assert.NotContains(t, request.Metadata, "aspectRatio")
}

func TestNormalizeTaskSubmitVideoOutputTopLevelFieldsIgnoreInvalidMetadataCounterparts(t *testing.T) {
	request := &TaskSubmitReq{
		Size:        "1280x720",
		AspectRatio: "16:9",
		Resolution:  "720p",
		Metadata: map[string]interface{}{
			"size":         720,
			"ratio":        false,
			"aspectRatio":  []string{"9:16"},
			"aspect_ratio": map[string]bool{"invalid": true},
			"resolution":   1080,
		},
	}

	spec, err := NormalizeTaskSubmitVideoOutput(request)

	require.NoError(t, err)
	assert.Equal(t, "1280x720", spec.Size)
	assert.Equal(t, "16:9", spec.AspectRatio)
	assert.Equal(t, "720p", spec.Resolution)
	assert.Equal(t, "1280x720", request.Metadata["size"])
	assert.Equal(t, "16:9", request.Metadata["aspect_ratio"])
	assert.Equal(t, "720p", request.Metadata["resolution"])
	assert.NotContains(t, request.Metadata, "ratio")
	assert.NotContains(t, request.Metadata, "aspectRatio")
}

func TestNormalizeTaskSubmitVideoOutputTopLevelPixelSizeOverridesMetadataAspectRatio(t *testing.T) {
	request := &TaskSubmitReq{
		Size: "1280x720",
		Metadata: map[string]interface{}{
			"ratio":        "9:16",
			"aspectRatio":  "9:16",
			"aspect_ratio": "9:16",
		},
	}

	spec, err := NormalizeTaskSubmitVideoOutput(request)

	require.NoError(t, err)
	assert.Equal(t, "16:9", spec.AspectRatio)
	assert.Equal(t, "16:9", request.AspectRatio)
	assert.Equal(t, "16:9", request.Metadata["aspect_ratio"])
	assert.NotContains(t, request.Metadata, "ratio")
	assert.NotContains(t, request.Metadata, "aspectRatio")
}

func TestNormalizeTaskSubmitVideoOutputTopLevelRatioDoesNotUseMetadataSize(t *testing.T) {
	request := &TaskSubmitReq{
		Ratio: "9:16",
		Metadata: map[string]interface{}{
			"size": "1280x720",
		},
	}

	spec, err := NormalizeTaskSubmitVideoOutput(request)

	require.NoError(t, err)
	assert.Empty(t, spec.Size)
	assert.Equal(t, "9:16", spec.AspectRatio)
	assert.Empty(t, request.Ratio)
	assert.NotContains(t, request.Metadata, "size")
	assert.Equal(t, "9:16", request.Metadata["aspect_ratio"])
}

func TestNormalizeTaskSubmitVideoOutputLegacySizeUsesMetadataAspectRatio(t *testing.T) {
	request := &TaskSubmitReq{
		Size: "720p",
		Metadata: map[string]interface{}{
			"size":         "1080p",
			"aspect_ratio": "9:16",
		},
	}

	spec, err := NormalizeTaskSubmitVideoOutput(request)

	require.NoError(t, err)
	assert.Equal(t, "720p", spec.Size)
	assert.Equal(t, "9:16", spec.AspectRatio)
	assert.Equal(t, "720p", request.Metadata["size"])
	assert.Equal(t, "9:16", request.Metadata["aspect_ratio"])
}

func TestNormalizeTaskSubmitVideoOutputRejectsConflictingMetadataGeometry(t *testing.T) {
	request := &TaskSubmitReq{
		Metadata: map[string]interface{}{
			"size":         "1280x720",
			"aspect_ratio": "9:16",
		},
	}

	_, err := NormalizeTaskSubmitVideoOutput(request)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts with aspect_ratio")
}

func TestTaskSubmitReqUnmarshalCapturesAspectRatioCamelAlias(t *testing.T) {
	var request TaskSubmitReq
	require.NoError(t, basecommon.Unmarshal([]byte(`{"aspectRatio":"32:18","resolution":"1080P"}`), &request))

	spec, err := NormalizeTaskSubmitVideoOutput(&request)

	require.NoError(t, err)
	assert.Equal(t, "16:9", spec.AspectRatio)
	assert.Equal(t, "1080p", spec.Resolution)
}

func TestValidateBasicTaskRequestNormalizesMultipartOutputAliases(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("model", "video-model"))
	require.NoError(t, writer.WriteField("size", "1280x720"))
	require.NoError(t, writer.WriteField("ratio", "16:9"))
	require.NoError(t, writer.WriteField("resolution", "720P"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { basecommon.CleanupBodyStorage(c) })
	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}

	require.Nil(t, ValidateBasicTaskRequest(c, info, "generate"))
	request, err := GetTaskRequest(c)

	require.NoError(t, err)
	require.NotNil(t, request.VideoOutput)
	assert.Equal(t, "1280x720", request.Size)
	assert.Equal(t, "16:9", request.AspectRatio)
	assert.Equal(t, "720p", request.Resolution)
}
