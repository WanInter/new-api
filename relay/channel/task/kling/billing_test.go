package kling

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingMatchesKlingPayload(t *testing.T) {
	request := relaycommon.TaskSubmitReq{
		Model:    "public-kling",
		Prompt:   "cat",
		Duration: 10,
		Metadata: map[string]any{"mode": "pro"},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", request)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-kling",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1-6", IsModelMapped: true},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&request, info)
	require.NoError(t, err)
	input, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)

	var canonical struct {
		Billing struct {
			Duration int    `json:"duration_seconds"`
			Quality  string `json:"quality"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	duration, err := strconv.Atoi(payload.Duration)
	require.NoError(t, err)
	assert.Equal(t, duration, canonical.Billing.Duration)
	assert.Equal(t, payload.Mode, canonical.Billing.Quality)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestKlingLimitedSchemaRejectsUnsupportedWireMode(t *testing.T) {
	adaptor := &TaskAdaptor{}
	capability := adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1"}})
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.Error(t, billingexpr.ValidateCanonicalBillingInput([]byte(`{"billing":{"duration_seconds":5,"quality":"custom"}}`), fields))
}
