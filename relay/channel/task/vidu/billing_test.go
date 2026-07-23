package vidu

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

func TestCanonicalBillingMatchesViduPayload(t *testing.T) {
	request := relaycommon.TaskSubmitReq{
		Model:      "viduq2",
		Prompt:     "cat",
		Duration:   5,
		Resolution: "720p",
		Metadata:   map[string]any{"bgm": true},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", request)
	info := &relaycommon.RelayInfo{
		OriginModelName: "viduq2",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&request, info)
	require.NoError(t, err)
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
	assert.Equal(t, payload.Duration, canonical.Billing.Duration)
	assert.Equal(t, payload.Resolution, canonical.Billing.Resolution)
	assert.Equal(t, payload.Bgm, canonical.Billing.AudioEnabled)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestViduLimitedSchemaRejectsOtherDurations(t *testing.T) {
	adaptor := &TaskAdaptor{}
	capability := adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "viduq2"}})
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.Error(t, billingexpr.ValidateCanonicalBillingInput([]byte(`{"billing":{"duration_seconds":8,"resolution":"1080p","audio_enabled":false}}`), fields))
}
