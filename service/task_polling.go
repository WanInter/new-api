package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/samber/lo"
	"gorm.io/gorm"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(ctx context.Context, baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

type taskPollingContextResultParser interface {
	ParseTaskResultWithContext(ctx context.Context, body []byte) (*relaycommon.TaskInfo, error)
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// ExecuteLocalImageTaskFunc 由 main 包注入，用于执行本地异步图片任务。
var ExecuteLocalImageTaskFunc func(ctx context.Context, task *model.Task, ch *model.Channel, key string, proxy string) (*http.Response, error)

type channelTaskMap map[int]map[string][]*model.Task

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		task.ExecutionLeaseOwner = ""
		task.ExecutionLeaseUntil = 0
		clearLocalImageTaskRequest(task)
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
			if task.Quota != 0 {
				setTaskBillingIntent(task, 0, reason)
			}
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.BillingStatus == model.TaskBillingStatusPending {
			if err := processPendingTaskBilling(ctx, task.ID); err != nil {
				logger.LogError(ctx, fmt.Sprintf("settle timed out task %s billing: %v", task.TaskID, err))
			}
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollingLoop 主轮询循环，按配置间隔检查已到期的未完成任务。
func TaskPollingLoop() {
	localImageDispatcher := newLocalImageTaskDispatcher(
		constant.LocalImageTaskConcurrency,
		time.Duration(constant.LocalImageTaskLeaseSeconds)*time.Second,
		runLeasedLocalImageTask,
	)
	common.SysLog(fmt.Sprintf(
		"local async image executor started with concurrency %d, lease %ds, attempt timeout %s, max attempts %d",
		localImageDispatcher.Concurrency(),
		int(localImageDispatcher.LeaseDuration()/time.Second),
		localImageDispatcher.AttemptTimeout(),
		localImageDispatcher.MaxAttempts(),
	))
	go localImageDispatcher.Run(context.Background(), localImageDispatchInterval, constant.TaskQueryLimit)
	for {
		startedAt := time.Now()
		common.SysLog("任务进度轮询开始")
		runTaskPollingCycle(context.Background(), localImageDispatcher)
		common.SysLog("任务进度轮询完成")
		remaining := time.Duration(taskPollingIntervalSeconds())*time.Second - time.Since(startedAt)
		if remaining > 0 {
			time.Sleep(remaining)
		}
	}
}

func runTaskPollingCycle(ctx context.Context, localImageDispatcher *localImageTaskDispatcher) {
	settlePendingTaskBillings(ctx, constant.TaskQueryLimit)
	sweepTimedOutTasks(ctx)
	allTasks, err := model.GetUnfinishedPollingTasks(time.Now().Unix(), constant.TaskQueryLimit)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("query unfinished polling tasks failed: %v", err))
		return
	}
	platformTasks := make(map[constant.TaskPlatform][]*model.Task)
	for _, task := range allTasks {
		if isLocalImageExecutionTask(task) {
			if !task.IsLocalImageTask {
				if err := model.MarkTaskAsLocalImage(task.ID); err != nil {
					logger.LogError(ctx, fmt.Sprintf("mark legacy local image task %s failed: %s", task.TaskID, err.Error()))
				} else {
					task.IsLocalImageTask = true
				}
			}
			if localImageDispatcher != nil {
				localImageDispatcher.TryDispatch(task)
			}
			continue
		}
		platformTasks[task.Platform] = append(platformTasks[task.Platform], task)
	}

	var wg sync.WaitGroup
	for platform, tasks := range platformTasks {
		platform, tasks := platform, tasks
		if len(tasks) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			dispatchTaskPlatform(ctx, platform, tasks)
		}()
	}
	wg.Wait()
}

func dispatchTaskPlatform(ctx context.Context, platform constant.TaskPlatform, tasks []*model.Task) {
	taskChannelM := make(map[int][]string)
	taskM := make(channelTaskMap)
	for _, task := range tasks {
		upstreamID := task.GetUpstreamTaskID()
		if upstreamID == "" {
			if _, err := failTaskWithRefund(ctx, task, task.Status, "", "上游任务 ID 为空"); err != nil {
				logger.LogError(ctx, fmt.Sprintf("fail task with empty upstream ID %d: %v", task.ID, err))
			}
			continue
		}
		if taskM[task.ChannelId] == nil {
			taskM[task.ChannelId] = make(map[string][]*model.Task)
		}
		if len(taskM[task.ChannelId][upstreamID]) == 0 {
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		taskM[task.ChannelId][upstreamID] = append(taskM[task.ChannelId][upstreamID], task)
	}
	if len(taskChannelM) > 0 {
		dispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(platform constant.TaskPlatform, taskChannelM map[int][]string, taskM channelTaskMap) {
	dispatchPlatformUpdate(context.Background(), platform, taskChannelM, taskM)
}

func dispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM channelTaskMap) {
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM channelTaskMap) error {
	var errs []error
	for channelId, taskIds := range taskChannelM {
		err := updateSunoTasks(ctx, channelId, taskIds, taskM[channelId])
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string][]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := getTaskPollingChannel(channelId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			reason := fmt.Sprintf("任务所属渠道已不存在，渠道 ID：%d", channelId)
			failTaskMapWithRefund(ctx, taskM, reason)
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		reason := "Suno 任务缺少状态查询适配器"
		failTaskMapWithRefund(ctx, taskM, reason)
		return errors.New(reason)
	}
	proxy := ch.GetSetting().Proxy
	requestCtx, cancel := TaskPollingRequestContext(ctx)
	defer cancel()
	resp, err := adaptor.FetchTask(requestCtx, *ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		return handleTaskMapPollFailure(ctx, taskM, fmt.Errorf("fetch Suno tasks failed: %w", err))
	}
	if resp == nil || resp.Body == nil {
		return handleTaskMapPollFailure(ctx, taskM, errors.New("fetch Suno tasks returned an empty response"))
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return handleTaskMapPollFailure(ctx, taskM, fmt.Errorf("read Suno task response failed: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return handleSunoTaskMapHTTPFailure(ctx, taskM, resp.StatusCode)
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		return handleTaskMapPollFailure(ctx, taskM, fmt.Errorf("parse Suno task response failed: %w", err))
	}
	if !responseItems.IsSuccess() {
		return handleTaskMapPollFailure(ctx, taskM, fmt.Errorf("Suno task query returned an unsuccessful response for channel %d", channelId))
	}

	seen := make(map[string]bool)
	for _, responseItem := range responseItems.Data {
		seen[responseItem.TaskID] = true
		for _, task := range taskM[responseItem.TaskID] {
			snap := task.Snapshot()
			status := normalizeSunoTaskStatus(responseItem.Status)
			if status == model.TaskStatusUnknown {
				_ = handleTaskPollFailure(ctx, task, snap.Status, "", fmt.Errorf("unknown Suno task status %q for task %s", responseItem.Status, task.TaskID))
				continue
			}
			task.Status = status
			task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
			task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
			task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
			task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
			task.Data = responseItem.Data
			now := time.Now().Unix()
			task.LastPollTime = now
			task.NextPollTime = now + int64(taskPollingIntervalSeconds())
			task.PollErrorCount = 0
			task.LastPollError = ""
			if status == model.TaskStatusFailure {
				logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
				if _, err := failTaskWithRefund(ctx, task, snap.Status, "", task.FailReason); err != nil {
					logger.LogError(ctx, fmt.Sprintf("persist failed Suno task %s: %v", task.TaskID, err))
				}
				continue
			}
			if status == model.TaskStatusSuccess {
				task.Progress = taskcommon.ProgressComplete
				task.NextPollTime = 0
				if task.FinishTime == 0 {
					task.FinishTime = now
				}
				prepareTaskBillingIntent(adaptor, task, &relaycommon.TaskInfo{Status: string(status)})
			}
			won, err := updateTaskWithExecutionGuard(task, snap.Status, "")
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("persist Suno task %s: %v", task.TaskID, err))
			} else if won && task.BillingStatus == model.TaskBillingStatusPending {
				if err := processPendingTaskBilling(ctx, task.ID); err != nil {
					logger.LogError(ctx, fmt.Sprintf("settle Suno task %s billing: %v", task.TaskID, err))
				}
			}
		}
	}
	for taskID, tasks := range taskM {
		if seen[taskID] {
			continue
		}
		for _, task := range tasks {
			_ = handleTaskPollFailure(ctx, task, task.Status, "", fmt.Errorf("Suno task %s was missing from batch response", taskID))
		}
	}
	return nil
}

func normalizeSunoTaskStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted", "created":
		return model.TaskStatusSubmitted
	case "queued", "queueing", "pending":
		return model.TaskStatusQueued
	case "processing", "running", "in_progress":
		return model.TaskStatusInProgress
	case "success", "succeeded", "completed":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "cancelled", "canceled":
		return model.TaskStatusFailure
	default:
		return model.TaskStatusUnknown
	}
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM channelTaskMap) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	concurrency := constant.TaskPollingConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, len(channelIDs))
	var wg sync.WaitGroup
	for _, channelID := range channelIDs {
		channelID := channelID
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := updateVideoTasks(ctx, platform, channelID, taskChannelM[channelID], taskM[channelID]); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelID, err.Error()))
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string][]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := getTaskPollingChannel(channelId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			reason := fmt.Sprintf("任务所属渠道已不存在，渠道 ID：%d", channelId)
			failTaskMapWithRefund(ctx, taskM, reason)
		}
		return fmt.Errorf("get task polling channel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		reason := fmt.Sprintf("任务平台 %s 缺少状态查询适配器", platform)
		failTaskMapWithRefund(ctx, taskM, reason)
		return errors.New(reason)
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	interval := time.Duration(constant.TaskPollingChannelIntervalMilliseconds) * time.Millisecond
	for i, taskId := range taskIds {
		if i > 0 && interval > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string][]*model.Task) error {
	tasks := taskM[taskId]
	if len(tasks) == 0 {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	var errs []error
	for _, task := range tasks {
		if err := updateTask(ctx, adaptor, ch, task, ""); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func getTaskPollingChannel(channelID int) (*model.Channel, error) {
	channel, cacheErr := model.CacheGetChannel(channelID)
	if cacheErr == nil {
		return channel, nil
	}
	channel, dbErr := model.GetChannelById(channelID, true)
	if dbErr != nil {
		return nil, dbErr
	}
	logger.LogWarn(context.Background(), fmt.Sprintf("task polling channel #%d missed cache lookup, using database record: %v", channelID, cacheErr))
	return channel, nil
}

func failTaskMapWithRefund(ctx context.Context, taskM map[string][]*model.Task, reason string) {
	for _, tasks := range taskM {
		for _, task := range tasks {
			if _, err := failTaskWithRefund(ctx, task, task.Status, "", reason); err != nil {
				logger.LogError(ctx, fmt.Sprintf("fail task %s: %v", task.TaskID, err))
			}
		}
	}
}

func handleTaskMapPollFailure(ctx context.Context, taskM map[string][]*model.Task, pollErr error) error {
	errs := []error{pollErr}
	for _, tasks := range taskM {
		for _, task := range tasks {
			if err := handleTaskPollFailure(ctx, task, task.Status, "", pollErr); err != nil && err.Error() != pollErr.Error() {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func handleSunoTaskMapHTTPFailure(ctx context.Context, taskM map[string][]*model.Task, statusCode int) error {
	var errs []error
	for _, tasks := range taskM {
		for _, task := range tasks {
			terminal, reason, responseErr := classifyTaskPollHTTPResponse(task, statusCode)
			if responseErr == nil {
				continue
			}
			if terminal {
				if _, err := failTaskWithRefund(ctx, task, task.Status, "", reason); err != nil {
					errs = append(errs, fmt.Errorf("persist terminal Suno poll failure for task %s: %w", task.TaskID, err))
				}
				continue
			}
			if err := handleTaskPollFailure(ctx, task, task.Status, "", responseErr); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func failTaskWithRefund(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, leaseOwner string, reason string) (bool, error) {
	if task == nil {
		return false, errors.New("task is nil")
	}
	if fromStatus == model.TaskStatusSuccess || fromStatus == model.TaskStatusFailure {
		return false, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "异步任务执行失败"
	}
	now := time.Now().Unix()
	task.Status = model.TaskStatusFailure
	task.Progress = taskcommon.ProgressComplete
	if task.FinishTime == 0 {
		task.FinishTime = now
	}
	task.FailReason = reason
	task.LastPollTime = now
	task.NextPollTime = 0
	task.LastPollError = reason
	task.ExecutionLeaseOwner = ""
	task.ExecutionLeaseUntil = 0
	clearLocalImageTaskRequest(task)
	if task.Quota != 0 {
		setTaskBillingIntent(task, 0, reason)
	}
	won, err := updateTaskWithExecutionGuard(task, fromStatus, leaseOwner)
	if err != nil || !won {
		return won, err
	}
	if task.BillingStatus == model.TaskBillingStatusPending {
		if err := processPendingTaskBilling(ctx, task.ID); err != nil {
			logger.LogError(ctx, fmt.Sprintf("settle failed task %s billing: %v", task.TaskID, err))
		}
	}
	return true, nil
}

func classifyTaskPollHTTPResponse(task *model.Task, statusCode int) (bool, string, error) {
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return false, "", nil
	}
	err := fmt.Errorf("upstream task query returned HTTP %d for task %s", statusCode, task.TaskID)
	if statusCode == http.StatusNotFound {
		grace := int64(constant.TaskPollingNotFoundGraceSeconds)
		if grace < 0 {
			grace = 0
		}
		if task.SubmitTime > 0 && time.Now().Unix()-task.SubmitTime < grace {
			return false, "", err
		}
		return true, "上游任务不存在（HTTP 404）", err
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusConflict || statusCode == http.StatusTooEarly || statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError {
		return false, "", err
	}
	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return true, fmt.Sprintf("上游任务查询失败（HTTP %d）", statusCode), err
	}
	return false, "", err
}

func handleTaskPollFailure(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, leaseOwner string, pollErr error) error {
	if pollErr == nil {
		return nil
	}
	// Local one-shot image executions use their execution lease and retry budget.
	if leaseOwner != "" {
		return pollErr
	}

	now := time.Now().Unix()
	task.LastPollTime = now
	task.PollErrorCount++
	task.LastPollError = truncateTaskPollError(pollErr.Error())
	if task.PollErrorCount >= taskPollingMaxErrors() {
		reason := fmt.Sprintf("上游任务状态连续查询失败（%d 次）", task.PollErrorCount)
		if _, err := failTaskWithRefund(ctx, task, fromStatus, "", reason); err != nil {
			return fmt.Errorf("%v; persist polling failure: %w", pollErr, err)
		}
		return pollErr
	}
	task.NextPollTime = now + int64(taskPollingRetryDelay(task.PollErrorCount)/time.Second)
	won, err := updateTaskWithExecutionGuard(task, fromStatus, "")
	if err != nil {
		return fmt.Errorf("%v; persist polling retry: %w", pollErr, err)
	}
	if !won {
		logger.LogInfo(ctx, fmt.Sprintf("task %s transitioned while recording poll failure", task.TaskID))
	}
	return pollErr
}

func taskPollingIntervalSeconds() int {
	if constant.TaskPollingIntervalSeconds > 0 {
		return constant.TaskPollingIntervalSeconds
	}
	return 15
}

// TaskPollingRequestContext applies a bounded deadline even when RELAY_TIMEOUT is disabled.
func TaskPollingRequestContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := constant.TaskPollingRequestTimeoutSeconds
	if timeout < 1 {
		timeout = 30
	}
	return context.WithTimeout(parent, time.Duration(timeout)*time.Second)
}

func taskPollingMaxErrors() int {
	if constant.TaskPollingMaxErrors > 0 {
		return constant.TaskPollingMaxErrors
	}
	return 20
}

func taskPollingRetryDelay(errorCount int) time.Duration {
	delay := 15 * time.Second
	for i := 1; i < errorCount && delay < 5*time.Minute; i++ {
		delay *= 2
		if delay >= 5*time.Minute {
			return 5 * time.Minute
		}
	}
	return delay
}

func truncateTaskPollError(message string) string {
	const maxLength = 1024
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength]
}

func updateTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task, leaseOwner string) error {
	taskId := task.GetUpstreamTaskID()
	snap := task.Snapshot()
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	var resp *http.Response
	var err error
	responseCtx := ctx
	if task.Platform == constant.TaskPlatformImage && task.PrivateData.LocalImageTask != nil {
		if ExecuteLocalImageTaskFunc == nil {
			return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, errors.New("local image task executor not found"))
		}
		resp, err = ExecuteLocalImageTaskFunc(ctx, task, ch, key, proxy)
	} else {
		requestCtx, cancel := TaskPollingRequestContext(ctx)
		defer cancel()
		responseCtx = requestCtx
		resp, err = adaptor.FetchTask(requestCtx, baseURL, key, map[string]any{
			"task_id": task.GetUpstreamTaskID(),
			"action":  task.Action,
			"model":   taskPollingModelName(task),
		}, proxy)
	}
	if err != nil {
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("fetchTask failed for task %s: %w", taskId, err))
	}
	return ApplyTaskPollResponse(responseCtx, adaptor, ch, task, resp, leaseOwner)
}

func taskPollingModelName(task *model.Task) string {
	if task == nil {
		return ""
	}
	if task.Properties.UpstreamModelName != "" {
		return task.Properties.UpstreamModelName
	}
	return task.Properties.OriginModelName
}

// ApplyTaskPollResponse applies a successful upstream fetch through the same
// persistence, result rehosting, terminal CAS, and billing path for background
// polling and request-triggered realtime refreshes.
func ApplyTaskPollResponse(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task, resp *http.Response, leaseOwner string) error {
	taskId := task.GetUpstreamTaskID()
	snap := task.Snapshot()
	proxy := ch.GetSetting().Proxy
	if resp == nil || resp.Body == nil {
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("fetchTask returned an empty response for task %s", taskId))
	}
	defer resp.Body.Close()
	if snap.Status == model.TaskStatusSuccess || snap.Status == model.TaskStatusFailure {
		return nil
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("readAll failed for task %s: %w", taskId, err))
	}

	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)
	terminal, reason, responseErr := classifyTaskPollHTTPResponse(task, resp.StatusCode)
	if responseErr != nil {
		if terminal {
			_, persistErr := failTaskWithRefund(ctx, task, snap.Status, leaseOwner, reason)
			if persistErr != nil {
				return fmt.Errorf("persist terminal poll failure for task %s: %w", task.TaskID, persistErr)
			}
			return nil
		}
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, responseErr)
	}

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
	} else if taskResult, err = parseTaskPollResult(ctx, adaptor, responseBody); err != nil {
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err))
	}

	now := time.Now().Unix()
	if taskResult == nil || taskResult.Status == "" {
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil && openaiError.Code == "429" {
				return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("upstream task query was rate limited for task %s", taskId))
			}
			if errorResult.ToMessage() != "" {
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("upstream returned an empty task status for task %s", taskId))
			}
		} else {
			return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("upstream returned an empty task status for task %s", taskId))
		}
	}

	newStatus := model.TaskStatus(taskResult.Status)
	switch newStatus {
	case model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure:
	default:
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("unknown task status %q for task %s", taskResult.Status, task.TaskID))
	}

	task.Data = redactVideoResponseBody(responseBody)
	task.LastPollTime = now
	task.NextPollTime = now + int64(taskPollingIntervalSeconds())
	task.PollErrorCount = 0
	task.LastPollError = ""
	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch newStatus {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			if task.Platform == constant.TaskPlatformImage {
				if err := completeImageDataURLResult(ctx, task, taskResult); err != nil {
					logger.LogError(ctx, fmt.Sprintf("Task %s data URL result rehost failed: %s", task.TaskID, err.Error()))
					task.Status = model.TaskStatusFailure
					task.Progress = taskcommon.ProgressComplete
					task.FailReason = err.Error()
					task.Data = imageTaskFailureData(task.FailReason)
					taskResult.Progress = taskcommon.ProgressComplete
					if quota != 0 {
						shouldRefund = true
					}
					break
				}
			} else {
				// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
				task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
			}
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.).
			// Some providers return URLs that are not reachable by clients (e.g. vidgen.x.ai),
			// so optionally rehost them to our configured object storage.
			resultURL := taskResult.Url
			if TaskResultRehostEnabledForURL(resultURL) {
				rehostedURL, err := RehostTaskResultURL(ctx, task, resultURL, proxy)
				if err != nil {
					logger.LogError(ctx, fmt.Sprintf("Task %s result rehost failed: %s", task.TaskID, err.Error()))
				} else if strings.TrimSpace(rehostedURL) != "" {
					task.Data = replaceRehostedURLInJSON(task.Data, resultURL, rehostedURL)
					taskResult.Url = rehostedURL
					resultURL = rehostedURL
				}
			}
			task.PrivateData.ResultURL = resultURL
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		shouldSettle = true
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
		shouldRefund = quota != 0
	default:
		return handleTaskPollFailure(ctx, task, snap.Status, leaseOwner, fmt.Errorf("unknown task status %q for task %s", taskResult.Status, task.TaskID))
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone {
		clearLocalImageTaskRequest(task)
		task.NextPollTime = 0
		if leaseOwner != "" {
			owned, err := model.IsTaskExecutionLeaseOwner(task.ID, leaseOwner, snap.Status)
			if err != nil {
				return fmt.Errorf("verify local image task %s lease: %w", task.TaskID, err)
			}
			if !owned {
				return errTaskExecutionLeaseLost
			}
		}
		if shouldRefund {
			setTaskBillingIntent(task, 0, task.FailReason)
		} else if shouldSettle {
			prepareTaskBillingIntent(adaptor, task, taskResult)
		}
	}
	leaseLost := false
	if isDone && snap.Status != task.Status {
		if leaseOwner != "" {
			task.ExecutionLeaseOwner = ""
			task.ExecutionLeaseUntil = 0
		}
		won, err := updateTaskWithExecutionGuard(task, snap.Status, leaseOwner)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldRefund = false
			shouldSettle = false
			if leaseOwner != "" {
				return fmt.Errorf("persist local image task %s failed: %w", task.TaskID, err)
			}
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned or lost its execution lease, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
			leaseLost = leaseOwner != ""
		}
	} else if !snap.Equal(task.Snapshot()) {
		won, err := updateTaskWithExecutionGuard(task, snap.Status, leaseOwner)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
			if leaseOwner != "" {
				return fmt.Errorf("persist local image task %s failed: %w", task.TaskID, err)
			}
		} else if !won && leaseOwner != "" {
			leaseLost = true
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	if (shouldSettle || shouldRefund) && task.BillingStatus == model.TaskBillingStatusPending {
		if err := processPendingTaskBilling(ctx, task.ID); err != nil {
			logger.LogError(ctx, fmt.Sprintf("settle task %s billing: %v", task.TaskID, err))
		}
	}
	if leaseLost {
		return errTaskExecutionLeaseLost
	}

	return nil
}

func parseTaskPollResult(ctx context.Context, adaptor TaskPollingAdaptor, body []byte) (*relaycommon.TaskInfo, error) {
	if parser, ok := adaptor.(taskPollingContextResultParser); ok {
		return parser.ParseTaskResultWithContext(ctx, body)
	}
	return adaptor.ParseTaskResult(body)
}

func clearLocalImageTaskRequest(task *model.Task) {
	if task == nil || task.PrivateData.LocalImageTask == nil {
		return
	}
	task.PrivateData.LocalImageTask.Request = nil
}

func updateTaskWithExecutionGuard(task *model.Task, fromStatus model.TaskStatus, leaseOwner string) (bool, error) {
	if leaseOwner != "" {
		return task.UpdateWithStatusAndLease(fromStatus, leaseOwner)
	}
	return task.UpdateWithStatus(fromStatus)
}

func imageTaskFailureData(reason string) []byte {
	data, err := common.Marshal(map[string]string{
		"status":      string(model.TaskStatusFailure),
		"fail_reason": reason,
	})
	if err != nil {
		return nil
	}
	return data
}

func completeImageDataURLResult(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo) error {
	resultURL := taskResult.Url
	if TaskResultRehostEnabledForDataURL(resultURL) {
		rehostedURL, err := RehostTaskResultDataURL(ctx, task, resultURL)
		if err != nil {
			return err
		}
		if strings.TrimSpace(rehostedURL) == "" {
			return fmt.Errorf("rehost image data URL returned empty URL")
		}
		task.Data = replaceRehostedImageDataURLInJSON(task.Data, resultURL, rehostedURL)
		taskResult.Url = rehostedURL
		resultURL = rehostedURL
	}
	task.PrivateData.ResultURL = resultURL
	return nil
}

func replaceRehostedURLInJSON(data []byte, oldURL, newURL string) []byte {
	if len(data) == 0 || strings.TrimSpace(oldURL) == "" || strings.TrimSpace(newURL) == "" {
		return data
	}
	var payload any
	if err := common.Unmarshal(data, &payload); err != nil {
		return data
	}
	changed := replaceURLValue(&payload, oldURL, newURL)
	if !changed {
		return data
	}
	updated, err := common.Marshal(payload)
	if err != nil {
		return data
	}
	return updated
}

func replaceRehostedImageDataURLInJSON(data []byte, oldDataURL, newURL string) []byte {
	if len(data) == 0 || strings.TrimSpace(oldDataURL) == "" || strings.TrimSpace(newURL) == "" {
		return data
	}
	_, b64Payload, err := parseBase64DataURL(oldDataURL)
	if err != nil {
		return replaceRehostedURLInJSON(data, oldDataURL, newURL)
	}
	var payload any
	if err := common.Unmarshal(data, &payload); err != nil {
		return data
	}
	changed := replaceImageDataValue(&payload, oldDataURL, b64Payload, newURL)
	if !changed {
		return data
	}
	updated, err := common.Marshal(payload)
	if err != nil {
		return data
	}
	return updated
}

func replaceImageDataValue(value *any, oldDataURL, oldB64, newURL string) bool {
	if value == nil {
		return false
	}
	switch v := (*value).(type) {
	case string:
		if strings.TrimSpace(v) == oldDataURL {
			*value = newURL
			return true
		}
	case []any:
		changed := false
		for i := range v {
			if replaceImageDataValue(&v[i], oldDataURL, oldB64, newURL) {
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for k := range v {
			item := v[k]
			if (k == "b64_json" || k == "b64_image") && strings.TrimSpace(stringValue(item)) == oldB64 {
				delete(v, k)
				v["url"] = newURL
				changed = true
				continue
			}
			if replaceImageDataValue(&item, oldDataURL, oldB64, newURL) {
				v[k] = item
				changed = true
			}
		}
		return changed
	}
	return false
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func replaceURLValue(value *any, oldURL, newURL string) bool {
	if value == nil {
		return false
	}
	switch v := (*value).(type) {
	case string:
		if strings.TrimSpace(v) == oldURL {
			*value = newURL
			return true
		}
	case []any:
		changed := false
		for i := range v {
			if replaceURLValue(&v[i], oldURL, newURL) {
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for k := range v {
			item := v[k]
			if replaceURLValue(&item, oldURL, newURL) {
				v[k] = item
				changed = true
			}
		}
		return changed
	}
	return false
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return
	}
	if bc := task.PrivateData.BillingContext; bc != nil && bc.BillingMode == "tiered_expr" && bc.BillingSchema != "" {
		if actualQuota, reason, ok := calculateTaskQuotaByCanonicalBilling(task, taskResult); ok {
			RecalculateTaskQuota(ctx, task, actualQuota, reason)
		}
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return
	}
	// 2. 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		RecalculateTaskQuotaByTokens(ctx, task, taskResult.TotalTokens)
		return
	}
	// 3. 无调整，保持预扣额度
}
