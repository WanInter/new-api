package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	canonicalBillingTestModel  = "seedance-2.0-fast-noface"
	canonicalBillingTestSchema = "video.yobox.seedance-2.0.fast-noface.v1"
)

func setupBillingModelControllerTest(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalModes := billing_setting.GetBillingModeCopy()
	originalExprs := billing_setting.GetBillingExprCopy()
	originalSchemas := billing_setting.GetBillingSchemaCopy()
	common.OptionMapRWMutex.RLock()
	originalOptionMap := make(map[string]string, len(common.OptionMap))
	for key, value := range common.OptionMap {
		originalOptionMap[key] = value
	}
	common.OptionMapRWMutex.RUnlock()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Channel{}, &model.Ability{}, &model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	billing_setting.ReplaceBillingSettings(map[string]string{}, map[string]string{}, map[string]string{})
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "billing-admin",
		Status:   common.UserStatusEnabled,
	}).Error)

	t.Cleanup(func() {
		billing_setting.ReplaceBillingSettings(originalModes, originalExprs, originalSchemas)
		model.InvalidatePricingCache()
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createCanonicalBillingTestRoute(t *testing.T, db *gorm.DB, channelID int, channelType int) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id:     channelID,
		Type:   channelType,
		Name:   fmt.Sprintf("channel-%d", channelID),
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     canonicalBillingTestModel,
		ChannelId: channelID,
		Enabled:   true,
	}).Error)
}

func createMappedCanonicalBillingTestRoute(t *testing.T, db *gorm.DB, channelID, channelType int, publicModel, upstreamModel string) {
	t.Helper()
	mappingBytes, err := common.Marshal(map[string]string{publicModel: upstreamModel})
	require.NoError(t, err)
	mapping := string(mappingBytes)
	require.NoError(t, db.Create(&model.Channel{
		Id:           channelID,
		Type:         channelType,
		Name:         fmt.Sprintf("channel-%d", channelID),
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &mapping,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     publicModel,
		ChannelId: channelID,
		Enabled:   true,
	}).Error)
}

func canonicalDurationBranches(resolution string, creditsPerSecond int) string {
	durationBranches := make([]string, 0, 12)
	for seconds := 4; seconds <= 15; seconds++ {
		rawCost := float64(seconds*creditsPerSecond) * 1_000_000 / 20
		durationBranches = append(durationBranches, fmt.Sprintf(
			`param("billing.duration_seconds") == %d ? tier("%s_%ds", %.0f)`,
			seconds,
			resolution,
			seconds,
			rawCost,
		))
	}
	return strings.Join(durationBranches, " : ") + fmt.Sprintf(` : tier("%s_unsupported", 0)`, resolution)
}

func canonicalFastBillingExpression() string {
	return `param("billing.resolution") == "480p" ? (` + canonicalDurationBranches("480p", 3) + `) : (` + canonicalDurationBranches("720p", 4) + `)`
}

func updateBillingModelsRequest(t *testing.T, request BillingModelsUpdateRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(request)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/billing-models", bytes.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("id", 1)
	context.Set("username", "billing-admin")
	UpdateBillingModels(context)
	return recorder
}

func TestUpdateBillingModelsPersistsCanonicalSchemaAtomically(t *testing.T) {
	db := setupBillingModelControllerTest(t)
	createCanonicalBillingTestRoute(t, db, 1, constant.ChannelTypeYobox)

	expression := canonicalFastBillingExpression()
	recorder := updateBillingModelsRequest(t, BillingModelsUpdateRequest{
		BillingMode:   map[string]string{canonicalBillingTestModel: billing_setting.BillingModeTieredExpr},
		BillingExpr:   map[string]string{canonicalBillingTestModel: expression},
		BillingSchema: map[string]string{canonicalBillingTestModel: canonicalBillingTestSchema},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"])
	assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode(canonicalBillingTestModel))
	assert.Equal(t, canonicalBillingTestSchema, billing_setting.GetBillingSchema(canonicalBillingTestModel))
	assert.Equal(t, expression, billing_setting.GetBillingExprCopy()[canonicalBillingTestModel])

	var options []model.Option
	require.NoError(t, db.Find(&options).Error)
	assert.Len(t, options, 3)
}

func TestUpdateBillingModelsClearsDynamicSettingsWhenReturningToRatio(t *testing.T) {
	db := setupBillingModelControllerTest(t)
	createCanonicalBillingTestRoute(t, db, 1, constant.ChannelTypeYobox)

	expression := canonicalFastBillingExpression()
	const legacyModel = "legacy-tiered-model"
	const legacyExpression = `tier("base", 1000000)`
	initial := updateBillingModelsRequest(t, BillingModelsUpdateRequest{
		BillingMode: map[string]string{
			canonicalBillingTestModel: billing_setting.BillingModeTieredExpr,
			legacyModel:               billing_setting.BillingModeTieredExpr,
		},
		BillingExpr: map[string]string{
			canonicalBillingTestModel: expression,
			legacyModel:               legacyExpression,
		},
		BillingSchema: map[string]string{canonicalBillingTestModel: canonicalBillingTestSchema},
	})
	require.Equal(t, http.StatusOK, initial.Code)

	// Clients that only change the mode rely on the API to load the coupled
	// expression and schema maps before normalizing the complete snapshot.
	updated := updateBillingModelsRequest(t, BillingModelsUpdateRequest{
		BillingMode: map[string]string{canonicalBillingTestModel: billing_setting.BillingModeRatio},
	})
	require.Equal(t, http.StatusOK, updated.Code)

	assert.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode(canonicalBillingTestModel))
	assert.NotContains(t, billing_setting.GetBillingExprCopy(), canonicalBillingTestModel)
	assert.Empty(t, billing_setting.GetBillingSchema(canonicalBillingTestModel))
	assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode(legacyModel))
	assert.Equal(t, legacyExpression, billing_setting.GetBillingExprCopy()[legacyModel])
}

func TestUpdateBillingModelsRejectsRawPathAndIncompatibleRoute(t *testing.T) {
	db := setupBillingModelControllerTest(t)
	createCanonicalBillingTestRoute(t, db, 1, constant.ChannelTypeYobox)

	invalidPath := updateBillingModelsRequest(t, BillingModelsUpdateRequest{
		BillingMode:   map[string]string{canonicalBillingTestModel: billing_setting.BillingModeTieredExpr},
		BillingExpr:   map[string]string{canonicalBillingTestModel: `tier("bad", param("duration"))`},
		BillingSchema: map[string]string{canonicalBillingTestModel: canonicalBillingTestSchema},
	})
	require.Equal(t, http.StatusBadRequest, invalidPath.Code)
	assert.Empty(t, billing_setting.GetBillingSchema(canonicalBillingTestModel))

	createCanonicalBillingTestRoute(t, db, 2, constant.ChannelTypeOpenAI)
	incompatibleRoute := updateBillingModelsRequest(t, BillingModelsUpdateRequest{
		BillingMode:   map[string]string{canonicalBillingTestModel: billing_setting.BillingModeTieredExpr},
		BillingExpr:   map[string]string{canonicalBillingTestModel: canonicalFastBillingExpression()},
		BillingSchema: map[string]string{canonicalBillingTestModel: canonicalBillingTestSchema},
	})
	require.Equal(t, http.StatusBadRequest, incompatibleRoute.Code)

	var response map[string]any
	require.NoError(t, common.Unmarshal(incompatibleRoute.Body.Bytes(), &response))
	assert.Contains(t, response["message"], "不能启用规范动态计费")
}

func TestUpdateBillingModelsAcceptsMergedCanonicalSchema(t *testing.T) {
	db := setupBillingModelControllerTest(t)
	const publicModel = "shared-seedance"
	createMappedCanonicalBillingTestRoute(t, db, 1, constant.ChannelTypeDoubaoVideo, publicModel, "bytefor-2.0-real-priority")
	createMappedCanonicalBillingTestRoute(t, db, 2, constant.ChannelTypeSeventhFrame, publicModel, "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78")
	summary, err := relay.GetTaskBillingCapabilitySummary(publicModel)
	require.NoError(t, err)
	require.True(t, summary.Compatible)

	expression := canonicalDurationBranches("shared", 3)
	recorder := updateBillingModelsRequest(t, BillingModelsUpdateRequest{
		BillingMode:   map[string]string{publicModel: billing_setting.BillingModeTieredExpr},
		BillingExpr:   map[string]string{publicModel: expression},
		BillingSchema: map[string]string{publicModel: summary.SchemaVersion},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, summary.SchemaVersion, billing_setting.GetBillingSchema(publicModel))
	assert.Equal(t, expression, billing_setting.GetBillingExprCopy()[publicModel])
}

func TestUpdateOptionRejectsIndividualBillingMapUpdates(t *testing.T) {
	body, err := common.Marshal(OptionUpdateRequest{
		Key:   "billing_setting." + billing_setting.BillingSchemaField,
		Value: `{"seedance-2.0-fast-noface":"video.yobox.seedance-2.0.fast-noface.v1"}`,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	UpdateOption(context)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Contains(t, response["message"], "/api/option/billing-models")
	assert.Error(t, model.UpdateOption(
		"billing_setting."+billing_setting.BillingSchemaField,
		`{"seedance-2.0-fast-noface":"video.yobox.seedance-2.0.fast-noface.v1"}`,
	))
}

func TestGetBillingCapabilitiesReturnsCheckTime(t *testing.T) {
	db := setupBillingModelControllerTest(t)
	createCanonicalBillingTestRoute(t, db, 1, constant.ChannelTypeYobox)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/option/billing-capabilities?model="+canonicalBillingTestModel,
		nil,
	)
	GetBillingCapabilities(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			CheckedAt int64 `json:"checked_at"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Greater(t, response.Data.CheckedAt, int64(0))
}
