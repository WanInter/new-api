package sora

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	sora2Model                     = "sora-2"
	sora2ProModel                  = "sora-2-pro"
	axMultimodalVideoModel         = "ax2.0-9tu"
	sdquanImageVideoModel          = "sdquan-2"
	otoySeedanceMiniReferenceModel = "otoy-image-to-video-seedance-2-0-mini-reference-to-video"
	veoOmniFlashModel              = "veo-omni-flash"
	veoOmniFlashVideoEditModel     = "veo-omni-flash-video-edit"
	seedanceGatewayModel           = "seedance-gateway"
	canvasStandardSeedanceModel    = "navos-local-seedance-154-36-180-7"
	canvasStandardMaxVideoSeconds  = 14
	defaultUnprofiledVideoSeconds  = 4
)

type requestTransform int

const (
	requestTransformNone requestTransform = iota
	requestTransformOpenAIContent
	requestTransformOtoySeedanceReference
	requestTransformVeoReferenceImages
	requestTransformTokenStackSora15s
)

type contentRequestRules struct {
	RequireText        bool
	RequireImage       bool
	MaxImages          *int
	MaxVideos          *int
	MaxAudios          *int
	ConvertLegacyMedia bool
}

type soraModelProfile struct {
	ContentRules                     *contentRequestRules
	JSONTransform                    requestTransform
	JSONFinalTransform               requestTransform
	MultipartTransform               requestTransform
	FixedSeconds                     int
	DefaultSeconds                   int
	MaxSeconds                       int
	DropSecondsField                 bool
	SkipGenericDurationNormalization bool
	RequireJSON                      bool
	AllowedSizes                     []string
	InvalidSizeMessage               string
}

// Model-specific behavior belongs in this registry instead of the main adaptor.
var soraModelProfiles = map[string]soraModelProfile{
	sora2Model: {
		AllowedSizes:       []string{"720x1280", "1280x720"},
		InvalidSizeMessage: "sora-2 size is invalid",
	},
	sora2ProModel: {
		AllowedSizes:       []string{"720x1280", "1280x720", "1792x1024", "1024x1792"},
		InvalidSizeMessage: "sora-2 size is invalid",
	},
	axMultimodalVideoModel: {
		ContentRules:                     contentRulesFromRoutingCapability(axMultimodalVideoModel, false),
		JSONTransform:                    requestTransformOpenAIContent,
		FixedSeconds:                     15,
		DropSecondsField:                 true,
		SkipGenericDurationNormalization: true,
		RequireJSON:                      true,
	},
	sdquanImageVideoModel: {
		ContentRules:                     contentRulesFromRoutingCapability(sdquanImageVideoModel, true),
		JSONTransform:                    requestTransformOpenAIContent,
		FixedSeconds:                     15,
		DropSecondsField:                 true,
		SkipGenericDurationNormalization: true,
		RequireJSON:                      true,
	},
	otoySeedanceMiniReferenceModel: {
		JSONTransform:                    requestTransformOtoySeedanceReference,
		MultipartTransform:               requestTransformOtoySeedanceReference,
		DropSecondsField:                 true,
		SkipGenericDurationNormalization: true,
	},
	veoOmniFlashModel: {
		JSONTransform: requestTransformVeoReferenceImages,
	},
	veoOmniFlashVideoEditModel: {
		JSONTransform: requestTransformVeoReferenceImages,
	},
	seedanceGatewayModel: {
		DefaultSeconds: 15,
	},
	canvasStandardSeedanceModel: {
		MaxSeconds: canvasStandardMaxVideoSeconds,
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

var tokenStackMultiResolutionModels = map[string]struct{}{
	"seedance-2.0-480p-mini-15s": {},
	"seedance-2.0-480p-fast-15s": {},
	"seedance-2.0-480p-15s":      {},
	"seedance-2.0-720p-mini-15s": {},
	"seedance-2.0-720p-fast-15s": {},
	"seedance-2.0-720p-pro-15s":  {},
	"seedance-2.0-1080p-15s":     {},
	"seedance-2.0-4k-15s":        {},
}

var tokenStackSora15sProfile = soraModelProfile{
	JSONFinalTransform: requestTransformTokenStackSora15s,
	FixedSeconds:       15,
	RequireJSON:        true,
}

func contentRulesFromRoutingCapability(modelName string, requireImage bool) *contentRequestRules {
	rules := &contentRequestRules{
		RequireText:        true,
		RequireImage:       requireImage,
		ConvertLegacyMedia: true,
	}
	capability, ok := service.GetBuiltInVideoModelCapability(modelName)
	if !ok {
		return rules
	}
	if capability.Images != nil && capability.Images.Max != nil {
		rules.MaxImages = capability.Images.Max
	}
	if capability.Videos != nil && capability.Videos.Max != nil {
		rules.MaxVideos = capability.Videos.Max
	}
	if capability.Audios != nil && capability.Audios.Max != nil {
		rules.MaxAudios = capability.Audios.Max
	}
	return rules
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
	profile.RequireJSON = profile.RequireJSON || tokenStackProfile.RequireJSON
	if tokenStackProfile.JSONFinalTransform != requestTransformNone {
		profile.JSONFinalTransform = tokenStackProfile.JSONFinalTransform
	}
	return profile, true
}

func baseSoraModelProfileForRequest(requestModel string, info *relaycommon.RelayInfo) (soraModelProfile, bool) {
	if info != nil && info.ChannelMeta != nil {
		if profile, ok := findSoraModelProfile(info.ChannelMeta.UpstreamModelName); ok {
			return profile, true
		}
	}
	if info != nil {
		if profile, ok := findSoraModelProfile(info.OriginModelName); ok {
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
	return soraModelProfile{RequireJSON: true}, true
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

type soraTimingProfile struct {
	FixedSeconds   int
	DefaultSeconds int
	MaxSeconds     int
}

func resolveSoraTimingProfile(requestModel string, info *relaycommon.RelayInfo) soraTimingProfile {
	timing := soraTimingProfile{}
	if target, ok := soraModelProfileForRequest(requestModel, info); ok {
		timing.FixedSeconds = target.FixedSeconds
	}

	modelNames := []string{requestModel}
	if info != nil {
		modelNames = append(modelNames, info.OriginModelName)
		if info.ChannelMeta != nil {
			modelNames = append(modelNames, info.ChannelMeta.UpstreamModelName)
		}
	}
	seen := make(map[string]bool)
	for _, modelName := range modelNames {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" || seen[modelName] {
			continue
		}
		seen[modelName] = true
		profile, ok := findSoraModelProfile(modelName)
		if !ok {
			continue
		}
		if timing.FixedSeconds == 0 {
			timing.FixedSeconds = profile.FixedSeconds
		}
		if timing.DefaultSeconds == 0 {
			timing.DefaultSeconds = profile.DefaultSeconds
		}
		if profile.MaxSeconds > 0 && (timing.MaxSeconds == 0 || profile.MaxSeconds < timing.MaxSeconds) {
			timing.MaxSeconds = profile.MaxSeconds
		}
	}
	return timing
}

func fixedVideoSecondsForModel(requestModel string, info *relaycommon.RelayInfo) int {
	return resolveSoraTimingProfile(requestModel, info).FixedSeconds
}

func defaultVideoSecondsForModel(requestModel string, info *relaycommon.RelayInfo) int {
	if seconds := resolveSoraTimingProfile(requestModel, info).DefaultSeconds; seconds > 0 {
		return seconds
	}
	return defaultUnprofiledVideoSeconds
}

func maxVideoSecondsForModel(info *relaycommon.RelayInfo) int {
	return resolveSoraTimingProfile("", info).MaxSeconds
}

func validateSoraModelRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	profile, ok := soraModelProfileForRequest(req.Model, info)
	if !ok {
		return nil
	}
	if taskErr := validateSoraModelContentType(c, req.Model, profile); taskErr != nil {
		return taskErr
	}
	if len(profile.AllowedSizes) > 0 && strings.TrimSpace(req.Size) != "" && !containsString(profile.AllowedSizes, req.Size) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("%s", profile.InvalidSizeMessage), "invalid_size", http.StatusBadRequest)
	}
	if profile.ContentRules == nil {
		return nil
	}

	counts, validationErr := countProfiledContent(req)
	if validationErr != nil {
		return service.TaskErrorWrapperLocal(validationErr, "invalid_request", http.StatusBadRequest)
	}
	rules := profile.ContentRules
	if rules.RequireText && counts.text == 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("content must contain at least one non-empty text item"), "invalid_request", http.StatusBadRequest)
	}
	if rules.RequireImage && counts.images == 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model %s requires at least one image reference", req.Model), "invalid_request", http.StatusBadRequest)
	}
	if rules.MaxImages != nil && counts.images > *rules.MaxImages {
		return service.TaskErrorWrapperLocal(fmt.Errorf("content supports at most %d image references", *rules.MaxImages), "invalid_request", http.StatusBadRequest)
	}
	if rules.MaxVideos != nil && counts.videos > *rules.MaxVideos {
		return service.TaskErrorWrapperLocal(fmt.Errorf("content supports at most %d video references", *rules.MaxVideos), "invalid_request", http.StatusBadRequest)
	}
	if rules.MaxAudios != nil && counts.audios > *rules.MaxAudios {
		return service.TaskErrorWrapperLocal(fmt.Errorf("content supports at most %d audio references", *rules.MaxAudios), "invalid_request", http.StatusBadRequest)
	}
	return nil
}

func validateSoraModelContentType(c *gin.Context, modelName string, profile soraModelProfile) *dto.TaskError {
	if !profile.RequireJSON {
		return nil
	}
	contentType := ""
	if c != nil && c.Request != nil {
		contentType = c.GetHeader("Content-Type")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "application/json") {
		return nil
	}
	return service.TaskErrorWrapperLocal(
		fmt.Errorf("model %s only supports application/json requests", modelName),
		"unsupported_content_type",
		http.StatusUnsupportedMediaType,
	)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type contentItemCounts struct {
	text   int
	images int
	videos int
	audios int
}

func countProfiledContent(req relaycommon.TaskSubmitReq) (contentItemCounts, error) {
	counts := contentItemCounts{}
	if len(req.Content) == 0 {
		if strings.TrimSpace(req.Prompt) != "" {
			counts.text = 1
		}
		counts.images = countNonEmptyStrings(req.Images) +
			countNonEmptyStrings(req.ImageURLs) +
			countNonEmptyStrings(req.InputStartFrames) +
			countNonEmptyStrings(req.InputImageReferences) +
			countNonEmptyStrings(req.MetadataStartFrames)
		if strings.TrimSpace(req.Image) != "" {
			counts.images++
		}
		if strings.TrimSpace(req.InputReference) != "" {
			counts.images++
		}
		counts.videos = countNonEmptyStrings(req.Videos) + countNonEmptyStrings(req.VideoURLs)
		counts.audios = countNonEmptyStrings(req.Audios) + countNonEmptyStrings(req.AudioURLs)
		return counts, nil
	}

	for _, item := range req.Content {
		switch item.Type {
		case "text":
			if strings.TrimSpace(item.Text) == "" {
				return counts, fmt.Errorf("content text item must not be empty")
			}
			counts.text++
		case "image_url":
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return counts, fmt.Errorf("content image_url item requires image_url.url")
			}
			counts.images++
		case "video_url":
			if item.VideoURL == nil || strings.TrimSpace(item.VideoURL.URL) == "" {
				return counts, fmt.Errorf("content video_url item requires video_url.url")
			}
			counts.videos++
		case "audio_url":
			if item.AudioURL == nil || strings.TrimSpace(item.AudioURL.URL) == "" {
				return counts, fmt.Errorf("content audio_url item requires audio_url.url")
			}
			counts.audios++
		default:
			return counts, fmt.Errorf("unsupported content item type: %s", item.Type)
		}
	}
	return counts, nil
}

func countNonEmptyStrings(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}
