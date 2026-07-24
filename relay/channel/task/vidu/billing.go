package vidu

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const viduBillingSchema = "video.duration-seconds+resolution+audio.duration-5.default-5.resolution-720p-1080p.default-1080p.audio-false-true.default-false.limited.v1"

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
	return taskcommon.BuildCanonicalBillingInput(c, info, map[string]any{
		"duration_seconds": payload.Duration,
		"resolution":       payload.Resolution,
		"audio_enabled":    payload.Bgm,
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if !viduBillingModelSupported(viduBillingModelName(info)) {
		return nil
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: viduBillingSchema,
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: []string{"5"}},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: []string{"720p", "1080p"}},
			{Path: "billing.audio_enabled", Type: "boolean", Required: true, EnumValues: []string{"false", "true"}},
		},
	}
}

func viduBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}

func viduBillingModelSupported(modelName string) bool {
	for _, candidate := range (&TaskAdaptor{}).GetModelList() {
		if modelName == candidate {
			return true
		}
	}
	return false
}
