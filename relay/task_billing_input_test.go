package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskBillingInputTestAdaptor struct {
	channel.TaskAdaptor
	capability *channel.TaskBillingCapability
	input      billingexpr.RequestInput
}

func (a *taskBillingInputTestAdaptor) GetTaskBillingCapability(_ *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	return a.capability
}

func (a *taskBillingInputTestAdaptor) BuildBillingInput(_ *gin.Context, _ *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	return a.input, nil
}

func withTaskBillingSettings(t *testing.T, modes, exprs, schemas map[string]string) {
	t.Helper()
	savedModes, savedExprs, savedSchemas := billing_setting.GetBillingSettingsCopy()
	billing_setting.ReplaceBillingSettings(modes, exprs, schemas)
	t.Cleanup(func() {
		billing_setting.ReplaceBillingSettings(savedModes, savedExprs, savedSchemas)
	})
}

func TestPrepareTaskBillingRequestInputRejectsRawPathAtRelayTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const modelName = "canonical-relay-test"
	const schemaVersion = "test.video.v1"
	withTaskBillingSettings(t,
		map[string]string{modelName: billing_setting.BillingModeTieredExpr},
		map[string]string{modelName: `tier("unsafe", param("duration"))`},
		map[string]string{modelName: schemaVersion},
	)

	adaptor := &taskBillingInputTestAdaptor{
		capability: &channel.TaskBillingCapability{
			SchemaVersion: schemaVersion,
			Fields: []channel.TaskBillingField{{
				Path:       "billing.duration_seconds",
				Type:       "number",
				Required:   true,
				EnumValues: []string{"4"},
			}},
		},
		input: billingexpr.RequestInput{Body: []byte(`{"billing":{"duration_seconds":4}}`)},
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: modelName}
	err := prepareTaskBillingRequestInput(context, info, adaptor, modelName)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a canonical billing field")
	assert.Nil(t, info.BillingRequestInput)
}

func TestPrepareTaskBillingRequestInputFreezesBillingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const modelName = "canonical-relay-test"
	const schemaVersion = "test.video.v1"
	const expression = `tier("fixed", 1000000)`
	withTaskBillingSettings(t,
		map[string]string{modelName: billing_setting.BillingModeTieredExpr},
		map[string]string{modelName: expression},
		map[string]string{modelName: schemaVersion},
	)

	adaptor := &taskBillingInputTestAdaptor{
		capability: &channel.TaskBillingCapability{
			SchemaVersion: schemaVersion,
			Fields: []channel.TaskBillingField{{
				Path:       "billing.duration_seconds",
				Type:       "number",
				Required:   true,
				EnumValues: []string{"4"},
			}},
		},
		input: billingexpr.RequestInput{Body: []byte(`{"billing":{"duration_seconds":4}}`)},
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: modelName}
	require.NoError(t, prepareTaskBillingRequestInput(context, info, adaptor, modelName))
	require.NotNil(t, info.TaskBillingConfig)
	assert.Equal(t, expression, info.TaskBillingConfig.Expr)
	assert.Equal(t, schemaVersion, info.TaskBillingConfig.Schema)

	billing_setting.ReplaceBillingSettings(
		map[string]string{modelName: billing_setting.BillingModeTieredExpr},
		map[string]string{modelName: `tier("new", 5000000)`},
		map[string]string{modelName: schemaVersion},
	)
	assert.Equal(t, expression, info.TaskBillingConfig.Expr)
}

func TestPrepareTaskBillingRequestInputAcceptsMergedModelSchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTaskBillingCapabilityTestDB(t)
	const modelName = "shared-seedance"
	createTaskBillingCapabilityRoute(t, db, 1, constant.ChannelTypeDoubaoVideo, modelName, "bytefor-2.0-real-priority")
	createTaskBillingCapabilityRoute(t, db, 2, constant.ChannelTypeSeventhFrame, modelName, "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78")
	summary, err := GetTaskBillingCapabilitySummary(modelName)
	require.NoError(t, err)
	require.True(t, summary.Compatible)

	withTaskBillingSettings(t,
		map[string]string{modelName: billing_setting.BillingModeTieredExpr},
		map[string]string{modelName: `tier("fixed", 1000000)`},
		map[string]string{modelName: summary.SchemaVersion},
	)
	providerCapability := taskBillingTestCapability("video.provider.duration.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"},
	})
	adaptor := &taskBillingInputTestAdaptor{
		capability: providerCapability,
		input:      billingexpr.RequestInput{Body: []byte(`{"billing":{"duration_seconds":5}}`)},
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "bytefor-2.0-real-priority",
		},
	}

	require.NoError(t, prepareTaskBillingRequestInput(context, info, adaptor, modelName))
	require.NotNil(t, info.BillingRequestInput)
	assert.Equal(t, CanonicalBillingFields(&channel.TaskBillingCapability{
		SchemaVersion: summary.SchemaVersion,
		Fields:        summary.Fields,
	}), info.BillingCanonicalFields)
}
