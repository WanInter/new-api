package service

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

const DefaultVideoRoutingRequestPath = "/v1/videos"

var supportedVideoRoutingRequestPaths = []string{
	DefaultVideoRoutingRequestPath,
	"/v1/videos/generations",
	"/v1/video/generations",
}

type VideoRoutingRuleCandidate struct {
	Group              string                          `json:"group"`
	ChannelID          int                             `json:"channel_id"`
	ChannelName        string                          `json:"channel_name"`
	ChannelType        int                             `json:"channel_type"`
	ChannelStatus      int                             `json:"channel_status"`
	Priority           int64                           `json:"priority"`
	Weight             int                             `json:"weight"`
	Mapping            common.ModelMappingResolution   `json:"mapping"`
	Capability         *dto.VideoModelCapability       `json:"capability,omitempty"`
	Sources            []string                        `json:"sources,omitempty"`
	ConfigurationError string                          `json:"configuration_error,omitempty"`
	Eligible           *bool                           `json:"eligible,omitempty"`
	SelectedPriority   bool                            `json:"selected_priority,omitempty"`
	Violations         []VideoConstraintViolation      `json:"violations,omitempty"`
	EditableRule       *VideoRoutingCapabilityRuleView `json:"editable_rule,omitempty"`
}

type VideoRoutingRuleSet struct {
	PublicModel  string                      `json:"public_model"`
	Group        string                      `json:"group,omitempty"`
	Strict       bool                        `json:"strict"`
	StrictSource string                      `json:"strict_source"`
	Policy       *VideoRoutingPolicyView     `json:"policy,omitempty"`
	Candidates   []VideoRoutingRuleCandidate `json:"candidates"`
}

type VideoRoutingSimulationRequest struct {
	Model       string `json:"model"`
	Group       string `json:"group"`
	Images      int    `json:"images"`
	Videos      int    `json:"videos"`
	Audios      int    `json:"audios"`
	Duration    *int   `json:"duration,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Retry       int    `json:"retry,omitempty"`
	RequestPath string `json:"request_path,omitempty"`
}

type VideoRoutingSimulationResult struct {
	VideoRoutingRuleSet
	Features       VideoRequestFeatures `json:"features"`
	Retry          int                  `json:"retry"`
	TargetPriority *int64               `json:"target_priority,omitempty"`
}

func GetVideoRoutingRuleSet(publicModel, group string) (VideoRoutingRuleSet, error) {
	return getVideoRoutingRuleSetForPath(publicModel, group, "")
}

func GetVideoRoutingRuleSetForPath(publicModel, group, requestPath string) (VideoRoutingRuleSet, error) {
	return getVideoRoutingRuleSetForPath(publicModel, group, strings.TrimSpace(requestPath))
}

func getVideoRoutingRuleSetForPath(publicModel, group, requestPath string) (VideoRoutingRuleSet, error) {
	publicModel = strings.TrimSpace(publicModel)
	group = strings.TrimSpace(group)
	strict, strictSource, policy := ResolveVideoRoutingStrict(publicModel)
	result := VideoRoutingRuleSet{
		PublicModel:  publicModel,
		Group:        group,
		Strict:       strict,
		StrictSource: strictSource,
		Policy:       policy,
		Candidates:   make([]VideoRoutingRuleCandidate, 0),
	}
	candidates, err := model.GetEnabledChannelAbilityCandidates(group, publicModel)
	if err != nil {
		return result, err
	}
	for _, candidate := range candidates {
		result.Candidates = append(result.Candidates, describeVideoRoutingCandidate(candidate, publicModel, requestPath))
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].Priority == result.Candidates[j].Priority {
			return result.Candidates[i].ChannelID < result.Candidates[j].ChannelID
		}
		return result.Candidates[i].Priority > result.Candidates[j].Priority
	})
	return result, nil
}

func describeVideoRoutingCandidate(candidate model.ChannelAbilityCandidate, publicModel, requestPath string) VideoRoutingRuleCandidate {
	result := VideoRoutingRuleCandidate{
		Group:     candidate.Ability.Group,
		ChannelID: candidate.Ability.ChannelId,
		Weight:    int(candidate.Ability.Weight),
	}
	if candidate.Ability.Priority != nil {
		result.Priority = *candidate.Ability.Priority
	}
	if candidate.Channel == nil {
		result.ConfigurationError = "channel_not_found"
		return result
	}
	result.ChannelName = candidate.Channel.Name
	result.ChannelType = candidate.Channel.Type
	result.ChannelStatus = candidate.Channel.Status
	result.Weight = candidate.Channel.GetWeight()
	result.Priority = candidate.Channel.GetPriority()

	mapping, err := common.ResolveModelMapping(candidate.Channel.GetModelMapping(), publicModel)
	result.Mapping = mapping
	if err != nil {
		result.ConfigurationError = err.Error()
		return result
	}
	if effective, found := ResolveEffectiveVideoCapability(candidate.Channel, mapping.Model); found {
		result.Capability = &effective.Capability
		result.Sources = effective.Sources
	} else if IsStrictVideoRoutingModel(publicModel) {
		result.Violations = []VideoConstraintViolation{{Code: "missing_capability"}}
	}
	result.EditableRule = getChannelVideoRoutingCapabilityRuleView(candidate.Channel.Id, mapping.Model)
	if !channelSupportsVideoRoutingRequestPath(candidate.Channel, requestPath) {
		result.ConfigurationError = "request_path_not_supported"
	}
	return result
}

func channelSupportsVideoRoutingRequestPath(channel *model.Channel, requestPath string) bool {
	if channel == nil || channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	if config == nil {
		return false
	}
	requestPath = strings.TrimSpace(requestPath)
	if requestPath != "" {
		return config.SupportsPath(requestPath)
	}
	for _, supportedPath := range supportedVideoRoutingRequestPaths {
		if config.SupportsPath(supportedPath) {
			return true
		}
	}
	return false
}

func SimulateVideoRouting(request VideoRoutingSimulationRequest) (VideoRoutingSimulationResult, error) {
	requestPath := strings.TrimSpace(request.RequestPath)
	if requestPath == "" {
		requestPath = DefaultVideoRoutingRequestPath
	}
	rules, err := getVideoRoutingRuleSetForPath(request.Model, request.Group, requestPath)
	result := VideoRoutingSimulationResult{
		VideoRoutingRuleSet: rules,
		Features: VideoRequestFeatures{
			Images:      request.Images,
			Videos:      request.Videos,
			Audios:      request.Audios,
			Duration:    request.Duration,
			ContentType: request.ContentType,
		},
		Retry: request.Retry,
	}
	if err != nil {
		return result, err
	}

	priorities := make(map[int64]struct{})
	for i := range result.Candidates {
		candidate := &result.Candidates[i]
		eligible := candidate.ConfigurationError == "" && candidate.Capability != nil
		if candidate.Capability != nil {
			features := videoFeaturesForCapability(result.Features, *candidate.Capability)
			candidate.Violations = MatchVideoCapability(features, *candidate.Capability)
			eligible = eligible && len(candidate.Violations) == 0
		} else if !result.Strict && candidate.ConfigurationError == "" {
			eligible = true
		}
		candidate.Eligible = common.GetPointer(eligible)
		if eligible {
			priorities[candidate.Priority] = struct{}{}
		}
	}

	sortedPriorities := make([]int64, 0, len(priorities))
	for priority := range priorities {
		sortedPriorities = append(sortedPriorities, priority)
	}
	sort.Slice(sortedPriorities, func(i, j int) bool { return sortedPriorities[i] > sortedPriorities[j] })
	if len(sortedPriorities) == 0 {
		return result, nil
	}
	retry := request.Retry
	if retry < 0 {
		retry = 0
	}
	if retry >= len(sortedPriorities) {
		retry = len(sortedPriorities) - 1
	}
	targetPriority := sortedPriorities[retry]
	result.TargetPriority = common.GetPointer(targetPriority)
	for i := range result.Candidates {
		candidate := &result.Candidates[i]
		candidate.SelectedPriority = candidate.Eligible != nil && *candidate.Eligible && candidate.Priority == targetPriority
	}
	return result, nil
}
