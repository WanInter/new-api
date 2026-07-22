package service

import (
	"mime"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// StrictVideoRoutingModelSDBak1 is retained for compatibility with existing
// routing-policy records. Strictness is now configured only through an
// explicit video_routing_policy, never inferred from this model name.
const StrictVideoRoutingModelSDBak1 = "sd-bak-1"

type VideoRequestFeatures struct {
	Images      int    `json:"images"`
	Videos      int    `json:"videos"`
	Audios      int    `json:"audios"`
	Duration    *int   `json:"duration,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Size        string `json:"size,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	ContentType string `json:"content_type,omitempty"`

	profiledContent         *videoMediaCounts
	providerResolutionHints videoProviderResolutionHints
}

type videoMediaCounts struct {
	Images  int
	Videos  int
	Audios  int
	Text    int
	Invalid bool
}

// videoProviderResolutionHints holds nested wire-format fields that are not
// part of the public video contract. They are deliberately kept private so
// API responses and routing simulation remain channel-agnostic.
type videoProviderResolutionHints struct {
	soraParameters string
	aggcParams     string
}

type VideoConstraintViolation struct {
	Code                  string   `json:"code"`
	Field                 string   `json:"field,omitempty"`
	Actual                *int     `json:"actual,omitempty"`
	Expected              *int     `json:"expected,omitempty"`
	AspectRatio           string   `json:"aspect_ratio,omitempty"`
	SupportedAspectRatios []string `json:"supported_aspect_ratios,omitempty"`
	Size                  string   `json:"size,omitempty"`
	SupportedSizes        []string `json:"supported_sizes,omitempty"`
	Resolution            string   `json:"resolution,omitempty"`
	SupportedResolutions  []string `json:"supported_resolutions,omitempty"`
}

type EffectiveVideoCapability struct {
	Capability dto.VideoModelCapability `json:"capability"`
	Sources    []string                 `json:"sources"`
}

type ChannelVideoRoutingEvaluation struct {
	Eligible           bool                          `json:"eligible"`
	Strict             bool                          `json:"strict"`
	Mapping            common.ModelMappingResolution `json:"mapping"`
	Capability         *dto.VideoModelCapability     `json:"capability,omitempty"`
	Sources            []string                      `json:"sources,omitempty"`
	Violations         []VideoConstraintViolation    `json:"violations,omitempty"`
	ConfigurationError string                        `json:"configuration_error,omitempty"`
}

func IsStrictVideoRoutingModel(modelName string) bool {
	strict, _, _ := ResolveVideoRoutingStrict(modelName)
	return strict
}

// ResolveEffectiveVideoCapability resolves only the exact selected channel and
// mapped upstream model. Model limits must not leak to another channel using
// the same adapter or to another model on the same channel.
func ResolveEffectiveVideoCapability(channel *model.Channel, upstreamModel string) (*EffectiveVideoCapability, bool) {
	if channel == nil || channel.Id <= 0 {
		return nil, false
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		return nil, false
	}
	cached, ok := getCachedVideoRoutingCapabilityRule(model.VideoRoutingScopeChannelModel, 0, channel.Id, upstreamModel)
	if !ok {
		return nil, false
	}
	return &EffectiveVideoCapability{
		Capability: cached.Capability,
		Sources:    []string{"database:channel_model#" + strconv.Itoa(cached.Rule.Id)},
	}, true
}

func MatchVideoCapability(features VideoRequestFeatures, capability dto.VideoModelCapability) []VideoConstraintViolation {
	violations := make([]VideoConstraintViolation, 0, 5)
	violations = append(violations, matchVideoMediaRange("images", features.Images, capability.Images)...)
	violations = append(violations, matchVideoMediaRange("videos", features.Videos, capability.Videos)...)
	violations = append(violations, matchVideoMediaRange("audios", features.Audios, capability.Audios)...)
	violations = append(violations, matchVideoMediaRange("video_audio_total", features.Videos+features.Audios, capability.VideoAudioTotal)...)
	if features.Duration != nil {
		if capability.FixedDuration != nil && *features.Duration != *capability.FixedDuration {
			violations = append(violations, VideoConstraintViolation{
				Code:     "duration_mismatch",
				Field:    "duration",
				Actual:   common.GetPointer(*features.Duration),
				Expected: common.GetPointer(*capability.FixedDuration),
			})
		} else if capability.Duration != nil {
			violations = append(violations, matchVideoMediaRange("duration", *features.Duration, capability.Duration)...)
		}
	}
	if features.AspectRatio != "" && len(capability.AspectRatios) > 0 && !containsVideoAspectRatio(capability.AspectRatios, features.AspectRatio) {
		violations = append(violations, VideoConstraintViolation{
			Code:                  "aspect_ratio_not_supported",
			Field:                 "aspect_ratio",
			AspectRatio:           features.AspectRatio,
			SupportedAspectRatios: append([]string(nil), capability.AspectRatios...),
		})
	}
	if features.Size != "" && len(capability.Sizes) > 0 && !containsVideoSize(capability.Sizes, features.Size) {
		violations = append(violations, VideoConstraintViolation{
			Code:           "size_not_supported",
			Field:          "size",
			Size:           features.Size,
			SupportedSizes: append([]string(nil), capability.Sizes...),
		})
	}
	if features.Resolution != "" && len(capability.Resolutions) > 0 && !containsVideoResolution(capability.Resolutions, features.Resolution) {
		violations = append(violations, VideoConstraintViolation{
			Code:                 "resolution_not_supported",
			Field:                "resolution",
			Resolution:           features.Resolution,
			SupportedResolutions: append([]string(nil), capability.Resolutions...),
		})
	}
	if capability.RequireJSON != nil && *capability.RequireJSON && !isJSONMediaType(features.ContentType) {
		violations = append(violations, VideoConstraintViolation{Code: "content_type_mismatch", Field: "content_type"})
	}
	if capability.ContentPrecedence != nil && *capability.ContentPrecedence && features.profiledContent != nil {
		if features.profiledContent.Invalid {
			violations = append(violations, VideoConstraintViolation{Code: "invalid_content", Field: "content"})
		}
		if capability.RequireText != nil && *capability.RequireText && features.profiledContent.Text == 0 {
			violations = append(violations, VideoConstraintViolation{
				Code: "text_below_min", Field: "text",
				Actual: common.GetPointer(0), Expected: common.GetPointer(1),
			})
		}
	}
	return violations
}

func containsVideoAspectRatio(aspectRatios []string, aspectRatio string) bool {
	for _, supported := range aspectRatios {
		if supported == aspectRatio {
			return true
		}
	}
	return false
}

func containsVideoSize(sizes []string, size string) bool {
	for _, supported := range sizes {
		if supported == size {
			return true
		}
	}
	return false
}

func containsVideoResolution(resolutions []string, resolution string) bool {
	for _, supported := range resolutions {
		if supported == resolution {
			return true
		}
	}
	return false
}

func videoFeaturesForCapability(features VideoRequestFeatures, capability dto.VideoModelCapability) VideoRequestFeatures {
	if features.profiledContent == nil {
		return features
	}
	if capability.ContentPrecedence == nil || !*capability.ContentPrecedence {
		return features
	}
	features.Images = features.profiledContent.Images
	features.Videos = features.profiledContent.Videos
	features.Audios = features.profiledContent.Audios
	return features
}

// videoFeaturesForChannelCapability lets a provider-specific nested resolution
// participate in matching only for the channel that owns that wire shape. A
// standard top-level or metadata resolution always wins over a provider hint.
func videoFeaturesForChannelCapability(features VideoRequestFeatures, channel *model.Channel) VideoRequestFeatures {
	if channel == nil || features.Resolution != "" {
		return features
	}

	switch channel.Type {
	case constant.ChannelTypeSora, constant.ChannelTypeOpenAI:
		features.Resolution = features.providerResolutionHints.soraParameters
	case constant.ChannelTypeAGGC:
		features.Resolution = features.providerResolutionHints.aggcParams
	}
	return features
}

func matchVideoMediaRange(field string, actual int, mediaRange *dto.VideoMediaRange) []VideoConstraintViolation {
	if mediaRange == nil {
		return nil
	}
	violations := make([]VideoConstraintViolation, 0, 1)
	if mediaRange.Min != nil && actual < *mediaRange.Min {
		violations = append(violations, VideoConstraintViolation{
			Code:     field + "_below_min",
			Field:    field,
			Actual:   common.GetPointer(actual),
			Expected: common.GetPointer(*mediaRange.Min),
		})
	}
	if mediaRange.Max != nil && actual > *mediaRange.Max {
		violations = append(violations, VideoConstraintViolation{
			Code:     field + "_above_max",
			Field:    field,
			Actual:   common.GetPointer(actual),
			Expected: common.GetPointer(*mediaRange.Max),
		})
	}
	return violations
}

func isJSONMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func EvaluateChannelVideoRouting(channel *model.Channel, publicModel string, features VideoRequestFeatures) ChannelVideoRoutingEvaluation {
	result := ChannelVideoRoutingEvaluation{Strict: IsStrictVideoRoutingModel(publicModel)}
	if channel == nil {
		result.ConfigurationError = "channel_not_found"
		return result
	}

	mapping, err := common.ResolveModelMapping(channel.GetModelMapping(), publicModel)
	result.Mapping = mapping
	if err != nil {
		result.ConfigurationError = err.Error()
		return result
	}

	effective, found := ResolveEffectiveVideoCapability(channel, mapping.Model)
	if !found {
		if result.Strict {
			result.Violations = []VideoConstraintViolation{{Code: "missing_capability"}}
			return result
		}
		result.Eligible = true
		return result
	}

	result.Capability = &effective.Capability
	result.Sources = effective.Sources
	features = videoFeaturesForCapability(features, effective.Capability)
	features = videoFeaturesForChannelCapability(features, channel)
	result.Violations = MatchVideoCapability(features, effective.Capability)
	result.Eligible = len(result.Violations) == 0
	return result
}
