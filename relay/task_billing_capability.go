package relay

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// TaskBillingChannelCapability is a non-sensitive channel entry returned to
// administrators when configuring a model-level canonical billing expression.
type TaskBillingChannelCapability struct {
	ChannelID       int    `json:"channel_id"`
	ChannelName     string `json:"channel_name"`
	ChannelType     int    `json:"channel_type"`
	UpstreamModel   string `json:"upstream_model,omitempty"`
	SchemaVersion   string `json:"schema_version,omitempty"`
	Incompatibility string `json:"incompatibility,omitempty"`
}

// TaskBillingCapabilitySummary describes whether every currently enabled video
// route for a public model can safely project into one canonical schema.
type TaskBillingCapabilitySummary struct {
	Model                string                         `json:"model"`
	Applicable           bool                           `json:"applicable"`
	Compatible           bool                           `json:"compatible"`
	QuotaPerUnit         float64                        `json:"quota_per_unit"`
	CheckedAt            int64                          `json:"checked_at"`
	SchemaVersion        string                         `json:"schema_version,omitempty"`
	Fields               []channel.TaskBillingField     `json:"fields,omitempty"`
	CompatibleChannels   []TaskBillingChannelCapability `json:"compatible_channels"`
	IncompatibleChannels []TaskBillingChannelCapability `json:"incompatible_channels"`
	Reason               string                         `json:"reason,omitempty"`
}

// GetTaskBillingCapabilitySummary resolves every enabled ability route for a
// model. Billing settings are model-wide, therefore a dynamic expression is
// safe when every video route exposes a compatible canonical projection.
func GetTaskBillingCapabilitySummary(modelName string) (*TaskBillingCapabilitySummary, error) {
	modelName = strings.TrimSpace(modelName)
	summary := &TaskBillingCapabilitySummary{
		Model:                modelName,
		QuotaPerUnit:         common.QuotaPerUnit,
		CheckedAt:            time.Now().Unix(),
		CompatibleChannels:   make([]TaskBillingChannelCapability, 0),
		IncompatibleChannels: make([]TaskBillingChannelCapability, 0),
	}
	if modelName == "" {
		summary.Reason = "模型名称不能为空"
		return summary, nil
	}

	channels, err := model.GetEnabledChannelsForModel(modelName)
	if err != nil {
		return nil, fmt.Errorf("query enabled model channels: %w", err)
	}
	if len(channels) == 0 {
		summary.Reason = "该模型没有可路由的启用渠道"
		return summary, nil
	}

	type compatibleCandidate struct {
		entry      TaskBillingChannelCapability
		capability *channel.TaskBillingCapability
	}
	inspected := make([]compatibleCandidate, 0, len(channels))
	summary.Applicable = modelSupportsCanonicalTaskBilling(modelName)
	for _, upstreamChannel := range channels {
		entry, capability := inspectTaskBillingChannel(modelName, upstreamChannel)
		inspected = append(inspected, compatibleCandidate{entry: entry, capability: capability})
		if isDedicatedVideoTaskChannel(entry.ChannelType) {
			summary.Applicable = true
		}
	}

	candidates := make([]compatibleCandidate, 0, len(inspected))
	for _, candidate := range inspected {
		if !summary.Applicable && !isDedicatedVideoTaskChannel(candidate.entry.ChannelType) {
			// A generic task adaptor may expose profile-derived capabilities even
			// for a non-video public model. Do not let that fallback turn an
			// ordinary chat route into a video billing route.
			continue
		}
		if candidate.capability == nil {
			if !summary.Applicable {
				// Generic chat/image channels do not participate in video task
				// billing. They should not make every expression-priced model
				// display a video-specific warning.
				continue
			}
			summary.IncompatibleChannels = append(summary.IncompatibleChannels, candidate.entry)
			continue
		}
		candidates = append(candidates, candidate)
	}

	capabilities := make([]*channel.TaskBillingCapability, 0, len(candidates))
	for _, candidate := range candidates {
		capabilities = append(capabilities, candidate.capability)
	}
	merged, mergeErr := mergeTaskBillingCapabilities(capabilities)
	if mergeErr != nil {
		for _, candidate := range candidates {
			entry := candidate.entry
			entry.Incompatibility = "规范计费 schema 无法安全合并: " + mergeErr.Error()
			summary.IncompatibleChannels = append(summary.IncompatibleChannels, entry)
		}
	} else if merged != nil {
		summary.SchemaVersion = merged.SchemaVersion
		summary.Fields = cloneTaskBillingFields(merged.Fields)
		for _, candidate := range candidates {
			summary.CompatibleChannels = append(summary.CompatibleChannels, candidate.entry)
		}
	}

	summary.Compatible = merged != nil && len(summary.IncompatibleChannels) == 0
	if !summary.Compatible && summary.Reason == "" {
		if !summary.Applicable {
			summary.Reason = "该模型不是视频任务模型"
		} else {
			summary.Reason = "存在未提供或不兼容规范计费 schema 的启用渠道"
		}
	}
	return summary, nil
}

func modelSupportsCanonicalTaskBilling(modelName string) bool {
	for _, endpointType := range model.GetModelSupportEndpointTypes(modelName) {
		if endpointType == constant.EndpointTypeOpenAIVideo {
			return true
		}
	}
	return false
}

func isDedicatedVideoTaskChannel(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeVidu,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeSora,
		constant.ChannelTypeJimengDimensio,
		constant.ChannelTypeXingheVideo,
		constant.ChannelTypeAGGC,
		constant.ChannelTypeYobox,
		constant.ChannelTypeTencentVOD,
		constant.ChannelTypeAxmgc,
		constant.ChannelTypeSeventhFrame,
		constant.ChannelTypeYoboxCorp,
		constant.ChannelTypeShishi:
		return true
	default:
		return false
	}
}

// CanonicalBillingFields adapts a channel contract to the expression package
// without creating a package cycle between relay/channel and billingexpr.
func CanonicalBillingFields(capability *channel.TaskBillingCapability) []billingexpr.CanonicalBillingField {
	if capability == nil {
		return nil
	}
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{
			Path:       field.Path,
			Type:       field.Type,
			Required:   field.Required,
			EnumValues: append([]string(nil), field.EnumValues...),
		})
	}
	return fields
}

func inspectTaskBillingChannel(modelName string, upstreamChannel *model.Channel) (TaskBillingChannelCapability, *channel.TaskBillingCapability) {
	entry := TaskBillingChannelCapability{}
	if upstreamChannel == nil {
		entry.Incompatibility = "渠道不存在"
		return entry, nil
	}
	entry.ChannelID = upstreamChannel.Id
	entry.ChannelName = upstreamChannel.Name
	entry.ChannelType = upstreamChannel.Type

	resolution, err := common.ResolveModelMapping(upstreamChannel.GetModelMapping(), modelName)
	if err != nil {
		entry.Incompatibility = "模型映射无效: " + err.Error()
		return entry, nil
	}
	entry.UpstreamModel = resolution.Model
	apiType, _ := common.ChannelType2APIType(upstreamChannel.Type)
	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       upstreamChannel.Type,
			ChannelId:         upstreamChannel.Id,
			ChannelBaseUrl:    upstreamChannel.GetBaseURL(),
			ApiType:           apiType,
			UpstreamModelName: resolution.Model,
			IsModelMapped:     resolution.Mapped,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(upstreamChannel.Type)))
	if adaptor == nil {
		entry.Incompatibility = "渠道不支持异步任务规范计费"
		return entry, nil
	}
	adaptor.Init(info)
	if _, ok := adaptor.(channel.TaskBillingInputProvider); !ok {
		entry.Incompatibility = "渠道声明了规范计费 schema，但不能生成规范计费输入"
		return entry, nil
	}
	capability := resolveTaskBillingCapability(adaptor, info)
	if capability == nil {
		entry.Incompatibility = "该渠道映射模型未声明规范计费 schema"
		return entry, nil
	}
	if err := billingexpr.ValidateCanonicalBillingSchema(CanonicalBillingFields(capability)); err != nil {
		entry.Incompatibility = "规范计费 schema 无效: " + err.Error()
		return entry, nil
	}
	entry.SchemaVersion = capability.SchemaVersion
	return entry, capability
}

// resolveTaskBillingCapability first checks a model-specific profile and then
// falls back to a channel-level conservative schema. The fallback is useful
// for custom upstream aliases whose wire semantics are already handled by the
// adaptor but whose name is not present in a static profile table.
func resolveTaskBillingCapability(adaptor channel.TaskAdaptor, info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	if adaptor == nil {
		return nil
	}
	var capability *channel.TaskBillingCapability
	if provider, ok := adaptor.(channel.TaskBillingCapabilityProvider); ok {
		capability = provider.GetTaskBillingCapability(info)
		if capability == nil && info != nil {
			// Billing is configured against the public model name. A channel
			// mapping may use any transport alias, so retry the provider profile
			// with the public name before considering a generic fallback.
			publicModel := strings.TrimSpace(info.OriginModelName)
			upstreamModel := ""
			if info.ChannelMeta != nil {
				upstreamModel = strings.TrimSpace(info.ChannelMeta.UpstreamModelName)
			}
			if publicModel != "" && publicModel != upstreamModel {
				capability = provider.GetTaskBillingCapability(cloneTaskBillingCapabilityInfo(info, publicModel))
			}
		}
	}
	if capability == nil {
		if provider, ok := adaptor.(channel.TaskBillingDefaultCapabilityProvider); ok {
			capability = provider.GetDefaultTaskBillingCapability()
		}
	}
	if capability == nil {
		if provider, ok := adaptor.(channel.TaskBillingProfileFallbackProvider); ok && provider.SupportsTaskBillingProfileFallback() {
			capability = deriveTaskBillingCapabilityFromProfiles(adaptor, info)
		}
	}
	return normalizeTaskBillingCapability(capability)
}

// deriveTaskBillingCapabilityFromProfiles builds a conservative fallback for
// adaptors whose known models share a finite set of canonical dimensions but
// whose newly mapped upstream alias is not in the profile table. Every merged
// field becomes optional because the unknown model's defaults are not known.
func deriveTaskBillingCapabilityFromProfiles(adaptor channel.TaskAdaptor, info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	provider, ok := adaptor.(channel.TaskBillingCapabilityProvider)
	if !ok || adaptor == nil {
		return nil
	}

	models := adaptor.GetModelList()
	profiles := make([]*channel.TaskBillingCapability, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		if _, exists := seen[modelName]; exists {
			continue
		}
		seen[modelName] = struct{}{}

		profileInfo := cloneTaskBillingCapabilityInfo(info, modelName)
		if capability := normalizeTaskBillingCapability(provider.GetTaskBillingCapability(profileInfo)); capability != nil {
			profiles = append(profiles, capability)
		}
	}
	if len(profiles) == 0 {
		return nil
	}

	merged, err := mergeTaskBillingCapabilities(profiles)
	if err != nil || merged == nil {
		return nil
	}
	for index := range merged.Fields {
		merged.Fields[index].Required = false
	}
	merged.SchemaVersion = mergedTaskBillingSchemaVersion(merged.Fields)
	return merged
}

func cloneTaskBillingCapabilityInfo(info *relaycommon.RelayInfo, modelName string) *relaycommon.RelayInfo {
	clone := &relaycommon.RelayInfo{OriginModelName: modelName}
	if info == nil {
		clone.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: modelName}
		return clone
	}
	*clone = *info
	clone.OriginModelName = modelName
	if info.ChannelMeta != nil {
		meta := *info.ChannelMeta
		meta.UpstreamModelName = modelName
		clone.ChannelMeta = &meta
	} else {
		clone.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: modelName}
	}
	return clone
}

func normalizeTaskBillingCapability(capability *channel.TaskBillingCapability) *channel.TaskBillingCapability {
	if capability == nil {
		return nil
	}
	result := &channel.TaskBillingCapability{
		SchemaVersion: strings.TrimSpace(capability.SchemaVersion),
		Fields:        cloneTaskBillingFields(capability.Fields),
	}
	if result.SchemaVersion == "" {
		return nil
	}
	for index := range result.Fields {
		field := &result.Fields[index]
		field.Path = strings.TrimSpace(field.Path)
		field.Type = strings.ToLower(strings.TrimSpace(field.Type))
		for valueIndex := range field.EnumValues {
			field.EnumValues[valueIndex] = strings.TrimSpace(field.EnumValues[valueIndex])
		}
		sort.Strings(field.EnumValues)
	}
	sort.Slice(result.Fields, func(left, right int) bool {
		return result.Fields[left].Path < result.Fields[right].Path
	})
	return result
}

func cloneTaskBillingFields(fields []channel.TaskBillingField) []channel.TaskBillingField {
	result := make([]channel.TaskBillingField, 0, len(fields))
	for _, field := range fields {
		result = append(result, channel.TaskBillingField{
			Path:       field.Path,
			Type:       field.Type,
			Required:   field.Required,
			EnumValues: append([]string(nil), field.EnumValues...),
		})
	}
	return result
}

func mergeTaskBillingCapabilities(capabilities []*channel.TaskBillingCapability) (*channel.TaskBillingCapability, error) {
	normalized := make([]*channel.TaskBillingCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = normalizeTaskBillingCapability(capability)
		if capability == nil {
			return nil, fmt.Errorf("empty canonical billing capability")
		}
		if err := billingexpr.ValidateCanonicalBillingSchema(CanonicalBillingFields(capability)); err != nil {
			return nil, err
		}
		normalized = append(normalized, capability)
	}
	if len(normalized) == 0 {
		return nil, nil
	}

	allIdentical := true
	for index := 1; index < len(normalized); index++ {
		if !sameTaskBillingCapability(normalized[0], normalized[index]) {
			allIdentical = false
			break
		}
	}
	if allIdentical {
		return &channel.TaskBillingCapability{
			SchemaVersion: normalized[0].SchemaVersion,
			Fields:        cloneTaskBillingFields(normalized[0].Fields),
		}, nil
	}

	type mergedField struct {
		field          channel.TaskBillingField
		presentCount   int
		requiredByAll  bool
		openEnumValues bool
		enumValues     map[string]struct{}
	}
	mergedFields := make(map[string]*mergedField)
	for _, capability := range normalized {
		for _, field := range capability.Fields {
			current, exists := mergedFields[field.Path]
			if !exists {
				current = &mergedField{
					field: channel.TaskBillingField{
						Path: field.Path,
						Type: field.Type,
					},
					requiredByAll: field.Required,
					enumValues:    make(map[string]struct{}, len(field.EnumValues)),
				}
				mergedFields[field.Path] = current
			} else if current.field.Type != field.Type {
				return nil, fmt.Errorf("field %q has conflicting types %q and %q", field.Path, current.field.Type, field.Type)
			} else {
				current.requiredByAll = current.requiredByAll && field.Required
			}
			current.presentCount++
			if len(field.EnumValues) == 0 {
				current.openEnumValues = true
			}
			for _, value := range field.EnumValues {
				current.enumValues[value] = struct{}{}
			}
		}
	}

	fields := make([]channel.TaskBillingField, 0, len(mergedFields))
	for _, current := range mergedFields {
		field := current.field
		field.Required = current.presentCount == len(normalized) && current.requiredByAll
		if !current.openEnumValues {
			field.EnumValues = make([]string, 0, len(current.enumValues))
			for value := range current.enumValues {
				field.EnumValues = append(field.EnumValues, value)
			}
			sort.Strings(field.EnumValues)
		}
		fields = append(fields, field)
	}
	sort.Slice(fields, func(left, right int) bool {
		return fields[left].Path < fields[right].Path
	})
	merged := &channel.TaskBillingCapability{
		SchemaVersion: mergedTaskBillingSchemaVersion(fields),
		Fields:        fields,
	}
	if err := billingexpr.ValidateCanonicalBillingSchema(CanonicalBillingFields(merged)); err != nil {
		return nil, err
	}
	return merged, nil
}

func mergedTaskBillingSchemaVersion(fields []channel.TaskBillingField) string {
	var contract strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&contract, "%s\x00%s\x00%t\x00", field.Path, field.Type, field.Required)
		for _, value := range field.EnumValues {
			contract.WriteString(value)
			contract.WriteByte(0)
		}
		contract.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(contract.String()))
	return fmt.Sprintf("task.canonical-merged.%x.v1", digest[:16])
}

func taskBillingCapabilityFitsModelSchema(provider, modelCapability *channel.TaskBillingCapability) error {
	provider = normalizeTaskBillingCapability(provider)
	modelCapability = normalizeTaskBillingCapability(modelCapability)
	if provider == nil || modelCapability == nil {
		return fmt.Errorf("canonical billing capability is empty")
	}
	modelFields := make(map[string]channel.TaskBillingField, len(modelCapability.Fields))
	for _, field := range modelCapability.Fields {
		modelFields[field.Path] = field
	}
	providerFields := make(map[string]channel.TaskBillingField, len(provider.Fields))
	for _, field := range provider.Fields {
		providerFields[field.Path] = field
		modelField, exists := modelFields[field.Path]
		if !exists {
			return fmt.Errorf("provider declares field %q outside the model schema", field.Path)
		}
		if modelField.Type != field.Type {
			return fmt.Errorf("field %q has provider type %q but model type %q", field.Path, field.Type, modelField.Type)
		}
		if len(modelField.EnumValues) == 0 {
			continue
		}
		if len(field.EnumValues) == 0 {
			return fmt.Errorf("provider field %q is open-ended but the model schema is finite", field.Path)
		}
		allowedValues := make(map[string]struct{}, len(modelField.EnumValues))
		for _, value := range modelField.EnumValues {
			allowedValues[value] = struct{}{}
		}
		for _, value := range field.EnumValues {
			if _, exists := allowedValues[value]; !exists {
				return fmt.Errorf("provider field %q value %q is outside the model schema", field.Path, value)
			}
		}
	}
	for _, field := range modelCapability.Fields {
		providerField, exists := providerFields[field.Path]
		if field.Required && (!exists || !providerField.Required) {
			return fmt.Errorf("model-required field %q is not required by the provider", field.Path)
		}
	}
	return nil
}

func sameTaskBillingCapability(left, right *channel.TaskBillingCapability) bool {
	left = normalizeTaskBillingCapability(left)
	right = normalizeTaskBillingCapability(right)
	if left == nil || right == nil || left.SchemaVersion != right.SchemaVersion || len(left.Fields) != len(right.Fields) {
		return false
	}
	for index := range left.Fields {
		leftField := left.Fields[index]
		rightField := right.Fields[index]
		if leftField.Path != rightField.Path || leftField.Type != rightField.Type || leftField.Required != rightField.Required || len(leftField.EnumValues) != len(rightField.EnumValues) {
			return false
		}
		for valueIndex := range leftField.EnumValues {
			if leftField.EnumValues[valueIndex] != rightField.EnumValues[valueIndex] {
				return false
			}
		}
	}
	return true
}
