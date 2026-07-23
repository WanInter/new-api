package ali

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

var aliCanonicalDurations = []string{"3", "4", "5", "6", "7", "8", "9", "10"}
var aliCanonicalResolutions = []string{"480p", "720p", "1080p"}

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	payload, err := a.convertToAliRequest(info, req)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	resolution, err := aliCanonicalResolution(payload.Parameters)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	billing := map[string]any{
		"duration_seconds": payload.Parameters.Duration,
		"resolution":       resolution,
	}
	if defaultAudio, _, _, ok := aliCanonicalAudioProfile(payload.Model); ok {
		audioEnabled := defaultAudio
		if payload.Parameters.Audio != nil {
			audioEnabled = *payload.Parameters.Audio
		}
		billing["audio_enabled"] = audioEnabled
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := aliBillingModelName(info)
	if modelName == "" {
		return nil
	}
	payload, err := a.convertToAliRequest(nil, relaycommon.TaskSubmitReq{Model: modelName})
	if err != nil {
		return nil
	}
	defaultResolution, err := aliCanonicalResolution(payload.Parameters)
	if err != nil {
		return nil
	}
	fields := []channel.TaskBillingField{
		{Path: "billing.duration_seconds", Type: "number", Required: true, EnumValues: append([]string(nil), aliCanonicalDurations...)},
		{Path: "billing.resolution", Type: "string", Required: true, EnumValues: append([]string(nil), aliCanonicalResolutions...)},
	}
	schemaVersion := "video.duration-seconds+resolution.duration-3-10.default-5.resolution-480p-720p-1080p.default-" + defaultResolution
	if _, audioValues, audioSchema, ok := aliCanonicalAudioProfile(modelName); ok {
		fields = append(fields, channel.TaskBillingField{
			Path:       "billing.audio_enabled",
			Type:       "boolean",
			Required:   true,
			EnumValues: append([]string(nil), audioValues...),
		})
		schemaVersion += "+audio." + audioSchema
	}
	return &channel.TaskBillingCapability{SchemaVersion: schemaVersion + ".v2", Fields: fields}
}

func aliCanonicalAudioProfile(modelName string) (defaultValue bool, enumValues []string, schema string, ok bool) {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case strings.HasPrefix(modelName, "wan2.6-i2v-flash"):
		return true, []string{"false", "true"}, "values-false-true.default-true.required", true
	case strings.HasPrefix(modelName, "wan2.6"), strings.HasPrefix(modelName, "wan2.5"):
		return true, []string{"true"}, "fixed-true.required", true
	case strings.HasPrefix(modelName, "wan2.2"), strings.HasPrefix(modelName, "wanx2.1"):
		return false, []string{"false"}, "fixed-false.required", true
	default:
		return false, nil, "", false
	}
}

// applyAliCanonicalAudio keeps the audio request field only for the one Wan
// model that exposes it as a switch. Other known Wan models have fixed audio
// behavior, so matching client values are accepted but omitted from the wire.
func applyAliCanonicalAudio(parameters *AliVideoParameters, modelName string) error {
	if parameters == nil {
		return nil
	}
	defaultValue, enumValues, _, ok := aliCanonicalAudioProfile(modelName)
	if !ok || len(enumValues) != 1 {
		return nil
	}
	if parameters.Audio != nil && *parameters.Audio != defaultValue {
		return fmt.Errorf("Ali model %q has fixed audio_enabled=%t", modelName, defaultValue)
	}
	parameters.Audio = nil
	return nil
}

func aliBillingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}

func aliCanonicalResolution(parameters *AliVideoParameters) (string, error) {
	if parameters == nil {
		return "", nil
	}
	if size := strings.TrimSpace(parameters.Size); size != "" {
		resolution, err := sizeToResolution(size)
		return strings.ToLower(resolution), err
	}
	resolution, err := aliResolutionLabel(parameters.Resolution)
	return strings.ToLower(resolution), err
}
