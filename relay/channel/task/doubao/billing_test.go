package doubao

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestByteforBillingInputMatchesOutgoingDuration(t *testing.T) {
	testCases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want int
	}{
		{name: "duration", req: relaycommon.TaskSubmitReq{Duration: 8, Seconds: "12"}, want: 8},
		{name: "seconds alias", req: relaycommon.TaskSubmitReq{Seconds: "12 seconds"}, want: 12},
		{name: "missing uses wire default", req: relaycommon.TaskSubmitReq{}, want: 15},
		{name: "invalid alias uses wire default", req: relaycommon.TaskSubmitReq{Seconds: "invalid"}, want: 15},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
			c.Request.Header.Set("X-Request", "current")
			c.Set("task_request", testCase.req)
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-seedance",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "bytefor-2.0-real-priority",
				},
				RequestHeaders: map[string]string{"X-Frozen": "frozen"},
			}

			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			var canonical struct {
				Billing map[string]int `json:"billing"`
			}
			require.NoError(t, common.Unmarshal(input.Body, &canonical))
			assert.Equal(t, testCase.want, canonical.Billing["duration_seconds"])
			assert.Equal(t, "current", input.Headers["X-Request"])
			assert.Equal(t, "frozen", input.Headers["X-Frozen"])

			payload := convertToByteforRequestPayload(&testCase.req, info)
			assert.Equal(t, strconv.Itoa(testCase.want)+"s", payload.Duration)
		})
	}
}

func TestByteforBillingCapabilityDeclaresDefaultSemantics(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "bytefor-2.0-real-priority",
		},
	}

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	assert.Equal(t, byteforDurationBillingSchema, capability.SchemaVersion)
	require.Len(t, capability.Fields, 1)
	assert.Equal(t, "billing.duration_seconds", capability.Fields[0].Path)
	assert.True(t, capability.Fields[0].Required)
	assert.Equal(t, []string{"4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}, capability.Fields[0].EnumValues)
	canonicalFields := []billingexpr.CanonicalBillingField{
		{
			Path:       capability.Fields[0].Path,
			Type:       capability.Fields[0].Type,
			Required:   capability.Fields[0].Required,
			EnumValues: capability.Fields[0].EnumValues,
		},
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingSchema(canonicalFields))
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(
		[]byte(`{"billing":{"duration_seconds":15}}`),
		canonicalFields,
	))

	info.ChannelMeta.UpstreamModelName = "unknown-model"
	assert.Nil(t, adaptor.GetTaskBillingCapability(info))
}

func TestDoubaoVideoInputBillingMatchesOutgoingContent(t *testing.T) {
	testCases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want bool
	}{
		{name: "text only", req: relaycommon.TaskSubmitReq{Prompt: "animate"}},
		{name: "video alias", req: relaycommon.TaskSubmitReq{Prompt: "animate", Videos: []string{"https://example.com/video.mp4"}}, want: true},
		{name: "content video", req: relaycommon.TaskSubmitReq{Prompt: "animate", Content: []relaycommon.TaskContentItem{{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: "https://example.com/content.mp4"}}}}, want: true},
		{name: "metadata content video", req: relaycommon.TaskSubmitReq{Prompt: "animate", Metadata: map[string]any{"content": []any{map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://example.com/metadata.mp4"}}}}}, want: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
			c.Set("task_request", testCase.req)
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-seedance",
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2-0-260128"},
			}
			adaptor := &TaskAdaptor{}

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			var canonical struct {
				Billing struct {
					VideoInput bool `json:"video_input"`
				} `json:"billing"`
			}
			require.NoError(t, common.Unmarshal(input.Body, &canonical))
			assert.Equal(t, testCase.want, canonical.Billing.VideoInput)

			payload, err := adaptor.convertToRequestPayload(&testCase.req)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, doubaoPayloadHasVideo(payload))
		})
	}
}

func TestDoubaoVideoInputBillingCapabilityAndExpressionMatrix(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2-0-fast-260128"}}
	capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	assert.Equal(t, doubaoVideoInputBillingSchema, capability.SchemaVersion)
	require.Len(t, capability.Fields, 1)
	assert.Equal(t, "billing.video_input", capability.Fields[0].Path)
	assert.Equal(t, "boolean", capability.Fields[0].Type)
	assert.Equal(t, []string{"false", "true"}, capability.Fields[0].EnumValues)

	fields := []billingexpr.CanonicalBillingField{{
		Path:       capability.Fields[0].Path,
		Type:       capability.Fields[0].Type,
		Required:   capability.Fields[0].Required,
		EnumValues: capability.Fields[0].EnumValues,
	}}
	require.NoError(t, billingexpr.ValidateCanonicalBillingSchema(fields))
	require.NoError(t, billingexpr.ValidateCanonicalBillingExpressionMatrix(
		`param("billing.video_input") == true ? tier("video", 2) : tier("text", 1)`,
		fields,
	))
}

func TestLegacyDoubaoBillingCapabilityRejectsVideoInput(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-1-0-pro-250528"}}
	capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	assert.Equal(t, doubaoNoVideoInputBillingSchema, capability.SchemaVersion)
	require.Len(t, capability.Fields, 1)
	assert.Equal(t, []string{"false"}, capability.Fields[0].EnumValues)

	fields := []billingexpr.CanonicalBillingField{{
		Path:       capability.Fields[0].Path,
		Type:       capability.Fields[0].Type,
		Required:   capability.Fields[0].Required,
		EnumValues: capability.Fields[0].EnumValues,
	}}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput([]byte(`{"billing":{"video_input":false}}`), fields))
	assert.Error(t, billingexpr.ValidateCanonicalBillingInput([]byte(`{"billing":{"video_input":true}}`), fields))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "animate", Videos: []string{"https://example.com/video.mp4"}})
	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func doubaoPayloadHasVideo(payload *requestPayload) bool {
	if payload == nil {
		return false
	}
	for _, item := range payload.Content {
		if item.Type == "video_url" && item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			return true
		}
	}
	return false
}
