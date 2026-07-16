package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMidjourneyPollingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMemoryCache := common.MemoryCacheEnabled
	originalRedis := common.RedisEnabled
	originalBatchUpdate := common.BatchUpdateEnabled
	originalLogConsume := common.LogConsumeEnabled
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Midjourney{}, &model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.MemoryCacheEnabled = originalMemoryCache
		common.RedisEnabled = originalRedis
		common.BatchUpdateEnabled = originalBatchUpdate
		common.LogConsumeEnabled = originalLogConsume
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestUpdateMidjourneyChannelIgnoresUnknownResponseID(t *testing.T) {
	db := setupMidjourneyPollingTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"unknown","status":"IN_PROGRESS","progress":"50%"}]`))
	}))
	defer server.Close()
	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{Id: 1, BaseURL: &baseURL, Key: "secret"}).Error)
	task := &model.Midjourney{
		MjId:       "known",
		ChannelId:  1,
		Status:     "IN_PROGRESS",
		Progress:   "25%",
		SubmitTime: time.Now().UnixMilli(),
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, updateMidjourneyChannel(context.Background(), 1, []string{task.MjId}, map[string]*model.Midjourney{task.MjId: task}))
	var reloaded model.Midjourney
	require.NoError(t, db.First(&reloaded, task.Id).Error)
	assert.Equal(t, "IN_PROGRESS", reloaded.Status)
	assert.Equal(t, "25%", reloaded.Progress)
}

func TestUpdateMidjourneyChannelFailsAndRefundsOmittedExpiredTask(t *testing.T) {
	db := setupMidjourneyPollingTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{Id: 2, BaseURL: &baseURL, Key: "secret"}).Error)
	user := &model.User{Id: 2, Username: "midjourney_user", Quota: 1000}
	require.NoError(t, db.Create(user).Error)
	task := &model.Midjourney{
		MjId:       "omitted",
		UserId:     user.Id,
		ChannelId:  2,
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		SubmitTime: time.Now().Add(-2 * time.Hour).UnixMilli(),
		Quota:      200,
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, updateMidjourneyChannel(context.Background(), 2, []string{task.MjId}, map[string]*model.Midjourney{task.MjId: task}))
	var reloaded model.Midjourney
	require.NoError(t, db.First(&reloaded, task.Id).Error)
	assert.Equal(t, "FAILURE", reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Contains(t, reloaded.FailReason, "遗漏")
	var reloadedUser model.User
	require.NoError(t, db.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 1200, reloadedUser.Quota)
	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
}
