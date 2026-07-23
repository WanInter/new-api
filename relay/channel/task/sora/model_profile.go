package sora

import (
	"net/url"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	sora2Model                     = "sora-2"
	sora2ProModel                  = "sora-2-pro"
	axMultimodalVideoModel         = "ax2.0-9tu"
	sdquanImageVideoModel          = "sdquan-2"
	otoySeedanceMiniReferenceModel = "otoy-image-to-video-seedance-2-0-mini-reference-to-video"
	veoOmniFlashModel              = "veo-omni-flash"
	veoOmniFlashVideoEditModel     = "veo-omni-flash-video-edit"
	grokImageVideoModel            = "grok-video-3"
	grokVideo15PreviewModel        = "grok-imagine-video-1.5-preview"
	seedanceGatewayModel           = "seedance-gateway"
	canvasStandardSeedanceModel    = "navos-local-seedance-154-36-180-7"
	defaultUnprofiledVideoSeconds  = 4
)

type requestTransform int

const (
	requestTransformNone requestTransform = iota
	requestTransformOpenAIContent
	requestTransformOtoySeedanceReference
	requestTransformVeoReferenceImages
	requestTransformTokenStackSora15s
	requestTransformTokenStackDoubao
	requestTransformFixedContent15s
	requestTransformFixedSora15s
	requestTransformSeedanceGateway
	requestTransformGrokImageVideo
	requestTransformGrokVideo15
)

// soraModelProfile describes how this channel's wire format differs from the
// public task request. Profiles with strict provider contracts are validated
// after model mapping and before billing by the adaptor.
type soraModelProfile struct {
	JSONTransform              requestTransform
	JSONFinalTransform         requestTransform
	MultipartTransform         requestTransform
	DropSecondsField           bool
	SkipGenericDurationMapping bool
	RequireFinalUpstreamModel  bool
	DefaultDuration            int
	FixedDuration              int
}

var soraModelProfiles = map[string]soraModelProfile{
	axMultimodalVideoModel: {
		JSONTransform:              requestTransformOpenAIContent,
		JSONFinalTransform:         requestTransformFixedContent15s,
		DropSecondsField:           true,
		SkipGenericDurationMapping: true,
		DefaultDuration:            15,
		FixedDuration:              15,
	},
	sdquanImageVideoModel: {
		JSONTransform:              requestTransformOpenAIContent,
		JSONFinalTransform:         requestTransformFixedContent15s,
		DropSecondsField:           true,
		SkipGenericDurationMapping: true,
		DefaultDuration:            15,
		FixedDuration:              15,
	},
	otoySeedanceMiniReferenceModel: {
		JSONTransform:              requestTransformOtoySeedanceReference,
		MultipartTransform:         requestTransformOtoySeedanceReference,
		DropSecondsField:           true,
		SkipGenericDurationMapping: true,
	},
	veoOmniFlashModel: {
		JSONTransform: requestTransformVeoReferenceImages,
	},
	veoOmniFlashVideoEditModel: {
		JSONTransform: requestTransformVeoReferenceImages,
	},
	grokImageVideoModel: {
		JSONTransform:              requestTransformGrokImageVideo,
		MultipartTransform:         requestTransformGrokImageVideo,
		SkipGenericDurationMapping: true,
		RequireFinalUpstreamModel:  true,
	},
	grokVideo15PreviewModel: {
		JSONTransform:              requestTransformGrokVideo15,
		MultipartTransform:         requestTransformGrokVideo15,
		SkipGenericDurationMapping: true,
		RequireFinalUpstreamModel:  true,
		DefaultDuration:            6,
	},
	seedanceGatewayModel: {
		JSONFinalTransform:         requestTransformSeedanceGateway,
		SkipGenericDurationMapping: true,
		DefaultDuration:            15,
	},
	canvasStandardSeedanceModel: {
		JSONFinalTransform: requestTransformFixedSora15s,
		DefaultDuration:    15,
		FixedDuration:      15,
	},
}

const (
	tokenStackHostname                    = "tokenstack.cc"
	tokenStackMultiModeModel              = "seedance-2-0-sale"
	tokenStackDoubaoModel                 = "doubao-seedance-2-0-260128"
	tokenStackDoubaoFastModel             = "doubao-seedance-2-0-fast-260128"
	tokenStackMultiResolution720FastModel = "seedance-2.0-720p-fast-15s"
)

var tokenStackSora15sModels = map[string]struct{}{
	"seedance-2-0-15s-slow": {},
	"seedance-2-0-15s-high": {},
	"seedance-2-0-15s-fast": {},
}

var tokenStackSora15sProfile = soraModelProfile{
	JSONFinalTransform: requestTransformTokenStackSora15s,
	DefaultDuration:    15,
	FixedDuration:      15,
}

var tokenStackDoubaoModels = map[string]struct{}{
	tokenStackDoubaoModel:     {},
	tokenStackDoubaoFastModel: {},
}

var tokenStackMultiResolutionModels = map[string]struct{}{
	"seedance-2.0-480p-mini-15s":          {},
	"seedance-2.0-480p-fast-15s":          {},
	"seedance-2.0-480p-15s":               {},
	"seedance-2.0-720p-mini-15s":          {},
	tokenStackMultiResolution720FastModel: {},
	"seedance-2.0-720p-pro-15s":           {},
	"seedance-2.0-1080p-15s":              {},
	"seedance-2.0-4k-15s":                 {},
}

var tokenStackDoubaoProfile = soraModelProfile{
	JSONFinalTransform: requestTransformTokenStackDoubao,
	DefaultDuration:    5,
}

var tokenStackMultiResolutionProfile = soraModelProfile{
	DefaultDuration: 15,
	FixedDuration:   15,
}

func findSoraModelProfile(modelName string) (soraModelProfile, bool) {
	profile, ok := soraModelProfiles[strings.TrimSpace(modelName)]
	return profile, ok
}

func soraModelProfileForInfo(info *relaycommon.RelayInfo) (soraModelProfile, bool) {
	if info == nil {
		return soraModelProfile{}, false
	}
	return soraModelProfileForRequest("", info)
}

func soraModelProfileForRequest(requestModel string, info *relaycommon.RelayInfo) (soraModelProfile, bool) {
	profile, ok := baseSoraModelProfileForRequest(requestModel, info)
	tokenStackProfile, hasTokenStackProfile := tokenStackProfileForInfo(info)
	if !hasTokenStackProfile {
		return profile, ok
	}
	if !ok {
		return tokenStackProfile, true
	}
	if tokenStackProfile.JSONFinalTransform != requestTransformNone {
		profile.JSONFinalTransform = tokenStackProfile.JSONFinalTransform
	}
	profile.SkipGenericDurationMapping = tokenStackProfile.SkipGenericDurationMapping
	profile.DefaultDuration = tokenStackProfile.DefaultDuration
	profile.FixedDuration = tokenStackProfile.FixedDuration
	return profile, true
}

func baseSoraModelProfileForRequest(requestModel string, info *relaycommon.RelayInfo) (soraModelProfile, bool) {
	if info != nil && info.ChannelMeta != nil {
		// ModelMappedHelper updates ChannelMeta.UpstreamModelName in place, so this
		// is the final provider model after channel mapping.
		if profile, ok := findSoraModelProfile(info.ChannelMeta.UpstreamModelName); ok {
			return profile, true
		}
	}
	if info != nil {
		if profile, ok := findSoraModelProfile(info.OriginModelName); ok {
			if profile.RequireFinalUpstreamModel && info.ChannelMeta != nil &&
				strings.TrimSpace(info.ChannelMeta.UpstreamModelName) != "" &&
				strings.TrimSpace(info.ChannelMeta.UpstreamModelName) != strings.TrimSpace(info.OriginModelName) {
				return soraModelProfile{}, false
			}
			return profile, true
		}
	}
	return findSoraModelProfile(requestModel)
}

func tokenStackProfileForInfo(info *relaycommon.RelayInfo) (soraModelProfile, bool) {
	if !isTokenStackChannel(info) {
		return soraModelProfile{}, false
	}
	if _, ok := tokenStackSora15sModels[strings.TrimSpace(info.ChannelMeta.UpstreamModelName)]; ok {
		return tokenStackSora15sProfile, true
	}
	if _, ok := tokenStackDoubaoModels[strings.TrimSpace(info.ChannelMeta.UpstreamModelName)]; ok {
		return tokenStackDoubaoProfile, true
	}
	if _, ok := tokenStackMultiResolutionModels[strings.TrimSpace(info.ChannelMeta.UpstreamModelName)]; ok {
		return tokenStackMultiResolutionProfile, true
	}
	return soraModelProfile{}, false
}

func isTokenStackChannel(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(info.ChannelBaseUrl))
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	return hostname == tokenStackHostname || hostname == "www."+tokenStackHostname
}
