package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAppendMappedModelInfoAddsActualUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyIsModelMapped, true)
	common.SetContextKey(c, constant.ContextKeyUpstreamModel, "seedance-2.0-fast-S")
	other := map[string]interface{}{}

	appendMappedModelInfo(c, other)

	require.Equal(t, true, other["is_model_mapped"])
	require.Equal(t, "seedance-2.0-fast-S", other["upstream_model_name"])
}

func TestAppendMappedModelInfoKeepsUnmappedErrorsPrivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyUpstreamModel, "internal-upstream-model")
	other := map[string]interface{}{}

	appendMappedModelInfo(c, other)

	require.Empty(t, other)
}
