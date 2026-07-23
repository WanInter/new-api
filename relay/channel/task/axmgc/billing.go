package axmgc

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	axmgcBillingSchema       = "video.duration-seconds+resolution.duration-15.model-fixed.resolution-720p.model-fixed.v2"
	axmgcEffectiveDuration   = 15
	axmgcEffectiveResolution = "720p"
)

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	payload, ok := c.Get(jsonRequestContextKey)
	if !ok {
		return billingexpr.RequestInput{}, nil
	}
	request, ok := payload.(axmgcJSONRequest)
	if !ok {
		return billingexpr.RequestInput{}, nil
	}
	request.Model = upstreamModelName(info, request.Model)
	if request.Model != Seedance720p933Model {
		return billingexpr.RequestInput{}, nil
	}
	billing := map[string]any{
		"duration_seconds": axmgcEffectiveDuration,
		"resolution":       axmgcEffectiveResolution,
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if axmgcBillingModelName(info) != Seedance720p933Model {
		return nil
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: axmgcBillingSchema,
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: []string{"15"}},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: []string{axmgcEffectiveResolution}},
		},
	}
}

func axmgcBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}
