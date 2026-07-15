package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoFetchTaskAccessByRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Task{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	users := []model.User{
		{Id: 1, Username: "admin", Password: "password", Role: common.RoleAdminUser, AffCode: "admin-code"},
		{Id: 2, Username: "requester", Password: "password", Role: common.RoleCommonUser, AffCode: "requester-code"},
		{Id: 64, Username: "owner", Password: "password", Role: common.RoleCommonUser, AffCode: "owner-code"},
	}
	require.NoError(t, db.Create(&users).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:   1,
		Type: constant.ChannelTypeSora,
		Key:  "test-key",
	}).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "task_owned_by_another_user",
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)),
		UserId:    64,
		ChannelId: 1,
		Status:    model.TaskStatusQueued,
		Progress:  "1%",
		Data:      []byte(`{"id":"upstream_task","status":"queued","progress":1}`),
	}).Error)

	tests := []struct {
		name       string
		requester  int
		wantAccess bool
	}{
		{name: "owner can fetch own task", requester: 64, wantAccess: true},
		{name: "administrator can fetch another user's task", requester: 1, wantAccess: true},
		{name: "ordinary user cannot fetch another user's task", requester: 2, wantAccess: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_owned_by_another_user", nil)
			c.Params = gin.Params{{Key: "task_id", Value: "task_owned_by_another_user"}}
			c.Set("id", tt.requester)

			body, taskErr := videoFetchByIDRespBodyBuilder(c)
			if !tt.wantAccess {
				require.NotNil(t, taskErr)
				assert.Equal(t, "task_not_exist", taskErr.Code)
				assert.Empty(t, body)
				return
			}

			require.Nil(t, taskErr)
			var response map[string]any
			require.NoError(t, common.Unmarshal(body, &response))
			assert.Equal(t, "task_owned_by_another_user", response["id"])
			assert.Equal(t, "queued", response["status"])
		})
	}
}
