package kling

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const klingBillingSchema = "video.duration-seconds+quality.duration-5-10.default-5.quality-std-pro.default-std.limited.v1"

func (a *TaskAdaptor) SupportsTaskBillingProfileFallback() bool {
	return true
}

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	payload, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	duration, _ := taskcommon.PositiveIntegerSecondsFromWireValue(payload.Duration)
	return taskcommon.BuildCanonicalBillingInput(c, info, map[string]any{
		"duration_seconds": duration,
		"quality":          strings.ToLower(strings.TrimSpace(payload.Mode)),
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := klingBillingModelName(info)
	if !klingBillingModelSupported(modelName) {
		return nil
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: klingBillingSchema,
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: []string{"5", "10"}},
			{Path: "billing.quality", Type: "string", Required: true, EnumValues: []string{"std", "pro"}},
		},
	}
}

func klingBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}

func klingBillingModelSupported(modelName string) bool {
	for _, candidate := range (&TaskAdaptor{}).GetModelList() {
		if modelName == candidate {
			return true
		}
	}
	return false
}
