package jimeng

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const jimengBillingSchema = "video.duration-seconds.frames-121-241.duration-5-10.default-5.limited.v1"

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	payload, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	duration := float64(payload.Frames-1) / 24
	return taskcommon.BuildCanonicalBillingInput(c, info, map[string]any{
		"duration_seconds": duration,
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if strings.TrimSpace(jimengBillingModelName(info)) == "" {
		return nil
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: jimengBillingSchema,
		Fields: []channel.TaskBillingField{{
			Path:       "billing.duration_seconds",
			Type:       "number",
			Required:   true,
			EnumValues: []string{"5", "10"},
		}},
	}
}

func jimengBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}
