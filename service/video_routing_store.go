package service

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

type VideoRoutingPolicyView struct {
	Id          int    `json:"id"`
	PublicModel string `json:"public_model"`
	Strict      bool   `json:"strict"`
	Revision    int    `json:"revision"`
	UpdatedBy   int    `json:"updated_by"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

type VideoRoutingCapabilityRuleView struct {
	Id            int                      `json:"id"`
	Scope         string                   `json:"scope"`
	ChannelType   int                      `json:"channel_type"`
	ChannelId     int                      `json:"channel_id"`
	UpstreamModel string                   `json:"upstream_model"`
	Capability    dto.VideoModelCapability `json:"capability"`
	Revision      int                      `json:"revision"`
	UpdatedBy     int                      `json:"updated_by"`
	CreatedTime   int64                    `json:"created_time"`
	UpdatedTime   int64                    `json:"updated_time"`
}

type videoRoutingCapabilityRuleKey struct {
	Scope         string
	ChannelType   int
	ChannelId     int
	UpstreamModel string
}

type cachedVideoRoutingCapabilityRule struct {
	Rule       model.VideoRoutingCapabilityRule
	Capability dto.VideoModelCapability
}

type videoRoutingRuleSnapshot struct {
	Policies map[string]model.VideoRoutingPolicy
	Rules    map[videoRoutingCapabilityRuleKey]cachedVideoRoutingCapabilityRule
}

var videoRoutingRules atomic.Pointer[videoRoutingRuleSnapshot]

// ErrVideoRoutingRuleTablesUnavailable indicates that the routing-rule schema
// has not been migrated yet. This is expected during slave-node startup and
// is retried by the background cache synchronizer.
var ErrVideoRoutingRuleTablesUnavailable = errors.New("video routing rule tables unavailable")

func init() {
	videoRoutingRules.Store(newVideoRoutingRuleSnapshot())
}

func newVideoRoutingRuleSnapshot() *videoRoutingRuleSnapshot {
	return &videoRoutingRuleSnapshot{
		Policies: make(map[string]model.VideoRoutingPolicy),
		Rules:    make(map[videoRoutingCapabilityRuleKey]cachedVideoRoutingCapabilityRule),
	}
}

func ReloadVideoRoutingRuleCache() error {
	if !model.VideoRoutingRuleTablesAvailable() {
		return ErrVideoRoutingRuleTablesUnavailable
	}
	policies, err := model.GetAllVideoRoutingPolicies()
	if err != nil {
		return err
	}
	rules, err := model.GetAllVideoRoutingCapabilityRules()
	if err != nil {
		return err
	}

	snapshot := newVideoRoutingRuleSnapshot()
	for _, policy := range policies {
		publicModel := strings.TrimSpace(policy.PublicModel)
		if publicModel == "" {
			return fmt.Errorf("video routing policy %d has an empty public model", policy.Id)
		}
		policy.PublicModel = publicModel
		snapshot.Policies[publicModel] = policy
	}
	for _, rule := range rules {
		rule.UpstreamModel = strings.TrimSpace(rule.UpstreamModel)
		if err := validateVideoRoutingRuleScope(rule); err != nil {
			return fmt.Errorf("video routing capability rule %d: %w", rule.Id, err)
		}
		var capability dto.VideoModelCapability
		if err := common.UnmarshalJsonStr(rule.Capability, &capability); err != nil {
			return fmt.Errorf("video routing capability rule %d: %w", rule.Id, err)
		}
		if err := capability.Validate(); err != nil {
			return fmt.Errorf("video routing capability rule %d: %w", rule.Id, err)
		}
		if isEmptyVideoCapability(capability) {
			return fmt.Errorf("video routing capability rule %d is empty", rule.Id)
		}
		key := videoRoutingCapabilityRuleKey{
			Scope:         rule.Scope,
			ChannelType:   rule.ChannelType,
			ChannelId:     rule.ChannelId,
			UpstreamModel: rule.UpstreamModel,
		}
		snapshot.Rules[key] = cachedVideoRoutingCapabilityRule{Rule: rule, Capability: capability}
	}
	videoRoutingRules.Store(snapshot)
	return nil
}

func SyncVideoRoutingRuleCache(frequency int) {
	if frequency <= 0 {
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		if err := ReloadVideoRoutingRuleCache(); err != nil && !errors.Is(err, ErrVideoRoutingRuleTablesUnavailable) {
			common.SysError("failed to sync video routing rules from database: " + err.Error())
		}
	}
}

func ResolveVideoRoutingStrict(publicModel string) (bool, string, *VideoRoutingPolicyView) {
	publicModel = strings.TrimSpace(publicModel)
	if snapshot := videoRoutingRules.Load(); snapshot != nil {
		if policy, ok := snapshot.Policies[publicModel]; ok {
			view := videoRoutingPolicyView(policy)
			return policy.Strict, "database", &view
		}
	}
	_, strict := strictVideoRoutingModels[publicModel]
	if strict {
		return true, "built_in", nil
	}
	return false, "default", nil
}

func UpsertVideoRoutingPolicy(publicModel string, strict bool, revision int, updatedBy int) (*VideoRoutingPolicyView, error) {
	publicModel = strings.TrimSpace(publicModel)
	if publicModel == "" {
		return nil, fmt.Errorf("public model is required")
	}
	if len(publicModel) > 255 {
		return nil, fmt.Errorf("public model must not exceed 255 characters")
	}
	if revision < 0 {
		return nil, fmt.Errorf("revision must be non-negative")
	}
	policy, err := model.UpsertVideoRoutingPolicy(publicModel, strict, revision, updatedBy)
	if err != nil {
		return nil, err
	}
	if err := ReloadVideoRoutingRuleCache(); err != nil {
		return nil, err
	}
	view := videoRoutingPolicyView(*policy)
	return &view, nil
}

func UpsertChannelVideoRoutingCapabilityRule(channelId int, upstreamModel string, capability dto.VideoModelCapability, revision int, updatedBy int) (*VideoRoutingCapabilityRuleView, error) {
	if channelId <= 0 {
		return nil, fmt.Errorf("channel id must be positive")
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		return nil, fmt.Errorf("upstream model is required")
	}
	if len(upstreamModel) > 255 {
		return nil, fmt.Errorf("upstream model must not exceed 255 characters")
	}
	if revision < 0 {
		return nil, fmt.Errorf("revision must be non-negative")
	}
	if _, err := model.GetChannelById(channelId, false); err != nil {
		return nil, err
	}
	if err := capability.Validate(); err != nil {
		return nil, err
	}
	if isEmptyVideoCapability(capability) {
		return nil, fmt.Errorf("capability override must contain at least one field")
	}
	capabilityBytes, err := common.Marshal(capability)
	if err != nil {
		return nil, err
	}
	rule, err := model.UpsertVideoRoutingCapabilityRule(model.VideoRoutingCapabilityRule{
		Scope:         model.VideoRoutingScopeChannelModel,
		ChannelId:     channelId,
		UpstreamModel: upstreamModel,
		Capability:    string(capabilityBytes),
		UpdatedBy:     updatedBy,
	}, revision)
	if err != nil {
		return nil, err
	}
	if err := ReloadVideoRoutingRuleCache(); err != nil {
		return nil, err
	}
	view := videoRoutingCapabilityRuleView(*rule, capability)
	return &view, nil
}

func DeleteVideoRoutingCapabilityRule(id int, revision int) (*VideoRoutingCapabilityRuleView, error) {
	if id <= 0 || revision <= 0 {
		return nil, fmt.Errorf("rule id and revision must be positive")
	}
	rule, err := model.GetVideoRoutingCapabilityRuleById(id)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, model.ErrVideoRoutingRevisionConflict
	}
	var capability dto.VideoModelCapability
	if err := common.UnmarshalJsonStr(rule.Capability, &capability); err != nil {
		return nil, err
	}
	if err := model.DeleteVideoRoutingCapabilityRule(id, revision); err != nil {
		return nil, err
	}
	if err := ReloadVideoRoutingRuleCache(); err != nil {
		return nil, err
	}
	view := videoRoutingCapabilityRuleView(*rule, capability)
	return &view, nil
}

func getCachedVideoRoutingCapabilityRule(scope string, channelType int, channelId int, upstreamModel string) (cachedVideoRoutingCapabilityRule, bool) {
	snapshot := videoRoutingRules.Load()
	if snapshot == nil {
		return cachedVideoRoutingCapabilityRule{}, false
	}
	rule, ok := snapshot.Rules[videoRoutingCapabilityRuleKey{
		Scope:         scope,
		ChannelType:   channelType,
		ChannelId:     channelId,
		UpstreamModel: strings.TrimSpace(upstreamModel),
	}]
	return rule, ok
}

func getChannelVideoRoutingCapabilityRuleView(channelId int, upstreamModel string) *VideoRoutingCapabilityRuleView {
	cached, ok := getCachedVideoRoutingCapabilityRule(model.VideoRoutingScopeChannelModel, 0, channelId, upstreamModel)
	if !ok {
		return nil
	}
	view := videoRoutingCapabilityRuleView(cached.Rule, cached.Capability)
	return &view
}

func videoRoutingPolicyView(policy model.VideoRoutingPolicy) VideoRoutingPolicyView {
	return VideoRoutingPolicyView{
		Id:          policy.Id,
		PublicModel: policy.PublicModel,
		Strict:      policy.Strict,
		Revision:    policy.Revision,
		UpdatedBy:   policy.UpdatedBy,
		CreatedTime: policy.CreatedTime,
		UpdatedTime: policy.UpdatedTime,
	}
}

func videoRoutingCapabilityRuleView(rule model.VideoRoutingCapabilityRule, capability dto.VideoModelCapability) VideoRoutingCapabilityRuleView {
	return VideoRoutingCapabilityRuleView{
		Id:            rule.Id,
		Scope:         rule.Scope,
		ChannelType:   rule.ChannelType,
		ChannelId:     rule.ChannelId,
		UpstreamModel: rule.UpstreamModel,
		Capability:    capability,
		Revision:      rule.Revision,
		UpdatedBy:     rule.UpdatedBy,
		CreatedTime:   rule.CreatedTime,
		UpdatedTime:   rule.UpdatedTime,
	}
}

func validateVideoRoutingRuleScope(rule model.VideoRoutingCapabilityRule) error {
	switch rule.Scope {
	case model.VideoRoutingScopeChannelType:
		if rule.ChannelType <= 0 || rule.ChannelId != 0 || rule.UpstreamModel != "" {
			return fmt.Errorf("invalid channel_type scope")
		}
	case model.VideoRoutingScopeUpstreamModel:
		if rule.ChannelType != 0 || rule.ChannelId != 0 || rule.UpstreamModel == "" {
			return fmt.Errorf("invalid upstream_model scope")
		}
	case model.VideoRoutingScopeChannelTypeModel:
		if rule.ChannelType <= 0 || rule.ChannelId != 0 || rule.UpstreamModel == "" {
			return fmt.Errorf("invalid channel_type_model scope")
		}
	case model.VideoRoutingScopeChannel:
		if rule.ChannelType != 0 || rule.ChannelId <= 0 || rule.UpstreamModel != "" {
			return fmt.Errorf("invalid channel scope")
		}
	case model.VideoRoutingScopeChannelModel:
		if rule.ChannelType != 0 || rule.ChannelId <= 0 || rule.UpstreamModel == "" {
			return fmt.Errorf("invalid channel_model scope")
		}
	default:
		return fmt.Errorf("unsupported scope %q", rule.Scope)
	}
	return nil
}

func isEmptyVideoCapability(capability dto.VideoModelCapability) bool {
	return capability.Images == nil &&
		capability.Videos == nil &&
		capability.Audios == nil &&
		capability.Duration == nil &&
		capability.FixedDuration == nil &&
		len(capability.Resolutions) == 0 &&
		capability.RequireJSON == nil &&
		capability.RequireText == nil &&
		capability.ContentPrecedence == nil
}
