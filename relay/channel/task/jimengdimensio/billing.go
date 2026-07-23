package jimengdimensio

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const jimengDimensioBillingSchema = "video.duration-seconds+resolution.duration-4-15.default-4.resolution-720p-1080p.default-720p.limited.v1"

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
		"duration_seconds": payload.Duration,
		"resolution":       payload.Resolution,
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := jimengDimensioBillingModelName(info)
	if !jimengDimensioBillingModelSupported(modelName) {
		return nil
	}
	durations := make([]string, 0, 12)
	for duration := 4; duration <= 15; duration++ {
		durations = append(durations, strconv.Itoa(duration))
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: jimengDimensioBillingSchema,
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: durations},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: []string{"720p", "1080p"}},
		},
	}
}

func jimengDimensioBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}

func jimengDimensioBillingModelSupported(modelName string) bool {
	for _, candidate := range ModelList {
		if modelName == candidate {
			return true
		}
	}
	return false
}
