package xinghe

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

func TestCanonicalBillingMatchesXinghePayload(t *testing.T) {
	request := relaycommon.TaskSubmitReq{
		Model:      "public-xinghe",
		Prompt:     "cat",
		Duration:   12,
		Resolution: "1080p",
		Images:     []string{"https://example.com/cat.png"},
		Metadata:   map[string]any{},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", request)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-xinghe",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0", IsModelMapped: true},
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

func TestXingheSchemaReflectsModelResolutionSupport(t *testing.T) {
	adaptor := &TaskAdaptor{}
	mini := adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-mini"}})
	v2 := adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0"}})
	require.NotNil(t, mini)
	require.NotNil(t, v2)
	assert.Equal(t, []string{"720p"}, mini.Fields[1].EnumValues)
	assert.Equal(t, []string{"720p", "1080p"}, v2.Fields[1].EnumValues)
}
