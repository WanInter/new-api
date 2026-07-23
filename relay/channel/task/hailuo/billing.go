package hailuo

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

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
		"duration_seconds": *payload.Duration,
		"resolution":       strings.ToLower(payload.Resolution),
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := hailuoCapabilityModelName(info)
	if !contains(ModelList, modelName) {
		return nil
	}
	config := GetModelConfig(modelName)
	durations := make([]string, 0, len(config.SupportedDurations))
	for _, duration := range config.SupportedDurations {
		durations = append(durations, strconv.Itoa(duration))
	}
	resolutions := make([]string, 0, len(config.SupportedResolutions))
	for _, resolution := range config.SupportedResolutions {
		resolutions = append(resolutions, strings.ToLower(resolution))
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: hailuoBillingSchema(config, durations, resolutions),
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: durations},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: resolutions},
		},
	}
}

func hailuoCapabilityModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}

func hailuoBillingSchema(config ModelConfig, durations, resolutions []string) string {
	return "video.duration-seconds+resolution.duration-" + strings.Join(durations, "-") +
		".default-" + strconv.Itoa(DefaultDuration) +
		".resolution-" + strings.Join(resolutions, "-") +
		".default-" + strings.ToLower(config.DefaultResolution) + ".v1"
}
