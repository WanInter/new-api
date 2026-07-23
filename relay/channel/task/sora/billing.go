package sora

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	directSeedance20Model                   = "seedance-2-0"
	fixedDuration15Schema                   = "video.duration-seconds.fixed-15.v1"
	fixedDuration15Size1280x720Schema       = "video.duration-seconds.fixed-15.size.1280x720.fixed.v1"
	sora2DurationSizeSchema                 = "video.duration-seconds.integer.4-15.explicit-required.size.sora-2-2.default-720x1280.v1"
	sora2ProDurationSizeSchema              = "video.duration-seconds.integer.4-15.explicit-required.size.sora-2-pro-4.default-720x1280.v1"
	seedanceGatewayDurationResolutionSchema = "video.duration-seconds.integer.4-15.default-15.resolution.720p.explicit-required.v1"
	tokenStackSaleDurationResolutionSchema  = "video.duration-seconds.enum-5-10-15.explicit-required.resolution.720p-1080p.explicit-required.v1"
	tokenStackDoubaoDurationSchema          = "video.duration-seconds.integer.4-15.default-5.v1"
	otoyDurationResolutionSchema            = "video.duration-seconds.integer.4-15.explicit-required.resolution.480p-720p.required.v1"
	grokDurationResolutionSchema            = "video.duration-seconds.integer.1-20.explicit-required.resolution.480p-720p.required.v1"
	grok15DurationSizeBillingSchema         = "video.duration-seconds.integer.1-20.default-6.size.480p-720p.required.v1"
)

// BuildBillingInput reads the same top-level duration alias selected by the
// supported Sora JSON and multipart wire transforms. Unknown or non-integral
// values remain absent so a schema-pinned request is rejected by validation.
func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	requestInput := billingexpr.RequestInput{Headers: taskcommon.CanonicalBillingHeaders(c, info)}
	if a.GetTaskBillingCapability(info) == nil {
		return requestInput, nil
	}

	billing, err := canonicalSoraBillingValues(c, info)
	if err != nil {
		return requestInput, err
	}
	return taskcommon.BuildCanonicalBillingInput(c, info, billing)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	modelName := soraCanonicalModelName(info)
	switch modelName {
	case sora2Model:
		return taskcommon.DurationStringBillingCapability(sora2DurationSizeSchema, 4, 15, taskcommon.CanonicalSizePath, []string{"720x1280", "1280x720"})
	case sora2ProModel:
		return taskcommon.DurationStringBillingCapability(sora2ProDurationSizeSchema, 4, 15, taskcommon.CanonicalSizePath, taskcommon.CanonicalOpenAIVideoSizes())
	case seedanceGatewayModel:
		return taskcommon.DurationStringBillingCapability(seedanceGatewayDurationResolutionSchema, 4, 15, taskcommon.CanonicalResolutionPath, []string{"720p"})
	case axMultimodalVideoModel, sdquanImageVideoModel, canvasStandardSeedanceModel:
		return taskcommon.DurationValuesBillingCapability(fixedDuration15Schema, []int{15})
	case otoySeedanceMiniReferenceModel:
		return taskcommon.DurationStringBillingCapability(otoyDurationResolutionSchema, 4, 15, taskcommon.CanonicalResolutionPath, []string{"480p", "720p"})
	case grokImageVideoModel:
		return taskcommon.DurationStringBillingCapability(grokDurationResolutionSchema, 1, 20, taskcommon.CanonicalResolutionPath, []string{"480p", "720p"})
	case grokVideo15PreviewModel:
		return taskcommon.DurationStringBillingCapability(grok15DurationSizeBillingSchema, 1, 20, taskcommon.CanonicalSizePath, []string{"480p", "720p"})
	case veoOmniFlashModel, veoOmniFlashVideoEditModel:
		return taskcommon.DurationSizeBillingCapability(taskcommon.ExplicitRequiredDurationSizeBillingSchema4To15, 4, 15)
	case directSeedance20Model:
		return taskcommon.DurationSizeBillingCapability(taskcommon.ExplicitDurationSizeBillingSchema4To15, 4, 15)
	case tokenStackMultiModeModel:
		return taskcommon.DurationValuesStringBillingCapability(tokenStackSaleDurationResolutionSchema, []int{5, 10, 15}, taskcommon.CanonicalResolutionPath, []string{"720p", "1080p"})
	}
	if _, ok := tokenStackSora15sModels[modelName]; ok {
		return taskcommon.DurationValuesStringBillingCapability(fixedDuration15Size1280x720Schema, []int{15}, taskcommon.CanonicalSizePath, []string{"1280x720"})
	}
	if _, ok := tokenStackDoubaoModels[modelName]; ok {
		return taskcommon.DurationOnlyBillingCapability(tokenStackDoubaoDurationSchema, 4, 15)
	}
	if _, ok := tokenStackMultiResolutionModels[modelName]; ok {
		return taskcommon.DurationValuesBillingCapability(fixedDuration15Schema, []int{15})
	}
	return nil
}

func soraCanonicalModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil {
		if modelName := strings.TrimSpace(info.UpstreamModelName); modelName != "" {
			return modelName
		}
	}
	return strings.TrimSpace(info.OriginModelName)
}

func canonicalSoraBillingValues(c *gin.Context, info *relaycommon.RelayInfo) (map[string]any, error) {
	billing := make(map[string]any)
	if c == nil || c.Request == nil {
		return billing, nil
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		body := make(map[string]any)
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			return nil, err
		}
		if err := applySoraJSONWireTransforms(c, info, body); err != nil {
			return nil, err
		}
		addCanonicalSoraJSONFields(billing, body, soraCanonicalModelName(info))
		return billing, nil
	case strings.Contains(contentType, "multipart/form-data"):
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		defer form.RemoveAll()
		if err := applyNormalizedSoraMultipartVideoOutput(c, form.Value); err != nil {
			return nil, err
		}
		if err := addCanonicalSoraMultipartFields(billing, form.Value, soraCanonicalModelName(info)); err != nil {
			return nil, err
		}
		return billing, nil
	default:
		return billing, nil
	}
}

func addCanonicalSoraJSONFields(billing, body map[string]any, modelName string) {
	switch modelName {
	case axMultimodalVideoModel, sdquanImageVideoModel, canvasStandardSeedanceModel:
		billing["duration_seconds"] = 15
	case seedanceGatewayModel:
		addCanonicalSeedanceGatewayFields(billing, body)
	case tokenStackMultiModeModel:
		addCanonicalTokenStackSaleFields(billing, body)
	case otoySeedanceMiniReferenceModel:
		addCanonicalSoraDuration(billing, body)
		if _, hasUnmappedSize := body["size"]; hasUnmappedSize {
			return
		}
		addCanonicalSoraStringField(billing, "resolution", body["resolution"], hasMapKey(body, "resolution"))
	case grokImageVideoModel:
		addCanonicalSoraDuration(billing, body)
		addCanonicalSoraStringField(billing, "resolution", body["resolution"], hasMapKey(body, "resolution"))
	case grokVideo15PreviewModel:
		addCanonicalSoraDuration(billing, body)
		if _, hasDuration := billing["duration_seconds"]; !hasDuration {
			billing["duration_seconds"] = 6
		}
		addCanonicalSoraStringField(billing, "size", body["size"], hasMapKey(body, "size"))
	case veoOmniFlashModel, veoOmniFlashVideoEditModel:
		addCanonicalSoraDuration(billing, body)
		addCanonicalSoraExplicitSize(billing, body["size"], hasMapKey(body, "size"))
	default:
		if _, ok := tokenStackSora15sModels[modelName]; ok {
			addCanonicalTokenStackSora15sFields(billing, body)
			return
		}
		if _, ok := tokenStackDoubaoModels[modelName]; ok {
			addCanonicalTokenStackDoubaoFields(billing, body)
			return
		}
		if _, ok := tokenStackMultiResolutionModels[modelName]; ok {
			addCanonicalTokenStackMultiResolutionFields(billing, body)
			return
		}
		addCanonicalSoraDuration(billing, body)
		addCanonicalSoraSize(billing, body["size"], hasMapKey(body, "size"))
	}
}

func addCanonicalSoraMultipartFields(billing map[string]any, values map[string][]string, modelName string) error {
	switch modelName {
	case axMultimodalVideoModel, sdquanImageVideoModel, canvasStandardSeedanceModel:
		billing["duration_seconds"] = 15
		return nil
	case seedanceGatewayModel, tokenStackMultiModeModel:
		return nil
	case otoySeedanceMiniReferenceModel:
		addCanonicalSoraFormDuration(billing, values)
		_, resolution, mappedSize := otoyAspectRatioAndResolutionFromForm(values)
		if mappedSize {
			billing["resolution"] = resolution
			return nil
		}
		if len(values["size"]) == 0 && len(values["resolution"]) == 1 {
			addCanonicalSoraStringField(billing, "resolution", values["resolution"][0], true)
		}
		return nil
	case grokImageVideoModel, grokVideo15PreviewModel:
		input, err := grokRequestInputFromMultipartValues(values)
		if err != nil {
			return err
		}
		if modelName == grokImageVideoModel {
			if _, err := validateGrokImageVideoRequest(input); err != nil {
				return err
			}
			if input.HasDuration {
				billing["duration_seconds"] = input.Duration
			}
			billing["resolution"] = strings.ToLower(strings.TrimSpace(input.Resolution))
			return nil
		}
		size, err := validateGrokVideo15Request(input)
		if err != nil {
			return err
		}
		if input.HasDuration {
			billing["duration_seconds"] = input.Duration
		} else {
			billing["duration_seconds"] = 6
		}
		if size != "" {
			billing["size"] = size
		}
		return nil
	case veoOmniFlashModel, veoOmniFlashVideoEditModel:
		addCanonicalSoraFormDuration(billing, values)
		sizes, selected := values["size"]
		if selected && len(sizes) == 1 {
			addCanonicalSoraExplicitSize(billing, sizes[0], true)
		}
		return nil
	default:
		if _, ok := tokenStackSora15sModels[modelName]; ok {
			return nil
		}
		if _, ok := tokenStackDoubaoModels[modelName]; ok {
			return nil
		}
		if _, ok := tokenStackMultiResolutionModels[modelName]; ok {
			return nil
		}
		addCanonicalSoraFormDuration(billing, values)
		sizes, sizeSelected := values["size"]
		if !sizeSelected || len(sizes) == 0 {
			billing["size"] = taskcommon.DefaultCanonicalVideoSize
		} else if len(sizes) == 1 {
			addCanonicalSoraSize(billing, sizes[0], true)
		}
		return nil
	}
}

func addCanonicalSoraFormDuration(billing map[string]any, values map[string][]string) {
	durations, selected := soraDurationValuesFromForm(values)
	if selected && len(durations) == 1 {
		if seconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(durations[0]); ok {
			billing["duration_seconds"] = seconds
		}
	}
}

func applySoraJSONWireTransforms(c *gin.Context, info *relaycommon.RelayInfo, body map[string]any) error {
	applyNormalizedSoraJSONVideoOutput(c, body)
	body["model"] = info.UpstreamModelName
	profile, hasProfile := soraModelProfileForInfo(info)
	if hasProfile {
		if err := applySoraModelJSONProfile(body, profile); err != nil {
			return err
		}
	}
	if !hasProfile || !profile.SkipGenericDurationMapping {
		mapDurationToSoraSeconds(body)
	}
	if hasProfile {
		if err := applySoraModelJSONFinalProfile(body, profile); err != nil {
			return err
		}
	}
	return nil
}

func addCanonicalSoraDuration(billing, body map[string]any) {
	value, selected := body["duration"]
	if !selected {
		value, selected = body["seconds"]
	}
	if !selected {
		return
	}
	if seconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(value); ok {
		billing["duration_seconds"] = seconds
	}
}

func addCanonicalSeedanceGatewayFields(billing, body map[string]any) {
	metadata, _ := soraJSONMetadata(body)
	if metadata == nil {
		return
	}
	if value, exists := metadata["duration"]; exists {
		if seconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(value); ok {
			billing["duration_seconds"] = seconds
		}
	} else {
		billing["duration_seconds"] = 15
	}
	addCanonicalSoraStringField(billing, "resolution", metadata["resolution"], hasMapKey(metadata, "resolution"))
}

func addCanonicalTokenStackSaleFields(billing, body map[string]any) {
	parameters, ok := body["parameters"].(map[string]any)
	if !ok {
		return
	}
	if seconds, ok := strictJSONInteger(parameters["duration"]); ok {
		billing["duration_seconds"] = seconds
	}
	resolution, ok := parameters["resolution"].(string)
	if !ok {
		return
	}
	switch resolution {
	case "720P":
		billing["resolution"] = "720p"
	case "1080P":
		billing["resolution"] = "1080p"
	}
}

func addCanonicalTokenStackSora15sFields(billing, body map[string]any) {
	if seconds, ok := body["seconds"].(string); ok && seconds == "15" {
		billing["duration_seconds"] = 15
	}
	if size, ok := body["size"].(string); ok && size == "1280x720" {
		billing["size"] = size
	}
}

func addCanonicalTokenStackDoubaoFields(billing, body map[string]any) {
	value, exists := body["seconds"]
	if !exists {
		billing["duration_seconds"] = 5
		return
	}
	if seconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(value); ok {
		billing["duration_seconds"] = seconds
	}
}

func addCanonicalTokenStackMultiResolutionFields(billing, body map[string]any) {
	seconds, ok := body["seconds"].(string)
	if ok && seconds == "1" {
		billing["duration_seconds"] = 15
	}
}

func strictJSONInteger(value any) (int, bool) {
	switch value.(type) {
	case nil, string, bool:
		return 0, false
	}
	return taskcommon.PositiveIntegerSecondsFromWireValue(value)
}

func addCanonicalSoraSize(billing map[string]any, value any, selected bool) {
	if !selected {
		billing["size"] = taskcommon.DefaultCanonicalVideoSize
		return
	}
	addCanonicalSoraExplicitSize(billing, value, true)
}

func addCanonicalSoraExplicitSize(billing map[string]any, value any, selected bool) {
	if !selected {
		return
	}
	size, ok := value.(string)
	if !ok || strings.TrimSpace(size) == "" {
		return
	}
	billing["size"] = strings.ToLower(strings.TrimSpace(size))
}

func addCanonicalSoraStringField(billing map[string]any, key string, value any, selected bool) {
	if !selected {
		return
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return
	}
	billing[key] = strings.ToLower(strings.TrimSpace(text))
}

func hasMapKey(values map[string]any, key string) bool {
	_, ok := values[key]
	return ok
}
