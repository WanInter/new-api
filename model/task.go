package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
	"gorm.io/gorm"
)

type TaskStatus string

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart   TaskStatus = "NOT_START"
	TaskStatusSubmitted             = "SUBMITTED"
	TaskStatusQueued                = "QUEUED"
	TaskStatusInProgress            = "IN_PROGRESS"
	TaskStatusFailure               = "FAILURE"
	TaskStatusSuccess               = "SUCCESS"
	TaskStatusUnknown               = "UNKNOWN"
)

type Task struct {
	ID         int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt  int64                 `json:"created_at" gorm:"index"`
	UpdatedAt  int64                 `json:"updated_at"`
	TaskID     string                `json:"task_id" gorm:"type:varchar(191);index"` // 第三方id，不一定有/ song id\ Task id
	Platform   constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId     int                   `json:"user_id" gorm:"index"`
	Group      string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId  int                   `json:"channel_id" gorm:"index"`
	Quota      int                   `json:"quota"`
	Action     string                `json:"action" gorm:"type:varchar(40);index"` // 任务类型, song, lyrics, description-mode
	Status     TaskStatus            `json:"status" gorm:"type:varchar(20);index"` // 任务状态
	FailReason string                `json:"fail_reason"`
	SubmitTime int64                 `json:"submit_time" gorm:"index"`
	StartTime  int64                 `json:"start_time" gorm:"index"`
	FinishTime int64                 `json:"finish_time" gorm:"index"`
	Progress   string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties Properties            `json:"properties" gorm:"type:json"`
	Username   string                `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
	// Execution lease fields coordinate one-shot local task execution across instances.
	// A future lease deadline with an empty owner is the retry-not-before time.
	IsLocalImageTask    bool   `json:"-" gorm:"index;index:idx_task_local_image_schedule,priority:1;default:false"`
	ExecutionLeaseOwner string `json:"-" gorm:"type:varchar(191)"`
	ExecutionLeaseUntil int64  `json:"-" gorm:"index:idx_task_local_image_schedule,priority:2;default:0"`
	ExecutionAttempts   int    `json:"-" gorm:"default:0"`
	LastPollTime        int64  `json:"-" gorm:"index;default:0"`
	NextPollTime        int64  `json:"-" gorm:"index;default:0"`
	PollErrorCount      int    `json:"-" gorm:"default:0"`
	LastPollError       string `json:"-" gorm:"type:text"`
	BillingStatus       string `json:"-" gorm:"type:varchar(20);index;default:''"`
	BillingDelta        int    `json:"-" gorm:"default:0"`
	BillingFinalQuota   int    `json:"-" gorm:"default:0"`
	BillingReason       string `json:"-" gorm:"type:text"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		*m = Properties{}
		return nil
	}
	return common.Unmarshal(bytesValue, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	return common.Marshal(m)
}

type TaskPrivateData struct {
	Key            string                     `json:"key,omitempty"`
	UpstreamTaskID string                     `json:"upstream_task_id,omitempty"` // 上游真实 task ID
	ResultURL      string                     `json:"result_url,omitempty"`       // 任务成功后的结果 URL（视频地址等）
	LocalImageTask *LocalImageTaskPrivateData `json:"local_image_task,omitempty"`
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource  string              `json:"billing_source,omitempty"`  // "wallet" 或 "subscription"
	SubscriptionId int                 `json:"subscription_id,omitempty"` // 订阅 ID，用于订阅退款
	TokenId        int                 `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	BillingContext *TaskBillingContext `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
}

type LocalImageTaskPrivateData struct {
	Request     json.RawMessage `json:"request,omitempty"`
	ChannelType int             `json:"channel_type,omitempty"`
	APIType     int             `json:"api_type,omitempty"`
	BaseURL     string          `json:"base_url,omitempty"`
	APIVersion  string          `json:"api_version,omitempty"`
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice        float64            `json:"model_price,omitempty"`         // 模型单价
	GroupRatio        float64            `json:"group_ratio,omitempty"`         // 分组倍率
	ModelRatio        float64            `json:"model_ratio,omitempty"`         // 模型倍率
	OtherRatios       map[string]float64 `json:"other_ratios,omitempty"`        // 附加倍率（时长、分辨率等）
	OriginModelName   string             `json:"origin_model_name,omitempty"`   // 模型名称，必须为OriginModelName
	PricingRuleId     int                `json:"pricing_rule_id,omitempty"`     // 命中的精确计费规则
	PricingRuleSource string             `json:"pricing_rule_source,omitempty"` // 计费规则来源
	PerCallBilling    bool               `json:"per_call_billing,omitempty"`    // 按次计费：跳过轮询阶段的差额结算
	// Tiered task-billing audit snapshot. These fields deliberately contain
	// only expression metadata and canonical provider-produced values; never
	// persist the original request body, headers, or upstream credentials.
	BillingMode                string                      `json:"billing_mode,omitempty"`
	BillingSchema              string                      `json:"billing_schema,omitempty"`
	BillingExpr                string                      `json:"billing_expr,omitempty"`
	BillingExprHash            string                      `json:"billing_expr_hash,omitempty"`
	BillingExprVersion         int                         `json:"billing_expr_version,omitempty"`
	QuotaPerUnit               float64                     `json:"quota_per_unit,omitempty"`
	EstimatedTier              string                      `json:"estimated_tier,omitempty"`
	MatchedTier                string                      `json:"matched_tier,omitempty"`
	CanonicalBillingInput      json.RawMessage             `json:"canonical_billing_input,omitempty"`
	ActualBillingInput         json.RawMessage             `json:"actual_billing_input,omitempty"`
	CanonicalBillingFieldPaths []string                    `json:"canonical_billing_field_paths,omitempty"`
	CanonicalBillingFields     []TaskBillingCanonicalField `json:"canonical_billing_fields,omitempty"`
	PreConsumedQuota           int                         `json:"pre_consumed_quota,omitempty"`
	FinalQuota                 int                         `json:"final_quota,omitempty"`
}

// TaskBillingCanonicalField is the immutable schema fragment saved with a
// schema-pinned task. Keeping it with the task means asynchronous settlement
// never has to infer semantics from a newer channel configuration.
type TaskBillingCanonicalField struct {
	Path       string   `json:"path"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	EnumValues []string `json:"enum_values,omitempty"`
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// GetResultURL 获取任务结果 URL（视频地址等）
// 新数据存在 PrivateData.ResultURL 中；旧数据回退到 FailReason（历史兼容）
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

func (t *Task) CompletionTime() int64 {
	if t.Status != TaskStatusSuccess && t.Status != TaskStatusFailure {
		return 0
	}
	if t.FinishTime > 0 {
		return t.FinishTime
	}
	return t.UpdatedAt
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		return nil
	}
	return common.Unmarshal(bytesValue, p)
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	if (p == TaskPrivateData{}) {
		return nil, nil
	}
	return common.Marshal(p)
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		if relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeGemini ||
			relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeVertexAi {
			privateData.Key = relayInfo.ChannelMeta.ApiKey
		}
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   relayInfo.ChannelId,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	var tasks []*Task
	err := DB.Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetAllUnFinishSyncTasks(limit int) []*Task {
	var tasks []*Task
	var err error
	// Poll every non-terminal task. Some upstream adapters may report 100% progress
	// before the terminal SUCCESS/FAILURE status is persisted; filtering by progress
	// would leave those tasks stuck forever.
	err = DB.Where("status != ?", TaskStatusFailure).Where("status != ?", TaskStatusSuccess).Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetUnfinishedPollingTasks(now int64, limit int) ([]*Task, error) {
	if limit < 1 {
		return nil, nil
	}
	var tasks []*Task
	err := DB.Where("status != ?", TaskStatusFailure).
		Where("status != ?", TaskStatusSuccess).
		Where("is_local_image_task = ?", false).
		Where("next_poll_time IS NULL OR next_poll_time = 0 OR next_poll_time <= ?", now).
		Limit(limit).
		Order("next_poll_time").
		Order("id").
		Find(&tasks).Error
	return tasks, err
}

func GetPendingLocalImageTasks(now int64, limit int) ([]*Task, error) {
	if limit < 1 {
		return nil, nil
	}
	var tasks []*Task
	err := DB.Where("is_local_image_task = ?", true).
		Where("status != ?", TaskStatusFailure).
		Where("status != ?", TaskStatusSuccess).
		Where("execution_lease_until IS NULL OR execution_lease_until <= ?", now).
		Limit(limit).
		Order("id").
		Find(&tasks).Error
	return tasks, err
}

func GetLegacyLocalImageTaskCandidates(afterID int64, limit int) ([]*Task, error) {
	if limit < 1 {
		return nil, nil
	}
	var tasks []*Task
	err := DB.Where("platform = ?", constant.TaskPlatformImage).
		Where("is_local_image_task = ?", false).
		Where("status != ?", TaskStatusFailure).
		Where("status != ?", TaskStatusSuccess).
		Where("id > ?", afterID).
		Limit(limit).
		Order("id").
		Find(&tasks).Error
	return tasks, err
}

func MarkTaskAsLocalImage(taskID int64) error {
	if taskID <= 0 {
		return errors.New("invalid local image task id")
	}
	return DB.Model(&Task{}).
		Where("id = ?", taskID).
		UpdateColumn("is_local_image_task", true).Error
}

func GetByOnlyTaskId(taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("task_id = ?", taskId).First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (Task *Task) Insert() error {
	Task.IsLocalImageTask = Task.PrivateData.LocalImageTask != nil
	return DB.Create(Task).Error
}

type taskSnapshot struct {
	Status         TaskStatus
	Progress       string
	StartTime      int64
	FinishTime     int64
	FailReason     string
	ResultURL      string
	Data           json.RawMessage
	LastPollTime   int64
	NextPollTime   int64
	PollErrorCount int
	LastPollError  string
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		s.LastPollTime == other.LastPollTime &&
		s.NextPollTime == other.NextPollTime &&
		s.PollErrorCount == other.PollErrorCount &&
		s.LastPollError == other.LastPollError &&
		bytes.Equal(s.Data, other.Data)
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:         t.Status,
		Progress:       t.Progress,
		StartTime:      t.StartTime,
		FinishTime:     t.FinishTime,
		FailReason:     t.FailReason,
		ResultURL:      t.PrivateData.ResultURL,
		Data:           t.Data,
		LastPollTime:   t.LastPollTime,
		NextPollTime:   t.NextPollTime,
		PollErrorCount: t.PollErrorCount,
		LastPollError:  t.LastPollError,
	}
}

func (Task *Task) Update() error {
	var err error
	err = DB.Save(Task).Error
	return err
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	result := DB.Model(t).Where("status = ?", fromStatus).Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (t *Task) UpdateWithStatusAndLease(fromStatus TaskStatus, leaseOwner string) (bool, error) {
	if leaseOwner == "" {
		return false, errors.New("task execution lease owner is required")
	}
	result := DB.Model(t).
		Where("status = ?", fromStatus).
		Where("execution_lease_owner = ?", leaseOwner).
		Select("*").
		Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func TryClaimTaskExecutionLease(taskID int64, owner string, now, leaseUntil int64, maxAttempts int) (bool, error) {
	if taskID <= 0 || owner == "" || leaseUntil <= now || maxAttempts < 1 {
		return false, errors.New("invalid task execution lease claim")
	}
	result := DB.Model(&Task{}).
		Where("id = ?", taskID).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("status != ? AND status != ?", TaskStatusFailure, TaskStatusSuccess).
		Where("execution_attempts < ?", maxAttempts).
		Where("execution_lease_until IS NULL OR execution_lease_until <= ?", now).
		UpdateColumns(map[string]any{
			"execution_lease_owner": owner,
			"execution_lease_until": leaseUntil,
			"execution_attempts":    gorm.Expr("execution_attempts + ?", 1),
			"status":                TaskStatusInProgress,
			"progress":              "30%",
			"start_time":            now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func RenewTaskExecutionLease(taskID int64, owner string, leaseUntil int64) (bool, error) {
	if taskID <= 0 || owner == "" || leaseUntil <= 0 {
		return false, errors.New("invalid task execution lease renewal")
	}
	result := DB.Model(&Task{}).
		Where("id = ?", taskID).
		Where("execution_lease_owner = ?", owner).
		Where("status != ? AND status != ?", TaskStatusFailure, TaskStatusSuccess).
		UpdateColumn("execution_lease_until", leaseUntil)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func IsTaskExecutionLeaseOwner(taskID int64, owner string, status TaskStatus) (bool, error) {
	if taskID <= 0 || owner == "" {
		return false, errors.New("invalid task execution lease check")
	}
	var count int64
	err := DB.Model(&Task{}).
		Where("id = ?", taskID).
		Where("status = ?", status).
		Where("execution_lease_owner = ?", owner).
		Count(&count).Error
	return count > 0, err
}

func RetryTaskExecutionLease(taskID int64, owner string, retryAt int64) (bool, error) {
	if taskID <= 0 || owner == "" || retryAt <= 0 {
		return false, errors.New("invalid task execution lease retry")
	}
	result := DB.Model(&Task{}).
		Where("id = ?", taskID).
		Where("status = ?", TaskStatusInProgress).
		Where("execution_lease_owner = ?", owner).
		UpdateColumns(map[string]any{
			"execution_lease_owner": "",
			"execution_lease_until": retryAt,
			"status":                TaskStatusQueued,
			"progress":              "20%",
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func ReleaseTaskExecutionLease(taskID int64, owner string) (bool, error) {
	if taskID <= 0 || owner == "" {
		return false, errors.New("invalid task execution lease release")
	}
	result := DB.Model(&Task{}).
		Where("id = ?", taskID).
		Where("execution_lease_owner = ?", owner).
		UpdateColumns(map[string]any{
			"execution_lease_owner": "",
			"execution_lease_until": 0,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// TaskBulkUpdate performs an unconditional bulk UPDATE by upstream task_id strings.
// Same caveats as TaskBulkUpdateByID — no CAS guard.
func TaskBulkUpdate(taskIds []string, params map[string]any) error {
	if len(taskIds) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("task_id in (?)", taskIds).
		Updates(params).Error
}

// TaskBulkUpdateByID performs an unconditional bulk UPDATE by primary key IDs.
// WARNING: This function has NO CAS (Compare-And-Swap) guard — it will overwrite
// any concurrent status changes. DO NOT use in billing/quota lifecycle flows
// (e.g., timeout, success, failure transitions that trigger refunds or settlements).
// For status transitions that involve billing, use Task.UpdateWithStatus() instead.
func TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id in (?)", ids).
		Updates(params).Error
}

type TaskQuotaUsage struct {
	Mode  string  `json:"mode"`
	Count float64 `json:"count"`
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	openAIVideo.CompletedAt = t.CompletionTime()
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}
