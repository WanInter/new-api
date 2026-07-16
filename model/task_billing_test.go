package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPendingTaskBillingRollsBackAndRetriesExactlyOnce(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:            "task_billing_retry",
		UserId:            9101,
		Quota:             200,
		Status:            TaskStatusFailure,
		BillingStatus:     TaskBillingStatusPending,
		BillingDelta:      -200,
		BillingFinalQuota: 0,
		BillingReason:     "upstream failed",
	}
	require.NoError(t, DB.Create(task).Error)

	settlement, applied, err := ApplyPendingTaskBilling(task.ID)
	require.Error(t, err)
	assert.False(t, applied)
	assert.Nil(t, settlement)

	var afterRollback Task
	require.NoError(t, DB.First(&afterRollback, task.ID).Error)
	assert.Equal(t, TaskBillingStatusPending, afterRollback.BillingStatus)
	assert.Equal(t, 200, afterRollback.Quota)

	user := &User{Id: task.UserId, Username: "billing_retry_user", Quota: 1000}
	require.NoError(t, DB.Create(user).Error)
	settlement, applied, err = ApplyPendingTaskBilling(task.ID)
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, settlement)
	assert.Equal(t, -200, settlement.Delta)

	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 1200, reloadedUser.Quota)
	var completed Task
	require.NoError(t, DB.First(&completed, task.ID).Error)
	assert.Equal(t, TaskBillingStatusCompleted, completed.BillingStatus)
	assert.Zero(t, completed.Quota)

	settlement, applied, err = ApplyPendingTaskBilling(task.ID)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Nil(t, settlement)
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 1200, reloadedUser.Quota)
}

func TestGetPendingTaskBillingsDoesNotSelectLegacyTerminalTasks(t *testing.T) {
	truncateTables(t)
	legacy := &Task{TaskID: "task_legacy_terminal", Status: TaskStatusFailure, Quota: 100}
	pending := &Task{TaskID: "task_pending_billing", Status: TaskStatusFailure, BillingStatus: TaskBillingStatusPending}
	require.NoError(t, DB.Create(legacy).Error)
	require.NoError(t, DB.Create(pending).Error)

	tasks, err := GetPendingTaskBillings(10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, pending.TaskID, tasks[0].TaskID)
}
