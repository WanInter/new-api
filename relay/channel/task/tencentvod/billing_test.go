package tencentvod

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingMatchesTencentVODPayload(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "public-kling",
		Prompt:     "cat",
		Seconds:    "11",
		Resolution: "1080p",
		Metadata:   map[string]any{"audio_generation": true, "off_peak": true},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-kling",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "kling-vod-2.1", IsModelMapped: true},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	payload, err := convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model: "public-kling", Prompt: "cat", Seconds: "11", Resolution: "1080p", Metadata: map[string]any{"audio_generation": true, "off_peak": true},
	}, info, 1)
	require.NoError(t, err)
	input, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)

	var canonical struct {
		Billing struct {
			Duration     int    `json:"duration_seconds"`
			Resolution   string `json:"resolution"`
			AudioEnabled bool   `json:"audio_enabled"`
			OffPeak      bool   `json:"off_peak"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	require.NotNil(t, payload.OutputConfig.Duration)
	assert.Equal(t, int(*payload.OutputConfig.Duration), canonical.Billing.Duration)
	assert.Equal(t, "1080p", canonical.Billing.Resolution)
	assert.Equal(t, payload.OutputConfig.AudioGeneration == "Enabled", canonical.Billing.AudioEnabled)
	assert.Equal(t, payload.OutputConfig.OffPeak == "Enabled", canonical.Billing.OffPeak)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestTencentVODCanonicalBillingUsesAudioDefaultAndPreservesOffPeakOmission(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	req := relaycommon.TaskSubmitReq{Model: "kling-vod-2.1", Prompt: "cat"}
	c.Set("task_request", req)
	info := &relaycommon.RelayInfo{
		OriginModelName: "kling-vod-2.1",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "kling-vod-2.1"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	payload, err := convertToRequestPayload(&req, info, 1)
	require.NoError(t, err)
	assert.Equal(t, "Disabled", payload.OutputConfig.AudioGeneration)
	assert.Empty(t, payload.OutputConfig.OffPeak)

	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	var canonical struct {
		Billing map[string]any `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	assert.Equal(t, false, canonical.Billing["audio_enabled"])
	assert.NotContains(t, canonical.Billing, "off_peak")

	capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	require.Len(t, capability.Fields, 4)
	assert.True(t, capability.Fields[2].Required)
	assert.False(t, capability.Fields[3].Required)
}

func TestTencentVODCanonicalBillingPreservesExplicitOffPeakFalse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	req := relaycommon.TaskSubmitReq{
		Model:    "kling-vod-2.1",
		Prompt:   "cat",
		Metadata: map[string]any{"off_peak": false},
	}
	c.Set("task_request", req)
	info := &relaycommon.RelayInfo{
		OriginModelName: "kling-vod-2.1",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "kling-vod-2.1"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	payload, err := convertToRequestPayload(&req, info, 1)
	require.NoError(t, err)
	assert.Equal(t, "Disabled", payload.OutputConfig.OffPeak)
	wire, err := common.Marshal(payload)
	require.NoError(t, err)
	var outbound requestPayload
	require.NoError(t, common.Unmarshal(wire, &outbound))
	assert.Equal(t, "Disabled", outbound.OutputConfig.OffPeak)

	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	var canonical struct {
		Billing map[string]any `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	require.Contains(t, canonical.Billing, "off_peak")
	assert.Equal(t, false, canonical.Billing["off_peak"])
}
