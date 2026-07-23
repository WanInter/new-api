package aggc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingMatchesAggcPayloadDuration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"seedance-2.0","prompt":"cat","duration":12,"resolution":"1080p"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "seedance-2.0"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	payload, err := adaptor.convertToRequestPayload(&req, info)
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
	assert.Equal(t, payload.Params.Duration, canonical.Billing.Duration)
	assert.Equal(t, payload.Params.Resolution, canonical.Billing.Resolution)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestAggcExplicitResolutionSchemaRejectsOmittedResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "seedance-2.0"}}
	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.Error(t, billingexpr.ValidateCanonicalBillingInput(
		[]byte(`{"billing":{"duration_seconds":4,"resolution":""}}`),
		fields,
	))
}
