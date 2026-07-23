package doubao

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	byteforDurationBillingSchema    = "video.duration-seconds.integer.4-15.default-15.v1"
	doubaoVideoInputBillingSchema   = "video.video-input.boolean.false-true.v1"
	doubaoNoVideoInputBillingSchema = "video.video-input.boolean.false-only.v1"
)

// BuildBillingInput uses the final Bytefor duration resolver or the native
// Doubao media resolver that also drives the video-input discount.
func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	requestInput := billingexpr.RequestInput{Headers: taskcommon.CanonicalBillingHeaders(c, info)}
	modelName := relayInfoModelName(info)
	if !isByteforModel(modelName) && !isNativeDoubaoModel(modelName) {
		return requestInput, nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return requestInput, err
	}
	if isByteforModel(modelName) {
		return taskcommon.BuildCanonicalBillingInput(c, info, map[string]any{
			"duration_seconds": byteforBillingSeconds(req),
		})
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, map[string]any{
		"video_input": hasDoubaoVideoInput(&req),
	})
}

// GetTaskBillingCapability exposes Bytefor duration billing and a boolean
// video-input contract for every native Doubao model. Models without a
// documented discount only allow false in schema-pinned mode.
func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if info == nil {
		return nil
	}
	modelName := strings.TrimSpace(relayInfoModelName(info))
	if isByteforModel(modelName) {
		return taskcommon.DurationOnlyBillingCapability(byteforDurationBillingSchema, 4, 15)
	}
	if !isNativeDoubaoModel(modelName) {
		return nil
	}
	schemaVersion := doubaoNoVideoInputBillingSchema
	enumValues := []string{"false"}
	if _, supportsVideoDiscount := GetVideoInputRatio(modelName); supportsVideoDiscount {
		schemaVersion = doubaoVideoInputBillingSchema
		enumValues = append(enumValues, "true")
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: schemaVersion,
		Fields: []channel.TaskBillingField{
			{
				Path:       "billing.video_input",
				Type:       "boolean",
				Required:   true,
				EnumValues: enumValues,
			},
		},
	}
}

func isNativeDoubaoModel(modelName string) bool {
	for _, supported := range doubaoModelList {
		if modelName == supported {
			return true
		}
	}
	return false
}
