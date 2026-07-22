package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributeRejectsBlankVideoModelBeforeChannelSelection(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		specificChannelID string
	}{
		{name: "automatic channel selection"},
		{name: "specific channel selection", specificChannelID: "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyLanguage, i18n.LangEn)
				if tt.specificChannelID != "" {
					common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, tt.specificChannelID)
				}
			})

			reachedHandler := false
			router.POST("/v1/videos", Distribute(), func(c *gin.Context) {
				reachedHandler = true
				c.Status(http.StatusNoContent)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":" ","prompt":"test"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.False(t, reachedHandler)
			assert.Contains(t, recorder.Body.String(), i18n.Translate(i18n.LangEn, i18n.MsgDistributorModelNameRequired))
		})
	}
}

func TestDistributeReturnsInvalidVideoOutputCodeDuringChannelSelection(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyLanguage, i18n.LangEn)
	})
	router.POST("/v1/videos", Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"video-model",
		"size":"1280x720",
		"aspect_ratio":"9:16"
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "invalid_video_output", response.Error.Code)
}
