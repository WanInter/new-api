package seventhframe

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func (a *TaskAdaptor) SupportsTaskBillingProfileFallback() bool {
	return true
}

// BuildBillingInput reuses seventhFrameDuration, the resolver used by the
// generation payload for JSON, multipart, and URL-encoded requests.
func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	requestInput := billingexpr.RequestInput{Headers: taskcommon.CanonicalBillingHeaders(c, info)}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return requestInput, err
	}
	duration, err := seventhFrameDuration(c, req)
	if err != nil {
		return requestInput, err
	}
	billing := make(map[string]any)
	if seconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(duration); ok {
		billing["duration_seconds"] = seconds
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if !supportsSeventhFrameCanonicalDuration(info) {
		return nil
	}
	return taskcommon.DurationOnlyBillingCapability(taskcommon.ExplicitDurationBillingSchema4To15, 4, 15)
}

func supportsSeventhFrameCanonicalDuration(info *relaycommon.RelayInfo) bool {
	modelName := ""
	if info != nil {
		if info.ChannelMeta != nil {
			modelName = strings.TrimSpace(info.UpstreamModelName)
		}
		if modelName == "" {
			modelName = strings.TrimSpace(info.OriginModelName)
		}
	}
	for _, supported := range ModelList {
		if modelName == supported {
			return true
		}
	}
	return false
}
