package jimeng

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

func TestCanonicalBillingMatchesJimengFramePayload(t *testing.T) {
	request := relaycommon.TaskSubmitReq{
		Model:    "public-jimeng",
		Prompt:   "cat",
		Duration: 5,
		Metadata: map[string]any{"frames": 241},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", request)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-jimeng",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20", IsModelMapped: true},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&request, info)
	require.NoError(t, err)
	input, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)

	var canonical struct {
		Billing struct {
			Duration float64 `json:"duration_seconds"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	assert.Equal(t, float64(payload.Frames-1)/24, canonical.Billing.Duration)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := []billingexpr.CanonicalBillingField{{Path: capability.Fields[0].Path, Type: capability.Fields[0].Type, Required: true, EnumValues: capability.Fields[0].EnumValues}}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}
