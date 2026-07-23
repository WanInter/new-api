package aggc

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const aggcBillingSchema = "video.duration-seconds+resolution.duration-4-15.default-4.resolution-720p-1080p.explicit-required.v1"

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	payload, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, map[string]any{
		"duration_seconds": payload.Params.Duration,
		"resolution":       payload.Params.Resolution,
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if strings.TrimSpace(aggcBillingModelName(info)) != "seedance-2.0" {
		return nil
	}
	durationCapability := taskcommon.DurationOnlyBillingCapability(aggcBillingSchema, 4, 15)
	durationCapability.Fields = append(durationCapability.Fields, channel.TaskBillingField{
		Path:       "billing.resolution",
		Type:       "string",
		Required:   true,
		EnumValues: []string{"720p", "1080p"},
	})
	return durationCapability
}

func aggcBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}
