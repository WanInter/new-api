package xinghe

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
		"duration_seconds": payload.Duration,
		"resolution":       payload.Resolution,
	})
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := strings.TrimSpace(upstreamModelName(info))
	if modelName == "" && info != nil {
		modelName = strings.TrimSpace(info.OriginModelName)
	}
	resolutions := []string{"720p"}
	if modelName == "xinghe-2.0" {
		resolutions = append(resolutions, "1080p")
	} else if modelName != "xinghe-mini" && modelName != "xinghe-fast" {
		return nil
	}
	durations := make([]string, 0, 12)
	for duration := 4; duration <= 15; duration++ {
		durations = append(durations, strconv.Itoa(duration))
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: "video.duration-seconds+resolution.duration-4-15.default-10.resolution-" + strings.Join(resolutions, "-") + ".default-720p.v1",
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: durations},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: resolutions},
		},
	}
}
