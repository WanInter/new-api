package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type localImageExecutionGuardAdaptor struct {
	result      *relaycommon.TaskInfo
	adjustCalls int
}

func (a *localImageExecutionGuardAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *localImageExecutionGuardAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, errors.New("unexpected remote task fetch")
}

func (a *localImageExecutionGuardAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return a.result, nil
}

func (a *localImageExecutionGuardAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	a.adjustCalls++
	return 0
}

func createLocalImageDispatcherTask(t *testing.T, suffix string) *model.Task {
	t.Helper()
	task := &model.Task{
		TaskID:           "local_image_dispatcher_" + suffix,
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusNotStart,
		Progress:         "0%",
		IsLocalImageTask: true,
		PrivateData: model.TaskPrivateData{
			LocalImageTask: &model.LocalImageTaskPrivateData{},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func TestLocalImageTaskDispatcherBoundsConcurrency(t *testing.T) {
	truncate(t)

	const concurrency = 2
	started := make(chan struct{}, concurrency)
	release := make(chan struct{})
	var mu sync.Mutex
	active := 0
	maxActive := 0
	dispatcher := newLocalImageTaskDispatcher(concurrency, 30*time.Second, func(_ context.Context, _ *model.Task, _ string) error {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		started <- struct{}{}

		<-release
		mu.Lock()
		active--
		mu.Unlock()
		return nil
	})

	tasks := make([]*model.Task, concurrency+1)
	for i := range tasks {
		tasks[i] = createLocalImageDispatcherTask(t, fmt.Sprintf("bound_%d", i))
	}

	require.True(t, dispatcher.TryDispatch(tasks[0]))
	require.True(t, dispatcher.TryDispatch(tasks[1]))
	assert.False(t, dispatcher.TryDispatch(tasks[2]))
	for range concurrency {
		<-started
	}

	mu.Lock()
	assert.Equal(t, concurrency, active)
	assert.Equal(t, concurrency, maxActive)
	mu.Unlock()

	close(release)
	dispatcher.Wait()
	for _, task := range tasks[:concurrency] {
		var reloaded model.Task
		require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
		assert.Empty(t, reloaded.ExecutionLeaseOwner)
		assert.Zero(t, reloaded.ExecutionLeaseUntil)
	}
}

func TestLocalImageTaskDispatcherRetriesAfterRunnerError(t *testing.T) {
	truncate(t)
	task := createLocalImageDispatcherTask(t, "retry")
	dispatcher := newLocalImageTaskDispatcher(1, 30*time.Second, func(_ context.Context, _ *model.Task, _ string) error {
		return errors.New("temporary upstream failure")
	})

	require.True(t, dispatcher.TryDispatch(task))
	dispatcher.Wait()

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusQueued, reloaded.Status)
	assert.Equal(t, "20%", reloaded.Progress)
	assert.Empty(t, reloaded.ExecutionLeaseOwner)
	assert.Greater(t, reloaded.ExecutionLeaseUntil, time.Now().Unix())
	assert.Equal(t, 1, reloaded.ExecutionAttempts)
}

func TestLocalImageTaskDispatcherFailsAtAttemptLimit(t *testing.T) {
	truncate(t)
	task := createLocalImageDispatcherTask(t, "attempt_limit")
	dispatcher := newLocalImageTaskDispatcher(1, 30*time.Second, func(context.Context, *model.Task, string) error {
		return errors.New("persistent upstream failure")
	})
	dispatcher.maxAttempts = 1

	require.True(t, dispatcher.TryDispatch(task))
	dispatcher.Wait()

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Equal(t, 1, reloaded.ExecutionAttempts)
	assert.Empty(t, reloaded.ExecutionLeaseOwner)
	assert.Zero(t, reloaded.ExecutionLeaseUntil)
}

func TestLocalImageTaskDispatcherBackoffDoesNotStarveLaterTask(t *testing.T) {
	truncate(t)
	first := createLocalImageDispatcherTask(t, "fairness_first")
	second := createLocalImageDispatcherTask(t, "fairness_second")
	runs := make(chan int64, 2)
	dispatcher := newLocalImageTaskDispatcher(1, 30*time.Second, func(_ context.Context, task *model.Task, _ string) error {
		runs <- task.ID
		if task.ID == first.ID {
			return errors.New("temporary first-task failure")
		}
		return nil
	})

	require.True(t, dispatcher.TryDispatch(first))
	dispatcher.Wait()
	assert.Equal(t, first.ID, <-runs)
	assert.False(t, dispatcher.TryDispatch(first), "backoff must make the failed task temporarily ineligible")
	require.True(t, dispatcher.TryDispatch(second))
	dispatcher.Wait()
	assert.Equal(t, second.ID, <-runs)
}

func TestLocalImageTaskDispatcherRefillsFreedSlot(t *testing.T) {
	truncate(t)
	first := createLocalImageDispatcherTask(t, "refill_first")
	second := createLocalImageDispatcherTask(t, "refill_second")
	started := make(chan int64, 2)
	release := make(chan struct{}, 2)
	dispatcher := newLocalImageTaskDispatcher(1, 30*time.Second, func(_ context.Context, task *model.Task, leaseOwner string) error {
		started <- task.ID
		<-release
		task.Status = model.TaskStatusSuccess
		task.Progress = "100%"
		won, err := task.UpdateWithStatusAndLease(model.TaskStatusInProgress, leaseOwner)
		if err != nil {
			return err
		}
		if !won {
			return errTaskExecutionLeaseLost
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		dispatcher.Run(ctx, time.Hour, 10)
	}()

	select {
	case taskID := <-started:
		assert.Equal(t, first.ID, taskID)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first local image task did not start")
	}
	release <- struct{}{}
	select {
	case taskID := <-started:
		assert.Equal(t, second.ID, taskID)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "dispatcher did not refill the freed slot")
	}

	cancel()
	release <- struct{}{}
	dispatcher.Wait()
	<-runDone
}

func TestLocalImageTaskDispatcherMarksLegacyTasks(t *testing.T) {
	truncate(t)
	remoteImage := &model.Task{
		TaskID:   "local_image_dispatcher_remote_image",
		Platform: constant.TaskPlatformImage,
		Status:   model.TaskStatusQueued,
	}
	require.NoError(t, model.DB.Create(remoteImage).Error)
	legacyLocalImage := createLocalImageDispatcherTask(t, "legacy_marker")
	require.NoError(t, model.DB.Model(legacyLocalImage).UpdateColumn("is_local_image_task", false).Error)
	dispatcher := newLocalImageTaskDispatcher(1, 30*time.Second, nil)

	dispatcher.markLegacyTasks(context.Background(), 1)

	var reloadedRemote model.Task
	require.NoError(t, model.DB.First(&reloadedRemote, remoteImage.ID).Error)
	assert.False(t, reloadedRemote.IsLocalImageTask)
	var reloadedLocal model.Task
	require.NoError(t, model.DB.First(&reloadedLocal, legacyLocalImage.ID).Error)
	assert.True(t, reloadedLocal.IsLocalImageTask)
}

func TestLocalImageRetryDelay(t *testing.T) {
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 0, want: 15 * time.Second},
		{attempts: 1, want: 15 * time.Second},
		{attempts: 2, want: 30 * time.Second},
		{attempts: 5, want: 4 * time.Minute},
		{attempts: 6, want: 5 * time.Minute},
		{attempts: 20, want: 5 * time.Minute},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, localImageRetryDelay(test.attempts))
	}
}

func TestUpdateTaskStaleLeaseSkipsSettlement(t *testing.T) {
	truncate(t)
	now := time.Now().Unix()
	task := createLocalImageDispatcherTask(t, "stale_settlement")
	require.NoError(t, model.DB.Model(task).Updates(map[string]any{
		"status":                model.TaskStatusInProgress,
		"progress":              "30%",
		"execution_lease_owner": "current-worker",
		"execution_lease_until": now + 300,
	}).Error)
	task.Status = model.TaskStatusInProgress
	task.Progress = "30%"
	task.ExecutionLeaseOwner = "stale-worker"
	task.ExecutionLeaseUntil = now + 300

	originalExecutor := ExecuteLocalImageTaskFunc
	t.Cleanup(func() { ExecuteLocalImageTaskFunc = originalExecutor })
	ExecuteLocalImageTaskFunc = func(context.Context, *model.Task, *model.Channel, string, string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	}
	adaptor := &localImageExecutionGuardAdaptor{
		result: &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess)},
	}

	err := updateTask(context.Background(), adaptor, &model.Channel{}, task, "stale-worker")
	require.ErrorIs(t, err, errTaskExecutionLeaseLost)
	assert.Zero(t, adaptor.adjustCalls)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, reloaded.Status)
	assert.Equal(t, "current-worker", reloaded.ExecutionLeaseOwner)
}

func TestIsLocalImageExecutionTask(t *testing.T) {
	localImage := &model.Task{
		Platform: constant.TaskPlatformImage,
		PrivateData: model.TaskPrivateData{
			LocalImageTask: &model.LocalImageTaskPrivateData{},
		},
	}
	remoteImage := &model.Task{Platform: constant.TaskPlatformImage}
	nonImage := &model.Task{
		Platform: constant.TaskPlatformSuno,
		PrivateData: model.TaskPrivateData{
			LocalImageTask: &model.LocalImageTaskPrivateData{},
		},
	}

	assert.True(t, isLocalImageExecutionTask(localImage))
	assert.False(t, isLocalImageExecutionTask(remoteImage))
	assert.False(t, isLocalImageExecutionTask(nonImage))
	assert.False(t, isLocalImageExecutionTask(nil))
}
