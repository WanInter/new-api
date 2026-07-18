package sora

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyNormalizesTokenStackPayload(t *testing.T) {
	bodyJSON := `{
		"duration":15,
		"images":["image-1","image-2","image-3","image-4"],
		"model":"sd-bak-1",
		"prompt":"animate the references",
		"ratio":"9:16",
		"resolution":"720p",
		"size":"720x1280",
		"unsupported":"drop-me"
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://www.tokenstack.cc"))
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, map[string]any{
		"images":  []any{"image-1", "image-2", "image-3", "image-4"},
		"model":   "seedance-2-0-15s-slow",
		"prompt":  "animate the references",
		"seconds": "15",
		"size":    "720x1280",
	}, got)
}

func TestBuildRequestBodyDerivesTokenStackSizeFromLegacyFields(t *testing.T) {
	tests := []struct {
		name      string
		fields    string
		wantSize  string
		wantField bool
	}{
		{name: "portrait ratio", fields: `"ratio":"9:16","resolution":"720p"`, wantSize: "720x1280", wantField: true},
		{name: "landscape aspect ratio", fields: `"aspect_ratio":"16:9","resolution":"720p"`, wantSize: "1280x720", wantField: true},
		{name: "unsupported resolution", fields: `"ratio":"9:16","resolution":"1080p"`, wantField: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyJSON := `{"model":"sd-bak-1","prompt":"animate",` + tt.fields + `}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://tokenstack.cc"))
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, common.Unmarshal(data, &got))
			if tt.wantField {
				assert.Equal(t, tt.wantSize, got["size"])
			} else {
				assert.NotContains(t, got, "size")
			}
			assert.NotContains(t, got, "ratio")
			assert.NotContains(t, got, "aspect_ratio")
			assert.NotContains(t, got, "resolution")
		})
	}
}

func TestBuildRequestBodyDoesNotApplyTokenStackRulesToOtherSoraChannels(t *testing.T) {
	bodyJSON := `{
		"model":"sd-bak-1",
		"prompt":"animate",
		"duration":15,
		"ratio":"9:16",
		"resolution":"720p",
		"size":"720x1280"
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(bodyJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, tokenStackRelayInfo("https://video.example.com"))
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "9:16", got["ratio"])
	assert.Equal(t, "720p", got["resolution"])
	assert.Equal(t, float64(15), got["duration"])
	assert.Equal(t, "15", got["seconds"])
}

func TestValidateRequestAndSetActionRequiresJSONForTokenStack(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("prompt=animate"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, tokenStackRelayInfo("https://www.tokenstack.cc"))

	require.NotNil(t, taskErr)
	assert.Equal(t, "unsupported_content_type", taskErr.Code)
}

func tokenStackRelayInfo(baseURL string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "sd-bak-1",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    baseURL,
			UpstreamModelName: "seedance-2-0-15s-slow",
			IsModelMapped:     true,
		},
	}
}
