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
}

var soraModelProfiles = map[string]soraModelProfile{
	axMultimodalVideoModel: {
		JSONTransform:              requestTransformOpenAIContent,
		DropSecondsField:           true,
		SkipGenericDurationMapping: true,
	},
	sdquanImageVideoModel: {
		JSONTransform:              requestTransformOpenAIContent,
		DropSecondsField:           true,
		SkipGenericDurationMapping: true,
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
}

const (
	tokenStackHostname       = "tokenstack.cc"
	tokenStackMultiModeModel = "seedance-2-0-sale"
)

var tokenStackSora15sModels = map[string]struct{}{
	"seedance-2-0-15s-slow": {},
	"seedance-2-0-15s-high": {},
	"seedance-2-0-15s-fast": {},
}

var tokenStackSora15sProfile = soraModelProfile{
	JSONFinalTransform: requestTransformTokenStackSora15s,
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
