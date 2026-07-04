package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRequestBodyLogTestContext(t *testing.T, contentType string, body string) *gin.Context {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", contentType)
	return ctx
}

func requestBodyInfoFromAdminInfo(t *testing.T, adminInfo map[string]interface{}) map[string]interface{} {
	t.Helper()
	bodyInfo, ok := adminInfo["request_body"].(map[string]interface{})
	require.True(t, ok)
	return bodyInfo
}

func TestAppendRequestBodyAdminInfoRecordsTextPreviewAndRestoresBody(t *testing.T) {
	body := `{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`
	ctx := newRequestBodyLogTestContext(t, "application/json", body)
	defer common.CleanupBodyStorage(ctx)
	adminInfo := map[string]interface{}{}

	AppendRequestBodyAdminInfo(ctx, adminInfo)

	bodyInfo := requestBodyInfoFromAdminInfo(t, adminInfo)
	assert.Equal(t, "application/json", bodyInfo["content_type"])
	assert.EqualValues(t, len(body), bodyInfo["size"])
	assert.Equal(t, body, bodyInfo["preview"])
	assert.Equal(t, false, bodyInfo["truncated"])
	assert.EqualValues(t, requestBodyLogPreviewLimit, bodyInfo["limit"])

	remaining, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(remaining))
}

func TestAppendRequestBodyAdminInfoTruncatesLargeTextBody(t *testing.T) {
	body := strings.Repeat("a", requestBodyLogPreviewLimit+32)
	ctx := newRequestBodyLogTestContext(t, "text/plain; charset=utf-8", body)
	defer common.CleanupBodyStorage(ctx)
	adminInfo := map[string]interface{}{}

	AppendRequestBodyAdminInfo(ctx, adminInfo)

	bodyInfo := requestBodyInfoFromAdminInfo(t, adminInfo)
	assert.EqualValues(t, len(body), bodyInfo["size"])
	assert.Equal(t, strings.Repeat("a", requestBodyLogPreviewLimit), bodyInfo["preview"])
	assert.Equal(t, true, bodyInfo["truncated"])
}

func TestAppendRequestBodyAdminInfoOmitsNonTextBody(t *testing.T) {
	ctx := newRequestBodyLogTestContext(t, "application/octet-stream", "\x00\x01\x02")
	defer common.CleanupBodyStorage(ctx)
	adminInfo := map[string]interface{}{}

	AppendRequestBodyAdminInfo(ctx, adminInfo)

	bodyInfo := requestBodyInfoFromAdminInfo(t, adminInfo)
	assert.Equal(t, "application/octet-stream", bodyInfo["content_type"])
	assert.EqualValues(t, 3, bodyInfo["size"])
	assert.Equal(t, "non_text_body", bodyInfo["omitted_reason"])
	assert.NotContains(t, bodyInfo, "preview")

	remaining, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 1, 2}, remaining)
}
