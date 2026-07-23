package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费：显式模型价格是固定按次价格，不应展示 seconds/size 等动态参数。
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) || info.PriceData.UsePrice {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.PriceData.GroupRatioInfo.PricingRuleId != 0 {
		other["pricing_rule_id"] = info.PriceData.GroupRatioInfo.PricingRuleId
		other["pricing_rule_source"] = info.PriceData.GroupRatioInfo.PricingRuleSource
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	appendTaskTieredBillingAudit(other, info)
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// appendTaskTieredBillingAudit records the frozen task billing contract at
// submission time. Request input is only exposed when it is the schema-pinned
// canonical object, never the client's original body or headers.
func appendTaskTieredBillingAudit(other map[string]interface{}, info *relaycommon.RelayInfo) {
	if other == nil || info == nil || info.TieredBillingSnapshot == nil {
		return
	}
	snapshot := info.TieredBillingSnapshot
	InjectTieredBillingInfo(other, info, &billingexpr.TieredResult{MatchedTier: snapshot.EstimatedTier})
	other["estimated_quota"] = snapshot.EstimatedQuotaAfterGroup
	other["pre_consumed_quota"] = info.FinalPreConsumedQuota
	other["final_quota"] = info.PriceData.Quota
	if snapshot.BillingSchema == "" || info.BillingRequestInput == nil {
		return
	}
	if input := canonicalBillingInputMap(info.BillingRequestInput.Body); input != nil {
		other["canonical_billing_input"] = input
	}
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if task == nil {
		return other
	}
	appendTaskBillingContextAudit(other, task.PrivateData.BillingContext)
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if bc.PricingRuleId != 0 {
			other["pricing_rule_id"] = bc.PricingRuleId
			other["pricing_rule_source"] = bc.PricingRuleSource
		}
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

func appendTaskBillingContextAudit(other map[string]interface{}, billingContext *model.TaskBillingContext) {
	if other == nil || billingContext == nil || billingContext.BillingMode == "" {
		return
	}
	other["billing_mode"] = billingContext.BillingMode
	if billingContext.BillingSchema != "" {
		other["billing_schema"] = billingContext.BillingSchema
	}
	if billingContext.BillingExprHash != "" {
		other["expr_hash"] = billingContext.BillingExprHash
	}
	if billingContext.BillingExprVersion != 0 {
		other["expr_version"] = billingContext.BillingExprVersion
	}
	if billingContext.EstimatedTier != "" {
		other["estimated_tier"] = billingContext.EstimatedTier
	}
	if billingContext.MatchedTier != "" {
		other["matched_tier"] = billingContext.MatchedTier
	}
	if input := canonicalBillingInputMap(billingContext.CanonicalBillingInput); input != nil {
		other["canonical_billing_input"] = input
	}
	if input := canonicalBillingInputMap(billingContext.ActualBillingInput); input != nil {
		other["actual_billing_input"] = input
	}
	other["pre_consumed_quota"] = billingContext.PreConsumedQuota
	other["final_quota"] = billingContext.FinalQuota
}

func canonicalBillingInputMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var root map[string]any
	if err := common.Unmarshal(raw, &root); err != nil || len(root) != 1 {
		return nil
	}
	billing, ok := root["billing"].(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]any, len(billing))
	for key, value := range billing {
		if strings.TrimSpace(key) == "" || !isSafeCanonicalBillingValue(value) {
			return nil
		}
		result[key] = value
	}
	return map[string]any{"billing": result}
}

func isSafeCanonicalBillingValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case bool:
		return true
	case float64:
		return !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case float32:
		return !math.IsNaN(float64(typed)) && !math.IsInf(float64(typed), 0)
	case int:
		return true
	case int8:
		return true
	case int16:
		return true
	case int32:
		return true
	case int64:
		return true
	case uint:
		return true
	case uint8:
		return true
	case uint16:
		return true
	case uint32:
		return true
	case uint64:
		return true
	default:
		return false
	}
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

func setTaskBillingIntent(task *model.Task, finalQuota int, reason string) {
	if finalQuota < 0 {
		finalQuota = 0
	}
	task.BillingStatus = model.TaskBillingStatusPending
	task.BillingFinalQuota = finalQuota
	task.BillingDelta = finalQuota - task.Quota
	task.BillingReason = strings.TrimSpace(reason)
	if billingContext := task.PrivateData.BillingContext; billingContext != nil {
		billingContext.FinalQuota = finalQuota
	}
}

func prepareTaskBillingIntent(adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	finalQuota := task.Quota
	reason := "任务完成，保持预扣额度"
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		setTaskBillingIntent(task, finalQuota, reason)
		return
	}
	if bc := task.PrivateData.BillingContext; bc != nil && bc.BillingMode == "tiered_expr" && bc.BillingSchema != "" {
		if actualQuota, settlementReason, ok := calculateTaskQuotaByCanonicalBilling(task, taskResult); ok {
			setTaskBillingIntent(task, actualQuota, settlementReason)
			return
		}
		// A schema-pinned task must never fall back to a provider ratio helper:
		// that would reintroduce provider-specific parsing after canonical pricing
		// has been frozen. Preserve its pre-charge if a trusted actual input is
		// unavailable or invalid.
		setTaskBillingIntent(task, finalQuota, reason)
		return
	}
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		setTaskBillingIntent(task, actualQuota, "adaptor计费调整")
		return
	}
	if taskResult.TotalTokens > 0 {
		if actualQuota, tokenReason, ok := calculateTaskQuotaByTokens(task, taskResult.TotalTokens); ok {
			setTaskBillingIntent(task, actualQuota, tokenReason)
			return
		}
	}
	setTaskBillingIntent(task, finalQuota, reason)
}

func calculateTaskQuotaByCanonicalBilling(task *model.Task, taskResult *relaycommon.TaskInfo) (int, string, bool) {
	if task == nil || taskResult == nil {
		return 0, "", false
	}
	billingContext := task.PrivateData.BillingContext
	if billingContext == nil || billingContext.BillingMode != "tiered_expr" || billingContext.BillingSchema == "" || strings.TrimSpace(billingContext.BillingExpr) == "" {
		return 0, "", false
	}
	input, actualInput, err := mergeTaskCanonicalBillingInput(billingContext, taskResult.ActualBillingInput)
	if err != nil {
		return 0, "", false
	}
	quotaPerUnit := billingContext.QuotaPerUnit
	if quotaPerUnit <= 0 {
		quotaPerUnit = common.QuotaPerUnit
	}
	expressionHash := billingContext.BillingExprHash
	if expressionHash == "" {
		expressionHash = billingexpr.ExprHashString(billingContext.BillingExpr)
	}
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode:   billingContext.BillingMode,
		BillingSchema: billingContext.BillingSchema,
		ModelName:     billingContext.OriginModelName,
		ExprString:    billingContext.BillingExpr,
		ExprHash:      expressionHash,
		GroupRatio:    billingContext.GroupRatio,
		EstimatedTier: billingContext.EstimatedTier,
		QuotaPerUnit:  quotaPerUnit,
		ExprVersion:   billingContext.BillingExprVersion,
	}
	result, err := billingexpr.ComputeTieredQuotaWithRequest(snapshot, billingexpr.TokenParams{}, billingexpr.RequestInput{Body: input})
	if err != nil || result.ActualQuotaAfterGroup < 0 {
		return 0, "", false
	}
	billingContext.MatchedTier = result.MatchedTier
	billingContext.FinalQuota = result.ActualQuotaAfterGroup
	if actualInput != nil {
		billingContext.ActualBillingInput = actualInput
	}
	reason := fmt.Sprintf("规范计费结算：schema=%s, tier=%s", billingContext.BillingSchema, result.MatchedTier)
	return result.ActualQuotaAfterGroup, reason, true
}
func mergeTaskCanonicalBillingInput(billingContext *model.TaskBillingContext, actual map[string]any) ([]byte, []byte, error) {
	base := canonicalBillingInputMap(billingContext.CanonicalBillingInput)
	if base == nil {
		return nil, nil, fmt.Errorf("missing canonical billing input")
	}
	billing := base["billing"].(map[string]any)
	if len(actual) == 0 {
		body, err := common.Marshal(base)
		return body, nil, err
	}
	actualBody, err := common.Marshal(actual)
	if err != nil {
		return nil, nil, err
	}
	actualCanonical := canonicalBillingInputMap(actualBody)
	if actualCanonical == nil {
		return nil, nil, fmt.Errorf("invalid actual canonical billing input")
	}
	allowedFields := make(map[string]struct{}, len(billingContext.CanonicalBillingFields)+len(billingContext.CanonicalBillingFieldPaths))
	for _, field := range billingContext.CanonicalBillingFields {
		path := strings.TrimSpace(field.Path)
		if strings.HasPrefix(path, "billing.") {
			allowedFields[strings.TrimPrefix(path, "billing.")] = struct{}{}
		}
	}
	for _, path := range billingContext.CanonicalBillingFieldPaths {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "billing.") {
			allowedFields[strings.TrimPrefix(path, "billing.")] = struct{}{}
		}
	}
	if len(allowedFields) == 0 {
		for key := range billing {
			allowedFields[key] = struct{}{}
		}
	}
	actualBilling := actualCanonical["billing"].(map[string]any)
	for key, value := range actualBilling {
		if _, ok := allowedFields[key]; !ok {
			return nil, nil, fmt.Errorf("actual canonical billing input contains undeclared field %q", key)
		}
		billing[key] = value
	}
	mergedBody, err := common.Marshal(base)
	if err != nil {
		return nil, nil, err
	}
	if err := validateTaskCanonicalBillingInput(billingContext, mergedBody); err != nil {
		return nil, nil, err
	}
	return mergedBody, mergedBody, nil
}

func validateTaskCanonicalBillingInput(billingContext *model.TaskBillingContext, body []byte) error {
	if billingContext == nil {
		return fmt.Errorf("missing canonical billing context")
	}
	if len(billingContext.CanonicalBillingFields) == 0 {
		// Tasks created before the field contract was persisted have already had
		// their allowed paths checked by mergeTaskCanonicalBillingInput. Do not
		// infer types or enum values from a live channel configuration.
		return nil
	}
	fields := make([]billingexpr.CanonicalBillingField, 0, len(billingContext.CanonicalBillingFields))
	for _, field := range billingContext.CanonicalBillingFields {
		fields = append(fields, billingexpr.CanonicalBillingField{
			Path:       field.Path,
			Type:       field.Type,
			Required:   field.Required,
			EnumValues: append([]string(nil), field.EnumValues...),
		})
	}
	return billingexpr.ValidateCanonicalBillingInput(body, fields)
}

func processPendingTaskBilling(ctx context.Context, taskID int64) error {
	settlement, applied, err := model.ApplyPendingTaskBilling(taskID)
	if err != nil || !applied {
		return err
	}
	if settlement.Delta == 0 {
		return nil
	}

	logType := model.LogTypeConsume
	logQuota := settlement.Delta
	if settlement.Delta < 0 {
		logType = model.LogTypeRefund
		logQuota = -settlement.Delta
	} else {
		model.UpdateUserUsedQuota(settlement.Task.UserId, settlement.Delta)
		model.UpdateChannelUsedQuota(settlement.Task.ChannelId, settlement.Delta)
	}
	other := taskBillingOther(&settlement.Task)
	other["task_id"] = settlement.Task.TaskID
	other["pre_consumed_quota"] = settlement.PreConsumedQuota
	other["actual_quota"] = settlement.FinalQuota
	other["reason"] = settlement.Reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    settlement.Task.UserId,
		LogType:   logType,
		Content:   settlement.Reason,
		ChannelId: settlement.Task.ChannelId,
		ModelName: taskModelName(&settlement.Task),
		Quota:     logQuota,
		TokenId:   settlement.Task.PrivateData.TokenId,
		Group:     settlement.Task.Group,
		Other:     other,
	})
	logger.LogInfo(ctx, fmt.Sprintf(
		"任务 %s 账务结算完成：delta=%s，最终额度=%s",
		settlement.Task.TaskID,
		logger.LogQuota(settlement.Delta),
		logger.LogQuota(settlement.FinalQuota),
	))
	return nil
}

func settlePendingTaskBillings(ctx context.Context, limit int) {
	tasks, err := model.GetPendingTaskBillings(limit)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("query pending task billings failed: %v", err))
		return
	}
	for _, task := range tasks {
		if err := processPendingTaskBilling(ctx, task.ID); err != nil {
			logger.LogError(ctx, fmt.Sprintf("settle pending billing for task %s failed: %v", task.TaskID, err))
		}
	}
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) {
	quota := task.Quota
	if quota == 0 {
		return
	}

	// 1. 退还资金来源（钱包或订阅）
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 2. 退还令牌额度
	taskAdjustTokenQuota(ctx, task, -quota)

	// 3. 记录日志
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string) {
	if actualQuota <= 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 调整资金来源
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度
	taskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	if billingContext := task.PrivateData.BillingContext; billingContext != nil {
		billingContext.FinalQuota = actualQuota
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
		model.UpdateUserUsedQuotaAndRequestCount(task.UserId, quotaDelta)
		model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	actualQuota, reason, ok := calculateTaskQuotaByTokens(task, totalTokens)
	if !ok {
		return
	}
	RecalculateTaskQuota(ctx, task, actualQuota, reason)
}

func calculateTaskQuotaByTokens(task *model.Task, totalTokens int) (int, string, bool) {
	if totalTokens <= 0 {
		return 0, "", false
	}

	modelName := taskModelName(task)

	// Keep the model ratio selected at submission time. Re-resolving it here
	// would retroactively change an in-flight task after an administrator edits
	// its pricing policy.
	modelRatio := 0.0
	hasRatioSetting := false
	if bc := task.PrivateData.BillingContext; bc != nil && bc.ModelRatio > 0 {
		modelRatio = bc.ModelRatio
		hasRatioSetting = true
	} else {
		modelRatio, hasRatioSetting, _ = ratio_setting.GetModelRatio(modelName)
	}
	if !hasRatioSetting || modelRatio <= 0 {
		return 0, "", false
	}

	// Task billing must keep the ratio selected when the task was submitted.
	// Re-resolving a user/model rule here would retroactively change an
	// in-flight task after an administrator edits its pricing policy.
	var finalGroupRatio float64
	if bc := task.PrivateData.BillingContext; bc != nil {
		finalGroupRatio = bc.GroupRatio
	} else {
		// Tasks created before billing snapshots existed retain the prior lookup
		// semantics for their eventual settlement.
		group := task.Group
		if group == "" {
			user, err := model.GetUserById(task.UserId, false)
			if err == nil {
				group = user.Group
			}
		}
		if group == "" {
			return 0, "", false
		}
		if userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(group, group); ok {
			finalGroupRatio = userGroupRatio
		} else {
			finalGroupRatio = ratio_setting.GetGroupRatio(group)
		}
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier
	actualQuota := int(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	if actualQuota <= 0 {
		return 0, "", false
	}
	return actualQuota, reason, true
}
