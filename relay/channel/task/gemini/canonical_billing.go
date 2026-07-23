package gemini

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

var veoCanonicalDurations = []string{"4", "6", "8"}

func ResolveVeoParameters(req *relaycommon.TaskSubmitReq, modelName string) (*VeoParameters, error) {
	if req == nil {
		return nil, fmt.Errorf("video request is required")
	}
	params := &VeoParameters{}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, params); err != nil {
		return nil, fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	if params.DurationSeconds == 0 {
		params.DurationSeconds = ResolveVeoDuration(nil, req.Duration, req.Seconds)
	}
	if !veoDurationSupported(params.DurationSeconds) {
		return nil, fmt.Errorf("Veo duration must be 4, 6, or 8 seconds")
	}
	output, err := ResolveVeoRequestOutput(req, modelName)
	if err != nil {
		return nil, err
	}
	if output.Resolution != "" {
		params.Resolution = output.Resolution
	}
	if output.AspectRatio != "" {
		params.AspectRatio = output.AspectRatio
	}
	if strings.TrimSpace(params.Resolution) == "" {
		params.Resolution = defaultVeoResolution
	}
	params.Resolution = strings.ToLower(params.Resolution)
	params.SampleCount = 1
	return params, nil
}

// resolveGeminiVeoParameters applies the Gemini Developer API contract. Veo
// 3.x always generates audio there, and generateAudio itself is not accepted
// by that API surface.
func resolveGeminiVeoParameters(req *relaycommon.TaskSubmitReq, modelName string) (*VeoParameters, error) {
	params, err := ResolveVeoParameters(req, modelName)
	if err != nil {
		return nil, err
	}
	if params.GenerateAudio != nil && !*params.GenerateAudio {
		return nil, fmt.Errorf("Gemini Veo 3.x always generates audio; generateAudio=false is not supported")
	}
	params.GenerateAudio = nil
	return params, nil
}

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	params, err := resolveGeminiVeoParameters(&req, veoModelName(info))
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	return BuildVeoCanonicalBillingInput(c, info, params, true)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	return GetGeminiVeoBillingCapability(veoModelName(info))
}

func BuildVeoCanonicalBillingInput(c *gin.Context, info *relaycommon.RelayInfo, params *VeoParameters, audioEnabled bool) (billingexpr.RequestInput, error) {
	if params == nil {
		return billingexpr.RequestInput{}, fmt.Errorf("Veo parameters are required")
	}
	billing := map[string]any{
		"duration_seconds": params.DurationSeconds,
		"resolution":       params.Resolution,
		"audio_enabled":    audioEnabled,
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func GetGeminiVeoBillingCapability(modelName string) *channel.TaskBillingCapability {
	return getVeoBillingCapability(modelName, "fixed-true.required", []string{"true"})
}

func GetVertexVeoBillingCapability(modelName string) *channel.TaskBillingCapability {
	return getVeoBillingCapability(modelName, "values-false-true.default-false.required", []string{"false", "true"})
}

func getVeoBillingCapability(modelName, audioSchema string, audioValues []string) *channel.TaskBillingCapability {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	resolutions := []string{"720p", "1080p"}
	if strings.Contains(modelName, "veo-3.1") {
		resolutions = append(resolutions, "4k")
	} else if !strings.Contains(modelName, "veo-3.0") {
		return nil
	}
	return &channel.TaskBillingCapability{
		SchemaVersion: "video.duration-seconds+resolution+audio.duration-4-6-8.default-8.resolution-" + strings.Join(resolutions, "-") + ".default-720p.audio-" + audioSchema + ".v3",
		Fields: []channel.TaskBillingField{
			{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: append([]string(nil), veoCanonicalDurations...)},
			{Path: "billing.resolution", Type: "string", Required: true, EnumValues: resolutions},
			{Path: "billing.audio_enabled", Type: "boolean", Required: true, EnumValues: append([]string(nil), audioValues...)},
		},
	}
}

func veoDurationSupported(duration int) bool {
	for _, value := range veoCanonicalDurations {
		if strconv.Itoa(duration) == value {
			return true
		}
	}
	return false
}
