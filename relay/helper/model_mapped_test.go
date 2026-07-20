package helper

import (
	"net/http/httptest"
	"testing"

	common "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperRecordsMappedModelForErrorLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"public-model":"upstream-model"}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "public-model",
		},
	}

	require.NoError(t, ModelMappedHelper(c, info, nil))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyIsModelMapped))
	require.Equal(t, "upstream-model", common.GetContextKeyString(c, constant.ContextKeyUpstreamModel))
}
