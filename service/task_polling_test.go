package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskPollingTestAdaptor struct {
	statusCode int
	body       string
	result     *relaycommon.TaskInfo
	parseCalls int
}

func (a *taskPollingTestAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *taskPollingTestAdaptor) FetchTask(context.Context, string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: a.statusCode,
		Body:       io.NopCloser(strings.NewReader(a.body)),
		Header:     make(http.Header),
	}, nil
}

func (a *taskPollingTestAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	a.parseCalls++
	if a.result == nil {
		return nil, errors.New("missing test result")
	}
	return a.result, nil
}

func (a *taskPollingTestAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateTaskRetryableHTTPPreservesStatus(t *testing.T) {
	truncate(t)
	originalMaxErrors := constant.TaskPollingMaxErrors
	constant.TaskPollingMaxErrors = 20
	t.Cleanup(func() { constant.TaskPollingMaxErrors = originalMaxErrors })

	task := &model.Task{
		TaskID:     "task_poll_retryable_http",
		Status:     model.TaskStatusInProgress,
		SubmitTime: time.Now().Unix(),
		Data:       []byte(`{"status":"processing"}`),
	}
	require.NoError(t, model.DB.Create(task).Error)
	adaptor := &taskPollingTestAdaptor{statusCode: http.StatusServiceUnavailable, body: `{"detail":"maintenance"}`}

	err := updateTask(context.Background(), adaptor, &model.Channel{}, task, "")
	require.Error(t, err)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, reloaded.Status)
	assert.Equal(t, 1, reloaded.PollErrorCount)
	assert.Greater(t, reloaded.NextPollTime, time.Now().Unix())
	assert.Zero(t, adaptor.parseCalls, "non-2xx responses must not reach provider parsers")
}

func TestUpdateTaskPermanentHTTPFailsAndRefundsOnce(t *testing.T) {
	truncate(t)
	const userID, channelID, preConsumed = 101, 101, 200
	seedUser(t, userID, 1000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.TaskID = "task_poll_permanent_http"
	task.SubmitTime = time.Now().Unix()
	require.NoError(t, model.DB.Create(task).Error)
	adaptor := &taskPollingTestAdaptor{statusCode: http.StatusUnauthorized, body: `{"error":{"message":"invalid key"}}`}

	require.NoError(t, updateTask(context.Background(), adaptor, &model.Channel{Id: channelID}, task, ""))
	assert.Equal(t, 1200, getUserQuota(t, userID))
	assert.Equal(t, int64(1), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Contains(t, reloaded.FailReason, "HTTP 401")

	won, err := failTaskWithRefund(context.Background(), &reloaded, model.TaskStatusFailure, "", "duplicate failure")
	require.NoError(t, err)
	assert.False(t, won)
	assert.Equal(t, 1200, getUserQuota(t, userID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestUpdateTaskStopsAfterPollErrorBudget(t *testing.T) {
	truncate(t)
	originalMaxErrors := constant.TaskPollingMaxErrors
	constant.TaskPollingMaxErrors = 1
	t.Cleanup(func() { constant.TaskPollingMaxErrors = originalMaxErrors })
	const userID, channelID, preConsumed = 102, 102, 300
	seedUser(t, userID, 1000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.TaskID = "task_poll_error_budget"
	task.SubmitTime = time.Now().Unix()
	require.NoError(t, model.DB.Create(task).Error)
	adaptor := &taskPollingTestAdaptor{statusCode: http.StatusServiceUnavailable, body: `{}`}

	require.Error(t, updateTask(context.Background(), adaptor, &model.Channel{Id: channelID}, task, ""))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Equal(t, 1, reloaded.PollErrorCount)
	assert.Equal(t, 1300, getUserQuota(t, userID))
}

func TestUpdateSunoTasksAppliesNotFoundGracePerTask(t *testing.T) {
	truncate(t)
	originalFactory := GetTaskAdaptorFunc
	originalGrace := constant.TaskPollingNotFoundGraceSeconds
	originalMaxErrors := constant.TaskPollingMaxErrors
	constant.TaskPollingNotFoundGraceSeconds = 60
	constant.TaskPollingMaxErrors = 20
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		require.Equal(t, constant.TaskPlatformSuno, platform)
		return &taskPollingTestAdaptor{statusCode: http.StatusNotFound, body: `{}`}
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = originalFactory
		constant.TaskPollingNotFoundGraceSeconds = originalGrace
		constant.TaskPollingMaxErrors = originalMaxErrors
	})

	const channelID = 103
	seedChannel(t, channelID)
	now := time.Now().Unix()
	newTask := &model.Task{
		TaskID:     "task_suno_not_found_new",
		Platform:   constant.TaskPlatformSuno,
		ChannelId:  channelID,
		Status:     model.TaskStatusInProgress,
		SubmitTime: now,
		Data:       []byte(`{}`),
	}
	oldTask := &model.Task{
		TaskID:     "task_suno_not_found_old",
		Platform:   constant.TaskPlatformSuno,
		ChannelId:  channelID,
		Status:     model.TaskStatusInProgress,
		SubmitTime: now - 61,
		Data:       []byte(`{}`),
	}
	require.NoError(t, model.DB.Create(newTask).Error)
	require.NoError(t, model.DB.Create(oldTask).Error)
	taskIDs := []string{newTask.TaskID, oldTask.TaskID}
	tasks := map[string][]*model.Task{
		newTask.TaskID: {newTask},
		oldTask.TaskID: {oldTask},
	}

	err := updateSunoTasks(context.Background(), channelID, taskIDs, tasks)
	require.Error(t, err, "the task inside the grace period must remain retryable")

	var reloadedNew model.Task
	require.NoError(t, model.DB.First(&reloadedNew, newTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, reloadedNew.Status)
	assert.Equal(t, 1, reloadedNew.PollErrorCount)
	assert.Greater(t, reloadedNew.NextPollTime, now)

	var reloadedOld model.Task
	require.NoError(t, model.DB.First(&reloadedOld, oldTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloadedOld.Status)
	assert.Equal(t, "100%", reloadedOld.Progress)
	assert.Contains(t, reloadedOld.FailReason, "HTTP 404")
}

func TestUpdateVideoTasksKeepsDuplicateUpstreamIDsScopedToChannel(t *testing.T) {
	truncate(t)
	originalFactory := GetTaskAdaptorFunc
	originalConcurrency := constant.TaskPollingConcurrency
	originalInterval := constant.TaskPollingChannelIntervalMilliseconds
	constant.TaskPollingConcurrency = 2
	constant.TaskPollingChannelIntervalMilliseconds = 0
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return &taskPollingTestAdaptor{
			statusCode: http.StatusOK,
			body:       `{}`,
			result:     &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess)},
		}
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = originalFactory
		constant.TaskPollingConcurrency = originalConcurrency
		constant.TaskPollingChannelIntervalMilliseconds = originalInterval
	})

	const firstChannelID, secondChannelID = 201, 202
	seedChannel(t, firstChannelID)
	seedChannel(t, secondChannelID)
	platform := constant.TaskPlatform("duplicate-upstream-id-test")
	first := &model.Task{TaskID: "task_duplicate_first", ChannelId: firstChannelID, Platform: platform, Status: model.TaskStatusInProgress, PrivateData: model.TaskPrivateData{UpstreamTaskID: "shared-upstream-id"}}
	second := &model.Task{TaskID: "task_duplicate_second", ChannelId: secondChannelID, Platform: platform, Status: model.TaskStatusInProgress, PrivateData: model.TaskPrivateData{UpstreamTaskID: "shared-upstream-id"}}
	require.NoError(t, model.DB.Create(first).Error)
	require.NoError(t, model.DB.Create(second).Error)
	taskChannels := map[int][]string{firstChannelID: {"shared-upstream-id"}, secondChannelID: {"shared-upstream-id"}}
	tasks := channelTaskMap{
		firstChannelID:  {"shared-upstream-id": {first}},
		secondChannelID: {"shared-upstream-id": {second}},
	}

	require.NoError(t, UpdateVideoTasks(context.Background(), platform, taskChannels, tasks))

	var reloaded []model.Task
	require.NoError(t, model.DB.Where("id IN ?", []int64{first.ID, second.ID}).Order("id").Find(&reloaded).Error)
	require.Len(t, reloaded, 2)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded[0].Status)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded[1].Status)
}

type blockingTaskPollingAdaptor struct {
	entered chan<- struct{}
	release <-chan struct{}
	result  *relaycommon.TaskInfo
}

func (a *blockingTaskPollingAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *blockingTaskPollingAdaptor) FetchTask(ctx context.Context, _, _ string, _ map[string]any, _ string) (*http.Response, error) {
	if a.entered != nil {
		a.entered <- struct{}{}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.release:
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	}
}

func (a *blockingTaskPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return a.result, nil
}

func (a *blockingTaskPollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateTaskPropagatesCancelledRequestContext(t *testing.T) {
	truncate(t)
	task := &model.Task{TaskID: "task_cancelled_poll", Status: model.TaskStatusInProgress}
	require.NoError(t, model.DB.Create(task).Error)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adaptor := &blockingTaskPollingAdaptor{release: make(chan struct{})}

	err := updateTask(ctx, adaptor, &model.Channel{}, task, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, 1, reloaded.PollErrorCount)
	assert.EqualValues(t, model.TaskStatusInProgress, reloaded.Status)
}

func TestRunTaskPollingCycleDispatchesPlatformsConcurrently(t *testing.T) {
	truncate(t)
	originalFactory := GetTaskAdaptorFunc
	originalLimit := constant.TaskQueryLimit
	originalConcurrency := constant.TaskPollingConcurrency
	originalInterval := constant.TaskPollingChannelIntervalMilliseconds
	constant.TaskQueryLimit = 100
	constant.TaskPollingConcurrency = 1
	constant.TaskPollingChannelIntervalMilliseconds = 0
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return &blockingTaskPollingAdaptor{
			entered: entered,
			release: release,
			result:  &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess)},
		}
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = originalFactory
		constant.TaskQueryLimit = originalLimit
		constant.TaskPollingConcurrency = originalConcurrency
		constant.TaskPollingChannelIntervalMilliseconds = originalInterval
	})

	seedChannel(t, 301)
	seedChannel(t, 302)
	tasks := []*model.Task{
		{TaskID: "task_parallel_a", Platform: "parallel-a", ChannelId: 301, Status: model.TaskStatusInProgress},
		{TaskID: "task_parallel_b", Platform: "parallel-b", ChannelId: 302, Status: model.TaskStatusInProgress},
	}
	require.NoError(t, model.DB.Create(&tasks).Error)
	done := make(chan struct{})
	go func() {
		runTaskPollingCycle(context.Background(), nil)
		close(done)
	}()

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-deadline.C:
			t.Fatal("all platforms did not enter polling before either was released")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("polling cycle did not finish")
	}
}
