package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func UpdateMidjourneyTaskBulk() {
	for {
		time.Sleep(15 * time.Second)
		ctx := context.Background()

		tasks := model.GetAllUnFinishTasks()
		if len(tasks) == 0 {
			continue
		}

		logger.LogInfo(ctx, fmt.Sprintf("检测到未完成的任务数有: %v", len(tasks)))
		taskChannelM := make(map[int][]string)
		taskM := make(map[int]map[string]*model.Midjourney)
		for _, task := range tasks {
			if task.MjId == "" {
				failMidjourneyTask(ctx, task, "上游任务 ID 为空")
				continue
			}
			if taskM[task.ChannelId] == nil {
				taskM[task.ChannelId] = make(map[string]*model.Midjourney)
			}
			taskM[task.ChannelId][task.MjId] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task.MjId)
		}

		for channelId, taskIds := range taskChannelM {
			if err := updateMidjourneyChannel(ctx, channelId, taskIds, taskM[channelId]); err != nil {
				logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新 Midjourney 任务失败: %v", channelId, err))
			}
		}
	}
}

func updateMidjourneyChannel(ctx context.Context, channelID int, taskIDs []string, tasks map[string]*model.Midjourney) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelID, len(taskIDs)))
	if len(taskIDs) == 0 {
		return nil
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil {
		channel, err = model.GetChannelById(channelID, true)
	}
	if err != nil {
		reason := fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelID)
		for _, task := range tasks {
			failMidjourneyTask(ctx, task, reason)
		}
		return err
	}
	body, err := common.Marshal(map[string]any{"ids": taskIDs})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, fmt.Sprintf("%s/mj/task/list-by-condition", channel.GetBaseURL()), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if req.Body != nil {
		defer req.Body.Close()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("mj-api-secret", channel.Key)
	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return err
	}
	if resp == nil || resp.Body == nil {
		return errors.New("Midjourney task query returned an empty response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Midjourney task query returned HTTP %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var responseItems []dto.MidjourneyDto
	if err := common.Unmarshal(responseBody, &responseItems); err != nil {
		return fmt.Errorf("parse Midjourney task response: %w", err)
	}

	seen := make(map[string]bool, len(responseItems))
	for _, responseItem := range responseItems {
		task := tasks[responseItem.MjId]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("渠道 #%d 返回未知 Midjourney 任务 ID %q", channelID, responseItem.MjId))
			continue
		}
		seen[responseItem.MjId] = true
		if midjourneyTaskTimedOut(task, time.Now()) {
			failMidjourneyTask(ctx, task, "上游任务超时（超过1小时）")
			continue
		}
		applyMidjourneyResponse(ctx, task, responseItem)
	}
	for taskID, task := range tasks {
		if !seen[taskID] && midjourneyTaskTimedOut(task, time.Now()) {
			failMidjourneyTask(ctx, task, "上游批次响应持续遗漏任务且已超过1小时")
		}
	}
	return nil
}

func midjourneyTaskTimedOut(task *model.Midjourney, now time.Time) bool {
	return task != nil && task.SubmitTime > 0 && now.UnixMilli()-task.SubmitTime > int64(time.Hour/time.Millisecond)
}

func applyMidjourneyResponse(ctx context.Context, task *model.Midjourney, response dto.MidjourneyDto) {
	if !checkMjTaskNeedUpdate(task, response) {
		return
	}
	preStatus := task.Status
	task.Code = 1
	task.Progress = response.Progress
	task.PromptEn = response.PromptEn
	task.State = response.State
	if response.SubmitTime != 0 {
		task.SubmitTime = response.SubmitTime
	}
	task.StartTime = response.StartTime
	task.FinishTime = response.FinishTime
	task.ImageUrl = response.ImageUrl
	task.Status = response.Status
	task.FailReason = response.FailReason
	if response.Properties != nil {
		value, _ := common.Marshal(response.Properties)
		task.Properties = string(value)
	}
	if response.Buttons != nil {
		value, _ := common.Marshal(response.Buttons)
		task.Buttons = string(value)
	}
	task.VideoUrl = response.VideoUrl
	if len(response.VideoUrls) > 0 {
		value, err := common.Marshal(response.VideoUrls)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("序列化 VideoUrls 失败: %v", err))
			task.VideoUrls = "[]"
		} else {
			task.VideoUrls = string(value)
		}
	} else {
		task.VideoUrls = ""
	}
	failed := task.Status == "FAILURE" || response.FailReason != ""
	if failed {
		task.Status = "FAILURE"
		task.Progress = "100%"
		if task.FailReason == "" {
			task.FailReason = "上游任务执行失败"
		}
	}
	var won bool
	var err error
	if failed {
		won, err = task.UpdateFailureWithRefund(preStatus)
	} else {
		won, err = task.UpdateWithStatus(preStatus)
	}
	if err != nil {
		logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
	} else if won && failed {
		recordMidjourneyRefund(task, "构图失败")
	}
}

func failMidjourneyTask(ctx context.Context, task *model.Midjourney, reason string) {
	if task == nil || task.Status == "SUCCESS" || task.Status == "FAILURE" {
		return
	}
	preStatus := task.Status
	task.Status = "FAILURE"
	task.Progress = "100%"
	task.FailReason = reason
	won, err := task.UpdateFailureWithRefund(preStatus)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("标记 Midjourney 任务 %s 失败: %v", task.MjId, err))
	} else if won {
		recordMidjourneyRefund(task, reason)
	}
}

func recordMidjourneyRefund(task *model.Midjourney, reason string) {
	if task.Quota == 0 {
		return
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId: task.UserId, LogType: model.LogTypeRefund, ChannelId: task.ChannelId,
		ModelName: service.CovertMjpActionToModelName(task.Action), Quota: task.Quota,
		Other: map[string]interface{}{"task_id": task.MjId, "reason": reason},
	})
}

func checkMjTaskNeedUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
	if oldTask.Code != 1 {
		return true
	}
	if oldTask.Progress != newTask.Progress {
		return true
	}
	if oldTask.PromptEn != newTask.PromptEn {
		return true
	}
	if oldTask.State != newTask.State {
		return true
	}
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.ImageUrl != newTask.ImageUrl {
		return true
	}
	if oldTask.Status != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.Progress != "100%" && newTask.FailReason != "" {
		return true
	}
	// 检查 VideoUrl 是否需要更新
	if oldTask.VideoUrl != newTask.VideoUrl {
		return true
	}
	// 检查 VideoUrls 是否需要更新
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		newVideoUrlsStr, _ := common.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(newVideoUrlsStr) {
			return true
		}
	} else if oldTask.VideoUrls != "" {
		// 如果新数据没有 VideoUrls 但旧数据有，需要更新（清空）
		return true
	}

	return false
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllUserTask(userId, queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
