package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupByteforBalanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestUpdateChannelBalanceQueriesByteforAvailableBalance(t *testing.T) {
	db := setupByteforBalanceTestDB(t)
	type observedRequest struct {
		method        string
		path          string
		authorization string
	}
	requestObserved := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestObserved <- observedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":100,"frozen":5,"available":95,"totalRecharged":200,"totalConsumed":105}}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	channel := &model.Channel{
		Type:    constant.ChannelTypeDoubaoVideo,
		Key:     "bytefor-test-key",
		BaseURL: &baseURL,
		Models:  "bytefor-2.0-real-priority",
	}
	require.NoError(t, db.Create(channel).Error)

	balance, err := updateChannelBalance(channel)
	require.NoError(t, err)
	assert.Equal(t, 95.0, balance)
	request := <-requestObserved
	assert.Equal(t, http.MethodGet, request.method)
	assert.Equal(t, "/api/v1/balance", request.path)
	assert.Equal(t, "Bearer bytefor-test-key", request.authorization)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, 95.0, stored.Balance)
	assert.NotZero(t, stored.BalanceUpdatedTime)
}
