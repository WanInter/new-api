package relay

import (
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

// TaskBillingCapabilitySummary describes whether every currently enabled route
// for a public model agrees on one canonical schema.
type TaskBillingCapabilitySummary struct {
	Model                string                         `json:"model"`
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
// safe only when all enabled routes expose the same canonical schema.
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

	var baseline *channel.TaskBillingCapability
	for _, upstreamChannel := range channels {
		entry, capability := inspectTaskBillingChannel(modelName, upstreamChannel)
		if capability == nil {
			summary.IncompatibleChannels = append(summary.IncompatibleChannels, entry)
			continue
		}
		if baseline == nil {
			baseline = capability
			summary.SchemaVersion = capability.SchemaVersion
			summary.Fields = cloneTaskBillingFields(capability.Fields)
			summary.CompatibleChannels = append(summary.CompatibleChannels, entry)
			continue
		}
		if sameTaskBillingCapability(baseline, capability) {
			summary.CompatibleChannels = append(summary.CompatibleChannels, entry)
			continue
		}
		entry.Incompatibility = fmt.Sprintf("规范计费 schema 与 %s 不一致", baseline.SchemaVersion)
		summary.IncompatibleChannels = append(summary.IncompatibleChannels, entry)
	}

	summary.Compatible = baseline != nil && len(summary.IncompatibleChannels) == 0
	if !summary.Compatible && summary.Reason == "" {
		summary.Reason = "存在未提供或不兼容规范计费 schema 的启用渠道"
	}
	return summary, nil
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
	provider, ok := adaptor.(channel.TaskBillingCapabilityProvider)
	if !ok {
		entry.Incompatibility = "渠道未提供规范计费输入"
		return entry, nil
	}
	if _, ok := adaptor.(channel.TaskBillingInputProvider); !ok {
		entry.Incompatibility = "渠道声明了规范计费 schema，但不能生成规范计费输入"
		return entry, nil
	}
	capability := normalizeTaskBillingCapability(provider.GetTaskBillingCapability(info))
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
