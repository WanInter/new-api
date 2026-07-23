package gemini

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingMatchesGeminiVeoPayload(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:     "cat",
		Seconds:    "6",
		Resolution: "1080P",
		Metadata:   map[string]any{"generateAudio": true},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-veo",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview", IsModelMapped: true},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	wire, err := io.ReadAll(body)
	require.NoError(t, err)
	var outbound VeoRequestPayload
	require.NoError(t, common.Unmarshal(wire, &outbound))
	require.NotNil(t, outbound.Parameters)

	input, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)
	var canonical struct {
		Billing struct {
			Duration     int    `json:"duration_seconds"`
			Resolution   string `json:"resolution"`
			AudioEnabled bool   `json:"audio_enabled"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	assert.Equal(t, outbound.Parameters.DurationSeconds, canonical.Billing.Duration)
	assert.Equal(t, outbound.Parameters.Resolution, canonical.Billing.Resolution)
	assert.Nil(t, outbound.Parameters.GenerateAudio)
	assert.True(t, canonical.Billing.AudioEnabled)
	assert.NotContains(t, string(wire), "generateAudio")

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestResolveVeoParametersLeavesAudioPolicyToProvider(t *testing.T) {
	params, err := ResolveVeoParameters(&relaycommon.TaskSubmitReq{}, "veo-3.0-generate-001")
	require.NoError(t, err)
	assert.Equal(t, 8, params.DurationSeconds)
	assert.Equal(t, "720p", params.Resolution)
	assert.Nil(t, params.GenerateAudio)
}

func TestGeminiCanonicalBillingUsesAlwaysOnAudio(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "cat"})
	info := &relaycommon.RelayInfo{
		OriginModelName: "veo-3.0-generate-001",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.0-generate-001"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	var canonical struct {
		Billing map[string]any `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	assert.Equal(t, true, canonical.Billing["audio_enabled"])

	capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	require.Len(t, capability.Fields, 3)
	assert.True(t, capability.Fields[2].Required)
	assert.Equal(t, []string{"true"}, capability.Fields[2].EnumValues)
	assert.Contains(t, capability.SchemaVersion, "audio-fixed-true")
}

func TestGeminiRejectsExplicitAudioFalse(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Prompt:   "cat",
		Metadata: map[string]any{"generateAudio": false},
	}
	_, err := resolveGeminiVeoParameters(&req, "veo-3.0-generate-001")
	require.ErrorContains(t, err, "always generates audio")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", req)
	info := &relaycommon.RelayInfo{
		OriginModelName: "veo-3.0-generate-001",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.0-generate-001"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	taskErr := (&TaskAdaptor{}).ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "always generates audio")
}
