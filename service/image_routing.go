package service

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	ImageRoutingTier1K = "1k"
	ImageRoutingTier2K = "2k"
	ImageRoutingTier4K = "4k"

	defaultImageRoutingSize = "1024x1024"
	ginKeyImageRouting      = "image_routing_resolution"
)

var ErrImageRoutingRuleTablesUnavailable = errors.New("image routing rule tables unavailable")

type ImageRoutingPolicyView struct {
	Id          int    `json:"id"`
	PublicModel string `json:"public_model"`
	Strict      bool   `json:"strict"`
	DefaultSize string `json:"default_size"`
	Revision    int    `json:"revision"`
	UpdatedBy   int    `json:"updated_by"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

type ImageRoutingSizeInput struct {
	Size string `json:"size"`
	Tier string `json:"tier"`
	Sort int    `json:"sort"`
}

type ImageRoutingRuleInput struct {
	Tier      string `json:"tier"`
	ChannelID int    `json:"channel_id"`
	Rank      int    `json:"rank"`
}

type ReplaceImageRoutingConfigRequest struct {
	PublicModel string                  `json:"public_model"`
	Strict      bool                    `json:"strict"`
	DefaultSize string                  `json:"default_size"`
	Revision    int                     `json:"revision"`
	Sizes       []ImageRoutingSizeInput `json:"sizes"`
	Rules       []ImageRoutingRuleInput `json:"rules"`
}

type UpdateImageRoutingPolicyRequest struct {
	PublicModel string `json:"public_model"`
	Strict      bool   `json:"strict"`
	DefaultSize string `json:"default_size"`
	Revision    int    `json:"revision"`
}

type ImageRoutingChannelView struct {
	ChannelID          int                           `json:"channel_id"`
	ChannelName        string                        `json:"channel_name,omitempty"`
	ChannelType        int                           `json:"channel_type,omitempty"`
	ChannelStatus      int                           `json:"channel_status,omitempty"`
	Priority           int64                         `json:"priority,omitempty"`
	Weight             int                           `json:"weight,omitempty"`
	Group              string                        `json:"group,omitempty"`
	Mapping            common.ModelMappingResolution `json:"mapping"`
	Tier               string                        `json:"tier,omitempty"`
	Rank               int                           `json:"rank,omitempty"`
	Eligible           bool                          `json:"eligible"`
	Selected           bool                          `json:"selected,omitempty"`
	ExclusionReason    string                        `json:"exclusion_reason,omitempty"`
	ConfigurationError string                        `json:"configuration_error,omitempty"`
}

type ImageRoutingConfigView struct {
	PublicModel string                    `json:"public_model"`
	Group       string                    `json:"group,omitempty"`
	Configured  bool                      `json:"configured"`
	Policy      *ImageRoutingPolicyView   `json:"policy,omitempty"`
	Strict      bool                      `json:"strict"`
	DefaultSize string                    `json:"default_size"`
	Revision    int                       `json:"revision"`
	Sizes       []ImageRoutingSizeInput   `json:"sizes"`
	Rules       []ImageRoutingRuleInput   `json:"rules"`
	Candidates  []ImageRoutingChannelView `json:"candidates"`
}

type ImageRoutingSimulationRequest struct {
	Model string `json:"model"`
	Group string `json:"group"`
	Size  string `json:"size,omitempty"`
}

type ImageRoutingSimulationResult struct {
	ImageRoutingConfigView
	RequestedSize   string                    `json:"requested_size"`
	NormalizedSize  string                    `json:"normalized_size,omitempty"`
	ResolvedTier    string                    `json:"resolved_tier,omitempty"`
	UsedDefaultSize bool                      `json:"used_default_size"`
	Fallback        bool                      `json:"fallback"`
	Reason          string                    `json:"reason,omitempty"`
	Route           []ImageRoutingChannelView `json:"route"`
}

type ImageRoutingRequestError struct {
	Message string
}

func (e *ImageRoutingRequestError) Error() string {
	return e.Message
}

type ImageRoutingResolution struct {
	Configured      bool
	Active          bool
	Strict          bool
	RequestedSize   string
	NormalizedSize  string
	Tier            string
	UsedDefaultSize bool
	Reason          string
	Rules           []model.ImageRoutingRule
}

type imageRoutingRuleSnapshot struct {
	Policies map[string]model.ImageRoutingPolicy
	Sizes    map[string]map[string]model.ImageRoutingSize
	Rules    map[string]map[string][]model.ImageRoutingRule
}

var imageRoutingRules atomic.Pointer[imageRoutingRuleSnapshot]

func init() {
	imageRoutingRules.Store(newImageRoutingRuleSnapshot())
}

func newImageRoutingRuleSnapshot() *imageRoutingRuleSnapshot {
	return &imageRoutingRuleSnapshot{
		Policies: make(map[string]model.ImageRoutingPolicy),
		Sizes:    make(map[string]map[string]model.ImageRoutingSize),
		Rules:    make(map[string]map[string][]model.ImageRoutingRule),
	}
}

func ReloadImageRoutingRuleCache() error {
	if !model.ImageRoutingRuleTablesAvailable() {
		return ErrImageRoutingRuleTablesUnavailable
	}
	policies, err := model.GetAllImageRoutingPolicies()
	if err != nil {
		return err
	}
	sizes, err := model.GetAllImageRoutingSizes()
	if err != nil {
		return err
	}
	rules, err := model.GetAllImageRoutingRules()
	if err != nil {
		return err
	}

	snapshot := newImageRoutingRuleSnapshot()
	for _, policy := range policies {
		policy.PublicModel = strings.TrimSpace(policy.PublicModel)
		if policy.PublicModel == "" {
			return fmt.Errorf("image routing policy %d has an empty public model", policy.Id)
		}
		normalizedDefault, err := NormalizeImageRoutingSize(policy.DefaultSize)
		if err != nil {
			return fmt.Errorf("image routing policy %d: %w", policy.Id, err)
		}
		policy.DefaultSize = normalizedDefault
		snapshot.Policies[policy.PublicModel] = policy
	}
	for _, size := range sizes {
		normalized, err := NormalizeImageRoutingSize(size.Size)
		if err != nil {
			return fmt.Errorf("image routing size %d: %w", size.Id, err)
		}
		tier, err := normalizeImageRoutingTier(size.Tier)
		if err != nil {
			return fmt.Errorf("image routing size %d: %w", size.Id, err)
		}
		size.Size = normalized
		size.Tier = tier
		if snapshot.Sizes[size.PublicModel] == nil {
			snapshot.Sizes[size.PublicModel] = make(map[string]model.ImageRoutingSize)
		}
		if _, exists := snapshot.Sizes[size.PublicModel][normalized]; exists {
			return fmt.Errorf("image routing size %d duplicates %s", size.Id, normalized)
		}
		snapshot.Sizes[size.PublicModel][normalized] = size
	}
	for _, rule := range rules {
		tier, err := normalizeImageRoutingTier(rule.Tier)
		if err != nil {
			return fmt.Errorf("image routing rule %d: %w", rule.Id, err)
		}
		if rule.ChannelId <= 0 || rule.Rank <= 0 {
			return fmt.Errorf("image routing rule %d has an invalid channel or rank", rule.Id)
		}
		rule.Tier = tier
		if snapshot.Rules[rule.PublicModel] == nil {
			snapshot.Rules[rule.PublicModel] = make(map[string][]model.ImageRoutingRule)
		}
		snapshot.Rules[rule.PublicModel][tier] = append(snapshot.Rules[rule.PublicModel][tier], rule)
	}
	for publicModel, byTier := range snapshot.Rules {
		for tier := range byTier {
			sort.SliceStable(byTier[tier], func(i, j int) bool {
				if byTier[tier][i].Rank == byTier[tier][j].Rank {
					return byTier[tier][i].ChannelId < byTier[tier][j].ChannelId
				}
				return byTier[tier][i].Rank < byTier[tier][j].Rank
			})
		}
		snapshot.Rules[publicModel] = byTier
	}
	for publicModel, policy := range snapshot.Policies {
		if _, ok := snapshot.Sizes[publicModel][policy.DefaultSize]; !ok {
			return fmt.Errorf("image routing policy %s default size is not in its size catalog", publicModel)
		}
	}
	imageRoutingRules.Store(snapshot)
	return nil
}

func SyncImageRoutingRuleCache(frequency int) {
	if frequency <= 0 {
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		if err := ReloadImageRoutingRuleCache(); err != nil && !errors.Is(err, ErrImageRoutingRuleTablesUnavailable) {
			common.SysError("failed to sync image routing rules from database: " + err.Error())
		}
	}
}

func NormalizeImageRoutingSize(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "×", "x")
	normalized = strings.ReplaceAll(normalized, " ", "")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid image size %q; expected WIDTHxHEIGHT", value)
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil || width <= 0 {
		return "", fmt.Errorf("invalid image size %q; width must be positive", value)
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil || height <= 0 {
		return "", fmt.Errorf("invalid image size %q; height must be positive", value)
	}
	if width > 99999 || height > 99999 {
		return "", fmt.Errorf("invalid image size %q; dimensions are too large", value)
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}

func GetImageRoutingConfigView(publicModel, group string) (ImageRoutingConfigView, error) {
	publicModel = strings.TrimSpace(publicModel)
	group = strings.TrimSpace(group)
	result := imageRoutingConfigFromSnapshot(publicModel, group)
	candidates, err := model.GetEnabledChannelAbilityCandidates(group, publicModel)
	if err != nil {
		return result, err
	}
	result.Candidates = make([]ImageRoutingChannelView, 0, len(candidates))
	for _, candidate := range candidates {
		result.Candidates = append(result.Candidates, describeImageRoutingAbility(candidate, publicModel))
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].Priority == result.Candidates[j].Priority {
			return result.Candidates[i].ChannelID < result.Candidates[j].ChannelID
		}
		return result.Candidates[i].Priority > result.Candidates[j].Priority
	})
	return result, nil
}

func ReplaceImageRoutingConfig(request ReplaceImageRoutingConfigRequest, updatedBy int) (ImageRoutingConfigView, error) {
	publicModel, defaultSize, sizes, rules, err := validateImageRoutingConfig(request)
	if err != nil {
		return ImageRoutingConfigView{}, err
	}
	policy, err := model.ReplaceImageRoutingConfig(
		publicModel,
		request.Strict,
		defaultSize,
		request.Revision,
		updatedBy,
		sizes,
		rules,
	)
	if err != nil {
		return ImageRoutingConfigView{}, err
	}
	if err := ReloadImageRoutingRuleCache(); err != nil {
		return ImageRoutingConfigView{}, err
	}
	result, err := GetImageRoutingConfigView(publicModel, "")
	if err != nil {
		return result, err
	}
	view := imageRoutingPolicyView(*policy)
	result.Policy = &view
	return result, nil
}

func UpdateImageRoutingPolicy(request UpdateImageRoutingPolicyRequest, updatedBy int) (ImageRoutingConfigView, error) {
	policy, sizes, rules, err := model.GetImageRoutingConfig(strings.TrimSpace(request.PublicModel))
	if err != nil {
		return ImageRoutingConfigView{}, err
	}
	if policy == nil {
		return ImageRoutingConfigView{}, fmt.Errorf("image routing configuration does not exist")
	}
	sizeInputs := make([]ImageRoutingSizeInput, 0, len(sizes))
	for _, size := range sizes {
		sizeInputs = append(sizeInputs, ImageRoutingSizeInput{Size: size.Size, Tier: size.Tier, Sort: size.Sort})
	}
	ruleInputs := make([]ImageRoutingRuleInput, 0, len(rules))
	for _, rule := range rules {
		ruleInputs = append(ruleInputs, ImageRoutingRuleInput{Tier: rule.Tier, ChannelID: rule.ChannelId, Rank: rule.Rank})
	}
	return ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: request.PublicModel,
		Strict:      request.Strict,
		DefaultSize: request.DefaultSize,
		Revision:    request.Revision,
		Sizes:       sizeInputs,
		Rules:       ruleInputs,
	}, updatedBy)
}

func SimulateImageRouting(request ImageRoutingSimulationRequest) (ImageRoutingSimulationResult, error) {
	request.Model = strings.TrimSpace(request.Model)
	request.Group = strings.TrimSpace(request.Group)
	if request.Model == "" {
		return ImageRoutingSimulationResult{}, fmt.Errorf("model is required")
	}
	if request.Group == "" {
		return ImageRoutingSimulationResult{}, fmt.Errorf("group is required")
	}
	config, err := GetImageRoutingConfigView(request.Model, request.Group)
	if err != nil {
		return ImageRoutingSimulationResult{}, err
	}
	result := ImageRoutingSimulationResult{
		ImageRoutingConfigView: config,
		RequestedSize:          strings.TrimSpace(request.Size),
		Route:                  make([]ImageRoutingChannelView, 0),
	}
	resolution, err := resolveImageRouting(request.Model, request.Size)
	if err != nil {
		result.Reason = err.Error()
		return result, nil
	}
	if resolution == nil || !resolution.Configured {
		result.Fallback = true
		result.Reason = "not_configured"
		return result, nil
	}
	result.NormalizedSize = resolution.NormalizedSize
	result.ResolvedTier = resolution.Tier
	result.UsedDefaultSize = resolution.UsedDefaultSize
	result.Fallback = !resolution.Active
	result.Reason = resolution.Reason
	for _, rule := range resolution.Rules {
		candidate := describeImageRoutingRule(rule, request.Model, request.Group)
		result.Route = append(result.Route, candidate)
	}
	for i := range result.Route {
		if result.Route[i].Eligible {
			result.Route[i].Selected = true
			break
		}
	}
	return result, nil
}

func IsImageRoutingRequestError(err error) bool {
	var requestErr *ImageRoutingRequestError
	return errors.As(err, &requestErr)
}

func ShouldBypassImageRoutingAffinity(c *gin.Context, publicModel string) bool {
	if !isImageGenerationRequest(c) {
		return false
	}
	resolution, err := resolveImageRoutingForRequest(c, publicModel)
	// A strict validation error must reach the regular channel selection path,
	// where it is returned to the caller instead of being hidden by affinity.
	if err != nil {
		return true
	}
	return resolution != nil && resolution.Active
}

func ImageRoutingRetryLimit(c *gin.Context, publicModel string, fallback int) int {
	resolution, err := resolveImageRoutingForRequest(c, publicModel)
	if err != nil || resolution == nil || !resolution.Active {
		return fallback
	}
	configured := len(resolution.Rules) - 1
	if configured > fallback {
		return configured
	}
	return fallback
}

func channelSupportsImageRouting(c *gin.Context, channel *model.Channel, publicModel string) bool {
	resolution, err := resolveImageRoutingForRequest(c, publicModel)
	if err != nil || resolution == nil || !resolution.Active {
		return err == nil
	}
	for _, rule := range resolution.Rules {
		if channel != nil && rule.ChannelId == channel.Id {
			return true
		}
	}
	return false
}

func imageRoutingFilterForGroup(
	c *gin.Context,
	publicModel string,
	group string,
	_ int,
	baseFilter model.ChannelFilter,
) (model.ChannelFilter, error) {
	resolution, err := resolveImageRoutingForRequest(c, publicModel)
	if err != nil {
		return nil, err
	}
	if resolution == nil || !resolution.Active {
		return baseFilter, nil
	}
	available := make([]int, 0, len(resolution.Rules))
	usedChannels := make(map[int]struct{})
	for _, used := range c.GetStringSlice("use_channel") {
		if channelID, err := strconv.Atoi(used); err == nil {
			usedChannels[channelID] = struct{}{}
		}
	}
	for _, rule := range resolution.Rules {
		if _, used := usedChannels[rule.ChannelId]; used {
			continue
		}
		if !model.IsChannelEnabledForGroupModel(group, publicModel, rule.ChannelId) {
			continue
		}
		channel, err := model.CacheGetChannel(rule.ChannelId)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if baseFilter != nil && !baseFilter(channel) {
			continue
		}
		available = append(available, rule.ChannelId)
	}
	if len(available) == 0 {
		return func(*model.Channel) bool { return false }, nil
	}
	channelID := available[0]
	return func(channel *model.Channel) bool {
		return channel != nil && channel.Id == channelID
	}, nil
}

func resolveImageRoutingForRequest(c *gin.Context, publicModel string) (*ImageRoutingResolution, error) {
	if !isImageGenerationRequest(c) {
		return nil, nil
	}
	if cached, ok := c.Get(ginKeyImageRouting); ok {
		if resolution, ok := cached.(*ImageRoutingResolution); ok {
			return resolution, nil
		}
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	var request dto.ImageRequest
	if err := common.Unmarshal(body, &request); err != nil {
		return nil, &ImageRoutingRequestError{Message: "invalid image request: " + err.Error()}
	}
	resolution, err := resolveImageRouting(publicModel, request.Size)
	if err != nil {
		return nil, err
	}
	if resolution != nil {
		c.Set(ginKeyImageRouting, resolution)
	}
	return resolution, nil
}

func resolveImageRouting(publicModel, requestedSize string) (*ImageRoutingResolution, error) {
	snapshot := imageRoutingRules.Load()
	if snapshot == nil {
		return nil, nil
	}
	publicModel = strings.TrimSpace(publicModel)
	policy, configured := snapshot.Policies[publicModel]
	if !configured {
		return nil, nil
	}
	resolution := &ImageRoutingResolution{
		Configured:    true,
		Strict:        policy.Strict,
		RequestedSize: strings.TrimSpace(requestedSize),
	}
	size := strings.TrimSpace(requestedSize)
	if size == "" {
		size = policy.DefaultSize
		resolution.UsedDefaultSize = true
	}
	normalized, err := NormalizeImageRoutingSize(size)
	if err != nil {
		if policy.Strict {
			return nil, &ImageRoutingRequestError{Message: err.Error()}
		}
		resolution.Reason = "invalid_size"
		return resolution, nil
	}
	resolution.NormalizedSize = normalized
	sizeRule, ok := snapshot.Sizes[publicModel][normalized]
	if !ok {
		if policy.Strict {
			return nil, &ImageRoutingRequestError{Message: fmt.Sprintf("image size %s is not configured for model %s", normalized, publicModel)}
		}
		resolution.Reason = "unknown_size"
		return resolution, nil
	}
	resolution.Tier = sizeRule.Tier
	resolution.Rules = append([]model.ImageRoutingRule(nil), snapshot.Rules[publicModel][sizeRule.Tier]...)
	if len(resolution.Rules) == 0 {
		if policy.Strict {
			return nil, &ImageRoutingRequestError{Message: fmt.Sprintf("image routing tier %s has no configured channels", sizeRule.Tier)}
		}
		resolution.Reason = "no_tier_rules"
		return resolution, nil
	}
	resolution.Active = true
	return resolution, nil
}

func isImageGenerationRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return false
	}
	return c.Request.URL.Path == "/v1/images/generations" || c.Request.URL.Path == "/v1/image/generations"
}

func imageRoutingConfigFromSnapshot(publicModel, group string) ImageRoutingConfigView {
	result := ImageRoutingConfigView{
		PublicModel: publicModel,
		Group:       group,
		DefaultSize: defaultImageRoutingSize,
		Sizes:       make([]ImageRoutingSizeInput, 0),
		Rules:       make([]ImageRoutingRuleInput, 0),
		Candidates:  make([]ImageRoutingChannelView, 0),
	}
	snapshot := imageRoutingRules.Load()
	if snapshot == nil {
		return result
	}
	policy, ok := snapshot.Policies[publicModel]
	if !ok {
		return result
	}
	result.Configured = true
	result.Strict = policy.Strict
	result.DefaultSize = policy.DefaultSize
	result.Revision = policy.Revision
	view := imageRoutingPolicyView(policy)
	result.Policy = &view
	for _, size := range snapshot.Sizes[publicModel] {
		result.Sizes = append(result.Sizes, ImageRoutingSizeInput{Size: size.Size, Tier: size.Tier, Sort: size.Sort})
	}
	sort.SliceStable(result.Sizes, func(i, j int) bool {
		if result.Sizes[i].Sort == result.Sizes[j].Sort {
			return result.Sizes[i].Size < result.Sizes[j].Size
		}
		return result.Sizes[i].Sort < result.Sizes[j].Sort
	})
	for _, tier := range []string{ImageRoutingTier1K, ImageRoutingTier2K, ImageRoutingTier4K} {
		for _, rule := range snapshot.Rules[publicModel][tier] {
			result.Rules = append(result.Rules, ImageRoutingRuleInput{Tier: tier, ChannelID: rule.ChannelId, Rank: rule.Rank})
		}
	}
	return result
}

func validateImageRoutingConfig(request ReplaceImageRoutingConfigRequest) (string, string, []model.ImageRoutingSize, []model.ImageRoutingRule, error) {
	publicModel := strings.TrimSpace(request.PublicModel)
	if publicModel == "" {
		return "", "", nil, nil, fmt.Errorf("public model is required")
	}
	if len(publicModel) > 255 {
		return "", "", nil, nil, fmt.Errorf("public model must not exceed 255 characters")
	}
	if request.Revision < 0 {
		return "", "", nil, nil, fmt.Errorf("revision must be non-negative")
	}
	defaultSize, err := NormalizeImageRoutingSize(request.DefaultSize)
	if err != nil {
		return "", "", nil, nil, err
	}
	sizes := make([]model.ImageRoutingSize, 0, len(request.Sizes))
	seenSizes := make(map[string]struct{}, len(request.Sizes))
	for index, input := range request.Sizes {
		size, err := NormalizeImageRoutingSize(input.Size)
		if err != nil {
			return "", "", nil, nil, err
		}
		if _, ok := seenSizes[size]; ok {
			return "", "", nil, nil, fmt.Errorf("duplicate image size %s", size)
		}
		seenSizes[size] = struct{}{}
		tier, err := normalizeImageRoutingTier(input.Tier)
		if err != nil {
			return "", "", nil, nil, err
		}
		sortOrder := input.Sort
		if sortOrder < 0 {
			return "", "", nil, nil, fmt.Errorf("image size sort must be non-negative")
		}
		if sortOrder == 0 {
			sortOrder = index + 1
		}
		sizes = append(sizes, model.ImageRoutingSize{Size: size, Tier: tier, Sort: sortOrder})
	}
	if len(sizes) == 0 {
		return "", "", nil, nil, fmt.Errorf("at least one image size is required")
	}
	if _, ok := seenSizes[defaultSize]; !ok {
		return "", "", nil, nil, fmt.Errorf("default image size must be present in the size catalog")
	}

	rules := make([]model.ImageRoutingRule, 0, len(request.Rules))
	seenChannels := make(map[string]struct{}, len(request.Rules))
	ranksByTier := make(map[string][]int)
	for _, input := range request.Rules {
		tier, err := normalizeImageRoutingTier(input.Tier)
		if err != nil {
			return "", "", nil, nil, err
		}
		if input.ChannelID <= 0 {
			return "", "", nil, nil, fmt.Errorf("channel id must be positive")
		}
		if input.Rank <= 0 {
			return "", "", nil, nil, fmt.Errorf("image routing rank must be positive")
		}
		key := fmt.Sprintf("%s:%d", tier, input.ChannelID)
		if _, ok := seenChannels[key]; ok {
			return "", "", nil, nil, fmt.Errorf("channel %d is duplicated in tier %s", input.ChannelID, tier)
		}
		seenChannels[key] = struct{}{}
		if _, err := model.GetChannelById(input.ChannelID, false); err != nil {
			return "", "", nil, nil, fmt.Errorf("channel %d: %w", input.ChannelID, err)
		}
		ranksByTier[tier] = append(ranksByTier[tier], input.Rank)
		rules = append(rules, model.ImageRoutingRule{Tier: tier, ChannelId: input.ChannelID, Rank: input.Rank})
	}
	for tier, ranks := range ranksByTier {
		sort.Ints(ranks)
		for i, rank := range ranks {
			if rank != i+1 {
				return "", "", nil, nil, fmt.Errorf("tier %s ranks must be contiguous starting at 1", tier)
			}
		}
	}
	return publicModel, defaultSize, sizes, rules, nil
}

func normalizeImageRoutingTier(value string) (string, error) {
	tier := strings.ToLower(strings.TrimSpace(value))
	switch tier {
	case ImageRoutingTier1K, ImageRoutingTier2K, ImageRoutingTier4K:
		return tier, nil
	default:
		return "", fmt.Errorf("unsupported image routing tier %q", value)
	}
}

func imageRoutingPolicyView(policy model.ImageRoutingPolicy) ImageRoutingPolicyView {
	return ImageRoutingPolicyView{
		Id:          policy.Id,
		PublicModel: policy.PublicModel,
		Strict:      policy.Strict,
		DefaultSize: policy.DefaultSize,
		Revision:    policy.Revision,
		UpdatedBy:   policy.UpdatedBy,
		CreatedTime: policy.CreatedTime,
		UpdatedTime: policy.UpdatedTime,
	}
}

func describeImageRoutingAbility(candidate model.ChannelAbilityCandidate, publicModel string) ImageRoutingChannelView {
	result := ImageRoutingChannelView{
		ChannelID: candidate.Ability.ChannelId,
		Group:     candidate.Ability.Group,
		Weight:    int(candidate.Ability.Weight),
		Eligible:  candidate.Ability.Enabled,
	}
	if candidate.Ability.Priority != nil {
		result.Priority = *candidate.Ability.Priority
	}
	if candidate.Channel == nil {
		result.Eligible = false
		result.ExclusionReason = "channel_not_found"
		return result
	}
	result.ChannelName = candidate.Channel.Name
	result.ChannelType = candidate.Channel.Type
	result.ChannelStatus = candidate.Channel.Status
	result.Priority = candidate.Channel.GetPriority()
	result.Weight = candidate.Channel.GetWeight()
	result.Mapping, _ = common.ResolveModelMapping(candidate.Channel.GetModelMapping(), publicModel)
	if candidate.Channel.Status != common.ChannelStatusEnabled {
		result.Eligible = false
		result.ExclusionReason = "channel_disabled"
	}
	return result
}

func describeImageRoutingRule(rule model.ImageRoutingRule, publicModel, group string) ImageRoutingChannelView {
	result := ImageRoutingChannelView{
		ChannelID: rule.ChannelId,
		Tier:      rule.Tier,
		Rank:      rule.Rank,
		Group:     group,
	}
	channel, err := model.CacheGetChannel(rule.ChannelId)
	if err != nil || channel == nil {
		result.ExclusionReason = "channel_not_found"
		return result
	}
	result.ChannelName = channel.Name
	result.ChannelType = channel.Type
	result.ChannelStatus = channel.Status
	result.Priority = channel.GetPriority()
	result.Weight = channel.GetWeight()
	result.Mapping, err = common.ResolveModelMapping(channel.GetModelMapping(), publicModel)
	if err != nil {
		result.ConfigurationError = err.Error()
		result.ExclusionReason = "invalid_model_mapping"
		return result
	}
	if channel.Status != common.ChannelStatusEnabled {
		result.ExclusionReason = "channel_disabled"
		return result
	}
	if !model.IsChannelEnabledForGroupModel(group, publicModel, channel.Id) {
		result.ExclusionReason = "model_unavailable"
		return result
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		config := channel.GetOtherSettings().AdvancedCustom
		if config == nil || (!config.SupportsPath("/v1/images/generations") && !config.SupportsPath("/v1/image/generations")) {
			result.ExclusionReason = "request_path_not_supported"
			return result
		}
	}
	result.Eligible = true
	return result
}

func imageRoutingNoAvailableChannelError(c *gin.Context, publicModel, group string) error {
	if len(c.GetStringSlice("use_channel")) > 0 {
		return nil
	}
	resolution, err := resolveImageRoutingForRequest(c, publicModel)
	if err != nil {
		return err
	}
	if resolution == nil || !resolution.Active || !resolution.Strict {
		return nil
	}
	return &ImageRoutingRequestError{Message: fmt.Sprintf(
		"image routing tier %s has no available channels in group %s",
		resolution.Tier,
		group,
	)}
}
