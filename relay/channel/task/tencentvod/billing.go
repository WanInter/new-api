package tencentvod

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const tencentVODBillingSchema = "video.duration-seconds+resolution+audio+off-peak.duration-3-15.default-5.resolution-720p-1080p.default-720p.audio-default-false.required.off-peak-optional.v2"

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	payload, err := convertToRequestPayload(&req, info, 1)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	duration := 0
	if payload.OutputConfig.Duration != nil {
		duration = int(*payload.OutputConfig.Duration)
	}
	billing := map[string]any{
		"duration_seconds": duration,
		"resolution":       strings.ToLower(payload.OutputConfig.Resolution),
		"audio_enabled":    payload.OutputConfig.AudioGeneration == "Enabled",
	}
	if payload.OutputConfig.OffPeak != "" {
		billing["off_peak"] = payload.OutputConfig.OffPeak == "Enabled"
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := tencentVODBillingModelName(info)
	if _, ok := modelVersionAliases[strings.ToLower(modelName)]; !ok {
		return nil
	}
	durations := make([]string, 0, 13)
	for duration := 3; duration <= 15; duration++ {
		durations = append(durations, strconv.Itoa(duration))
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: tencentVODBillingSchema,
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: durations},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: []string{"720p", "1080p"}},
			{Path: "billing.audio_enabled", Type: "boolean", Required: true, EnumValues: []string{"false", "true"}},
			{Path: "billing.off_peak", Type: "boolean", Required: false, EnumValues: []string{"false", "true"}},
		},
	}
}

func tencentVODBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}
