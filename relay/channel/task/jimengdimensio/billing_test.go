package jimengdimensio

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

func TestCanonicalBillingMatchesJimengDimensioPayload(t *testing.T) {
	request := relaycommon.TaskSubmitReq{
		Model:      "public-jimeng",
		Prompt:     "cat",
		Duration:   15,
		Resolution: "1080p",
		Metadata:   map[string]any{},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", request)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-jimeng",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip", IsModelMapped: true},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&request, info)
	require.NoError(t, err)
	input, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)

	var canonical struct {
		Billing struct {
			Duration   int    `json:"duration_seconds"`
			Resolution string `json:"resolution"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	assert.Equal(t, payload.Duration, canonical.Billing.Duration)
	assert.Equal(t, payload.Resolution, canonical.Billing.Resolution)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestJimengDimensioLimitedSchemaRejectsUnsupportedResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	capability := adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"}})
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	err := billingexpr.ValidateCanonicalBillingInput([]byte(`{"billing":{"duration_seconds":4,"resolution":"4k"}}`), fields)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported value")
}
