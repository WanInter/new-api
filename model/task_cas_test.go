package model

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&Token{},
		&Log{},
		&Channel{},
		&VideoRoutingPolicy{},
		&VideoRoutingCapabilityRule{},
		&Ability{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&UserOAuthBinding{},
		&PerfMetric{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM video_routing_policies")
		DB.Exec("DELETE FROM video_routing_capability_rules")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM perf_metrics")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

func TestTaskCompletionTimeOnlyReturnsTerminalTimestamp(t *testing.T) {
	task := &Task{Status: TaskStatusInProgress, UpdatedAt: 100, FinishTime: 90}
	assert.Zero(t, task.CompletionTime())

	task.Status = TaskStatusSuccess
	assert.EqualValues(t, 90, task.CompletionTime())

	task.Status = TaskStatusFailure
	task.FinishTime = 0
	assert.EqualValues(t, 100, task.CompletionTime())
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

func TestTaskExecutionLeaseLifecycle(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:   "task_execution_lease_lifecycle",
		Platform: constant.TaskPlatformImage,
		Status:   TaskStatusNotStart,
		Progress: "0%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	claimed, err := TryClaimTaskExecutionLease(task.ID, "worker-1", now, now+300, 3)
	require.NoError(t, err)
	assert.True(t, claimed)

	var claimedTask Task
	require.NoError(t, DB.First(&claimedTask, task.ID).Error)
	assert.Equal(t, "worker-1", claimedTask.ExecutionLeaseOwner)
	assert.Equal(t, now+300, claimedTask.ExecutionLeaseUntil)
	assert.EqualValues(t, TaskStatusInProgress, claimedTask.Status)
	assert.Equal(t, "30%", claimedTask.Progress)
	assert.Equal(t, now, claimedTask.StartTime)
	assert.Equal(t, 1, claimedTask.ExecutionAttempts)

	claimed, err = TryClaimTaskExecutionLease(task.ID, "worker-2", now, now+300, 3)
	require.NoError(t, err)
	assert.False(t, claimed)

	retryAt := now + 15
	retried, err := RetryTaskExecutionLease(task.ID, "worker-1", retryAt)
	require.NoError(t, err)
	assert.True(t, retried)

	var queuedTask Task
	require.NoError(t, DB.First(&queuedTask, task.ID).Error)
	assert.EqualValues(t, TaskStatusQueued, queuedTask.Status)
	assert.Equal(t, "20%", queuedTask.Progress)
	assert.Empty(t, queuedTask.ExecutionLeaseOwner)
	assert.Equal(t, retryAt, queuedTask.ExecutionLeaseUntil)
}

func TestTaskExecutionLeaseExpiredClaimRejectsStaleOwner(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:   "task_execution_lease_transfer",
		Platform: constant.TaskPlatformImage,
		Status:   TaskStatusNotStart,
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	claimed, err := TryClaimTaskExecutionLease(task.ID, "worker-old", now, now+300, 3)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", task.ID).UpdateColumn("execution_lease_until", now-1).Error)

	claimed, err = TryClaimTaskExecutionLease(task.ID, "worker-new", now, now+600, 3)
	require.NoError(t, err)
	require.True(t, claimed)

	renewed, err := RenewTaskExecutionLease(task.ID, "worker-old", now+900)
	require.NoError(t, err)
	assert.False(t, renewed)
	released, err := ReleaseTaskExecutionLease(task.ID, "worker-old")
	require.NoError(t, err)
	assert.False(t, released)

	staleTask := &Task{
		ID:                  task.ID,
		TaskID:              task.TaskID,
		Platform:            constant.TaskPlatformImage,
		Status:              TaskStatusSuccess,
		Progress:            "100%",
		ExecutionLeaseOwner: "worker-old",
		Data:                json.RawMessage(`{}`),
	}
	won, err := staleTask.UpdateWithStatusAndLease(TaskStatusInProgress, "worker-old")
	require.NoError(t, err)
	assert.False(t, won)

	var current Task
	require.NoError(t, DB.First(&current, task.ID).Error)
	assert.Equal(t, "worker-new", current.ExecutionLeaseOwner)
	assert.EqualValues(t, TaskStatusInProgress, current.Status)
}

func TestTaskInsertMarksLocalImageTask(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:   "task_insert_local_image_marker",
		Platform: constant.TaskPlatformImage,
		Status:   TaskStatusNotStart,
		PrivateData: TaskPrivateData{
			LocalImageTask: &LocalImageTaskPrivateData{},
		},
	}

	require.NoError(t, task.Insert())

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.True(t, reloaded.IsLocalImageTask)
}

func TestPollingQueriesIsolateLocalImageTasks(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	localImage := &Task{
		TaskID:           "task_polling_local_image",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusQueued,
		IsLocalImageTask: true,
		PrivateData: TaskPrivateData{
			LocalImageTask: &LocalImageTaskPrivateData{},
		},
	}
	video := &Task{
		TaskID:   "task_polling_video",
		Platform: constant.TaskPlatformSuno,
		Status:   TaskStatusQueued,
	}
	insertTask(t, localImage)
	insertTask(t, video)

	pollingTasks, err := GetUnfinishedPollingTasks(now, 1)
	require.NoError(t, err)
	require.Len(t, pollingTasks, 1)
	assert.Equal(t, video.ID, pollingTasks[0].ID)

	localTasks, err := GetPendingLocalImageTasks(now, 1)
	require.NoError(t, err)
	require.Len(t, localTasks, 1)
	assert.Equal(t, localImage.ID, localTasks[0].ID)

	require.NoError(t, DB.Model(localImage).UpdateColumn("execution_lease_until", now+60).Error)
	localTasks, err = GetPendingLocalImageTasks(now, 1)
	require.NoError(t, err)
	assert.Empty(t, localTasks)
}

func TestPollingQuerySkipsDeferredTaskWithoutStarvingReadyTasks(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	deferred := &Task{TaskID: "task_polling_deferred", Platform: constant.TaskPlatformSuno, Status: TaskStatusInProgress, NextPollTime: now + 300}
	readyFirst := &Task{TaskID: "task_polling_ready_first", Platform: constant.TaskPlatformSuno, Status: TaskStatusInProgress}
	readySecond := &Task{TaskID: "task_polling_ready_second", Platform: constant.TaskPlatformSuno, Status: TaskStatusInProgress}
	insertTask(t, deferred)
	insertTask(t, readyFirst)
	insertTask(t, readySecond)

	tasks, err := GetUnfinishedPollingTasks(now, 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, readyFirst.ID, tasks[0].ID)
	require.NoError(t, DB.Model(readyFirst).UpdateColumn("next_poll_time", now+300).Error)

	tasks, err = GetUnfinishedPollingTasks(now, 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, readySecond.ID, tasks[0].ID)
}

func TestTaskExecutionLeaseHonorsAttemptLimit(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:            "task_execution_attempt_limit",
		Platform:          constant.TaskPlatformImage,
		Status:            TaskStatusQueued,
		ExecutionAttempts: 3,
	}
	insertTask(t, task)

	claimed, err := TryClaimTaskExecutionLease(task.ID, "worker", now, now+300, 3)
	require.NoError(t, err)
	assert.False(t, claimed)
}
