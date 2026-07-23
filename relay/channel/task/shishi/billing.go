package shishi

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// BuildBillingInput mirrors Shishi's JSON and multipart duration alias
// selection. Resolution remains undeclared until the provider publishes a
// stable enumerable contract for the supported Veo models.
func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	requestInput := billingexpr.RequestInput{Headers: taskcommon.CanonicalBillingHeaders(c, info)}
	if !isVeoOmniFlashModel(info) {
		return requestInput, nil
	}

	billing, err := canonicalShishiBillingValues(c, info)
	if err != nil {
		return requestInput, err
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if !isVeoOmniFlashModel(info) {
		return nil
	}
	return taskcommon.DurationSizeBillingCapability(taskcommon.ExplicitDurationSizeBillingSchema4To15, 4, 15)
}

func canonicalShishiBillingValues(c *gin.Context, info *relaycommon.RelayInfo) (map[string]any, error) {
	billing := make(map[string]any)
	if c == nil || c.Request == nil {
		return billing, nil
	}
	if isJSONRequest(c) {
		payload, err := requestMap(c)
		if err != nil {
			return nil, err
		}
		req, err := shishiNormalizedTaskRequest(c, payload, info)
		if err != nil {
			return nil, err
		}
		mapSecondsToDuration(payload)
		if err := applyCanonicalVideoOutput(payload, req); err != nil {
			return nil, err
		}
		addCanonicalShishiDuration(billing, payload["duration"], hasShishiMapKey(payload, "duration"))
		addCanonicalShishiSize(billing, payload["size"], hasShishiMapKey(payload, "size"))
		return billing, nil
	}
	if !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return billing, nil
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		if err := relaycommon.ApplyNormalizedTaskMultipartVideoOutput(form.Value, req, relaycommon.MultipartVideoOutputOptions{}); err != nil {
			return nil, err
		}
	}
	values := form.Value["duration"]
	if len(values) == 0 {
		values = form.Value["seconds"]
	}
	if len(values) == 1 {
		addCanonicalShishiDuration(billing, values[0], true)
	}
	sizes, sizeSelected := form.Value["size"]
	if !sizeSelected || len(sizes) == 0 {
		billing["size"] = taskcommon.DefaultCanonicalVideoSize
	} else if len(sizes) == 1 {
		addCanonicalShishiSize(billing, sizes[0], true)
	}
	return billing, nil
}

func addCanonicalShishiDuration(billing map[string]any, value any, selected bool) {
	if !selected {
		return
	}
	if seconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(value); ok {
		billing["duration_seconds"] = seconds
	}
}

func addCanonicalShishiSize(billing map[string]any, value any, selected bool) {
	if !selected {
		billing["size"] = taskcommon.DefaultCanonicalVideoSize
		return
	}
	size, ok := value.(string)
	if !ok || strings.TrimSpace(size) == "" {
		return
	}
	billing["size"] = strings.ToLower(strings.TrimSpace(size))
}

func hasShishiMapKey(values map[string]any, key string) bool {
	_, ok := values[key]
	return ok
}
