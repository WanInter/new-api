package service

import (
	"mime"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

const StrictVideoRoutingModelSDBak1 = "sd-bak-1"

type VideoRequestFeatures struct {
	Images      int    `json:"images"`
	Videos      int    `json:"videos"`
	Audios      int    `json:"audios"`
	Duration    *int   `json:"duration,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type VideoConstraintViolation struct {
	Code     string `json:"code"`
	Field    string `json:"field,omitempty"`
	Actual   *int   `json:"actual,omitempty"`
	Expected *int   `json:"expected,omitempty"`
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

type channelModelCapabilityKey struct {
	ChannelType int
	Model       string
}

var strictVideoRoutingModels = map[string]struct{}{
	StrictVideoRoutingModelSDBak1: {},
}

var defaultVideoCapabilitiesByChannelType = map[int]dto.VideoModelCapability{
	constant.ChannelTypeYobox: capabilityWithLimits(nil, common.GetPointer(4), common.GetPointer(0), common.GetPointer(0), nil, nil),
	// AGGC accepts image, video, and audio arrays. Upstream-specific limits can
	// be narrowed with a channel override once they are known.
	constant.ChannelTypeAGGC: {},
}

var videoCapabilitiesByModel = map[string]dto.VideoModelCapability{
	"ax2.0-9tu": capabilityWithLimits(nil, common.GetPointer(9), common.GetPointer(0), common.GetPointer(0), common.GetPointer(15), common.GetPointer(true)),
	"sdquan-2":  capabilityWithLimits(common.GetPointer(1), common.GetPointer(4), common.GetPointer(3), common.GetPointer(1), common.GetPointer(15), common.GetPointer(true)),
}

var videoCapabilitiesByChannelModel = map[channelModelCapabilityKey]dto.VideoModelCapability{
	{ChannelType: constant.ChannelTypeYobox, Model: "happy-horse-1.1"}: capabilityWithLimits(nil, common.GetPointer(9), common.GetPointer(0), common.GetPointer(0), nil, nil),
}

func capabilityWithLimits(minImages, maxImages, maxVideos, maxAudios, fixedDuration *int, requireJSON *bool) dto.VideoModelCapability {
	return dto.VideoModelCapability{
		Images:        &dto.VideoMediaRange{Min: minImages, Max: maxImages},
		Videos:        &dto.VideoMediaRange{Max: maxVideos},
		Audios:        &dto.VideoMediaRange{Max: maxAudios},
		FixedDuration: fixedDuration,
		RequireJSON:   requireJSON,
	}
}

func IsStrictVideoRoutingModel(modelName string) bool {
	_, ok := strictVideoRoutingModels[strings.TrimSpace(modelName)]
	return ok
}

func GetBuiltInVideoModelCapability(modelName string) (dto.VideoModelCapability, bool) {
	capability, ok := videoCapabilitiesByModel[strings.TrimSpace(modelName)]
	if !ok {
		return dto.VideoModelCapability{}, false
	}
	return mergeVideoCapability(dto.VideoModelCapability{}, capability), true
}

func ResolveEffectiveVideoCapability(channel *model.Channel, upstreamModel string) (*EffectiveVideoCapability, bool) {
	if channel == nil {
		return nil, false
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	var effective dto.VideoModelCapability
	sources := make([]string, 0, 4)
	found := false

	if capability, ok := defaultVideoCapabilitiesByChannelType[channel.Type]; ok {
		effective = mergeVideoCapability(effective, capability)
		sources = append(sources, "channel_type")
		found = true
	}
	if capability, ok := videoCapabilitiesByModel[upstreamModel]; ok {
		effective = mergeVideoCapability(effective, capability)
		sources = append(sources, "model")
		found = true
	}
	if capability, ok := videoCapabilitiesByChannelModel[channelModelCapabilityKey{ChannelType: channel.Type, Model: upstreamModel}]; ok {
		effective = mergeVideoCapability(effective, capability)
		sources = append(sources, "channel_model")
		found = true
	}

	overrides := channel.GetOtherSettings().VideoRouting
	if overrides != nil {
		if capability, ok := overrides.Models["*"]; ok {
			effective = mergeVideoCapability(effective, capability)
			sources = append(sources, "channel_override:*")
			found = true
		}
		if capability, ok := overrides.Models[upstreamModel]; ok {
			effective = mergeVideoCapability(effective, capability)
			sources = append(sources, "channel_override:"+upstreamModel)
			found = true
		}
	}

	if !found {
		return nil, false
	}
	return &EffectiveVideoCapability{Capability: effective, Sources: sources}, true
}

func mergeVideoCapability(base, override dto.VideoModelCapability) dto.VideoModelCapability {
	base.Images = mergeVideoMediaRange(base.Images, override.Images)
	base.Videos = mergeVideoMediaRange(base.Videos, override.Videos)
	base.Audios = mergeVideoMediaRange(base.Audios, override.Audios)
	if override.FixedDuration != nil {
		base.FixedDuration = common.GetPointer(*override.FixedDuration)
	}
	if override.RequireJSON != nil {
		base.RequireJSON = common.GetPointer(*override.RequireJSON)
	}
	return base
}

func mergeVideoMediaRange(base, override *dto.VideoMediaRange) *dto.VideoMediaRange {
	if base == nil && override == nil {
		return nil
	}
	merged := &dto.VideoMediaRange{}
	if base != nil {
		if base.Min != nil {
			merged.Min = common.GetPointer(*base.Min)
		}
		if base.Max != nil {
			merged.Max = common.GetPointer(*base.Max)
		}
	}
	if override != nil {
		if override.Min != nil {
			merged.Min = common.GetPointer(*override.Min)
		}
		if override.Max != nil {
			merged.Max = common.GetPointer(*override.Max)
		}
	}
	return merged
}

func MatchVideoCapability(features VideoRequestFeatures, capability dto.VideoModelCapability) []VideoConstraintViolation {
	violations := make([]VideoConstraintViolation, 0, 4)
	violations = append(violations, matchVideoMediaRange("images", features.Images, capability.Images)...)
	violations = append(violations, matchVideoMediaRange("videos", features.Videos, capability.Videos)...)
	violations = append(violations, matchVideoMediaRange("audios", features.Audios, capability.Audios)...)
	if capability.FixedDuration != nil && features.Duration != nil && *features.Duration != *capability.FixedDuration {
		violations = append(violations, VideoConstraintViolation{
			Code:     "duration_mismatch",
			Field:    "duration",
			Actual:   common.GetPointer(*features.Duration),
			Expected: common.GetPointer(*capability.FixedDuration),
		})
	}
	if capability.RequireJSON != nil && *capability.RequireJSON && !isJSONMediaType(features.ContentType) {
		violations = append(violations, VideoConstraintViolation{Code: "content_type_mismatch", Field: "content_type"})
	}
	return violations
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
	result.Violations = MatchVideoCapability(features, effective.Capability)
	result.Eligible = len(result.Violations) == 0
	return result
}
