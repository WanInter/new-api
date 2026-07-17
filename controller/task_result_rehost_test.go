package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTaskResultRehostSettingsDoesNotExposeCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_ID", "sensitive-access-id")
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_SECRET", "sensitive-access-secret")
	t.Setenv("TASK_RESULT_REHOST_BUCKET", "media-1250000000")
	t.Setenv("TASK_RESULT_REHOST_REGION", "ap-guangzhou")
	t.Setenv("TASK_RESULT_REHOST_BACKEND", "tencent_cos")

	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/option/task-result-rehost", nil)

	GetTaskResultRehostSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.NotContains(t, body, "sensitive-access-id")
	assert.NotContains(t, body, "sensitive-access-secret")
	assert.Contains(t, body, `"credentials_configured":true`)
	assert.Contains(t, body, `"credential_source":"environment"`)
	assert.False(t, strings.Contains(body, "enc:v1:"))
}
