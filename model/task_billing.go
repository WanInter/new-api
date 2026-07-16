package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	TaskBillingStatusPending    = "pending"
	TaskBillingStatusProcessing = "processing"
	TaskBillingStatusCompleted  = "completed"
)

type TaskBillingSettlement struct {
	Task             Task
	PreConsumedQuota int
	FinalQuota       int
	Delta            int
	Reason           string
}

func GetPendingTaskBillings(limit int) ([]*Task, error) {
	if limit < 1 {
		return nil, nil
	}
	var tasks []*Task
	err := DB.Where("billing_status = ?", TaskBillingStatusPending).
		Order("id").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// ApplyPendingTaskBilling atomically claims one billing intent, adjusts its
// funding source and token quota, and marks the intent completed.
func ApplyPendingTaskBilling(taskID int64) (*TaskBillingSettlement, bool, error) {
	if taskID <= 0 {
		return nil, false, errors.New("invalid task billing id")
	}

	var settlement *TaskBillingSettlement
	var tokenKey string
	err := DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&Task{}).
			Where("id = ? AND billing_status = ?", taskID, TaskBillingStatusPending).
			UpdateColumn("billing_status", TaskBillingStatusProcessing)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil
		}

		var task Task
		if err := tx.First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure {
			return fmt.Errorf("task %s has pending billing before terminal status", task.TaskID)
		}

		delta := task.BillingDelta
		if delta != 0 {
			if task.PrivateData.BillingSource == "subscription" && task.PrivateData.SubscriptionId > 0 {
				if err := applyTaskSubscriptionDelta(tx, task.PrivateData.SubscriptionId, int64(delta)); err != nil {
					return err
				}
			} else {
				result := tx.Model(&User{}).
					Where("id = ?", task.UserId).
					UpdateColumn("quota", gorm.Expr("quota - ?", delta))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return fmt.Errorf("task billing user %d not found", task.UserId)
				}
			}

			if task.PrivateData.TokenId > 0 {
				var token Token
				err := tx.Unscoped().Where("id = ?", task.PrivateData.TokenId).First(&token).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil {
					tokenKey = token.Key
					result := tx.Model(&Token{}).Unscoped().
						Where("id = ?", token.Id).
						Updates(map[string]any{
							"remain_quota":  gorm.Expr("remain_quota - ?", delta),
							"used_quota":    gorm.Expr("used_quota + ?", delta),
							"accessed_time": common.GetTimestamp(),
						})
					if result.Error != nil {
						return result.Error
					}
				}
			}
		}

		preConsumedQuota := task.Quota
		result := tx.Model(&Task{}).
			Where("id = ? AND billing_status = ?", task.ID, TaskBillingStatusProcessing).
			Updates(map[string]any{
				"billing_status": TaskBillingStatusCompleted,
				"quota":          task.BillingFinalQuota,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("task billing claim was lost")
		}

		task.BillingStatus = TaskBillingStatusCompleted
		task.Quota = task.BillingFinalQuota
		settlement = &TaskBillingSettlement{
			Task:             task,
			PreConsumedQuota: preConsumedQuota,
			FinalQuota:       task.BillingFinalQuota,
			Delta:            delta,
			Reason:           task.BillingReason,
		}
		return nil
	})
	if err != nil || settlement == nil {
		return nil, false, err
	}

	if settlement.Delta != 0 {
		cacheDelta := int64(-settlement.Delta)
		if settlement.Task.PrivateData.BillingSource != "subscription" {
			if err := cacheIncrUserQuota(settlement.Task.UserId, cacheDelta); err != nil {
				common.SysLog(fmt.Sprintf("failed to sync task billing user cache: %s", err.Error()))
			}
		}
		if tokenKey != "" {
			if err := cacheIncrTokenQuota(tokenKey, cacheDelta); err != nil {
				common.SysLog(fmt.Sprintf("failed to sync task billing token cache: %s", err.Error()))
			}
		}
	}
	return settlement, true, nil
}

func applyTaskSubscriptionDelta(tx *gorm.DB, subscriptionID int, delta int64) error {
	if delta > 0 {
		result := tx.Model(&UserSubscription{}).
			Where("id = ?", subscriptionID).
			Where("amount_total <= 0 OR amount_used + ? <= amount_total", delta).
			UpdateColumn("amount_used", gorm.Expr("amount_used + ?", delta))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("subscription %d not found or quota exceeded", subscriptionID)
		}
		return nil
	}
	result := tx.Model(&UserSubscription{}).
		Where("id = ?", subscriptionID).
		UpdateColumn("amount_used", gorm.Expr(
			"CASE WHEN amount_used + ? < 0 THEN 0 ELSE amount_used + ? END",
			delta,
			delta,
		))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("subscription %d not found", subscriptionID)
	}
	return nil
}
