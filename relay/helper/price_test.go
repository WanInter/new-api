package helper

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func TestHandleGroupRatioUsesPreciseRuleAfterAutoGroupSelection(t *testing.T) {
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.ModelPricingRule{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		_ = model.DeleteModelPricingRule(1)
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, db.Create(&model.User{Id: 17, Username: "pricing-test"}).Error)
	rule := &model.ModelPricingRule{
		SubjectType:  model.ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		UsingGroup:   "creative-video",
		Ratio:        0.9,
		Enabled:      true,
	}
	require.NoError(t, model.CreateModelPricingRule(rule))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("auto_group", "creative-video")
	info := &relaycommon.RelayInfo{
		UserId:          17,
		UserGroup:       "vip_9折",
		OriginModelName: "seedance2.0",
		UsingGroup:      "auto",
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 100,
			EstimatedQuotaAfterGroup:  125,
		},
	}

	ratioInfo := HandleGroupRatio(ctx, info)
	assert.Equal(t, "creative-video", info.UsingGroup)
	assert.Equal(t, 0.9, ratioInfo.GroupRatio)
	assert.True(t, ratioInfo.HasSpecialRatio)
	assert.Equal(t, rule.Id, ratioInfo.PricingRuleId)
	assert.Equal(t, "model_pricing_rule", ratioInfo.PricingRuleSource)
	assert.Equal(t, 0.9, info.TieredBillingSnapshot.GroupRatio)
	assert.Equal(t, 90, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
}

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

func TestModelPriceHelperPerCallUsesTieredExpression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 20
	t.Cleanup(func() { common.QuotaPerUnit = savedQuotaPerUnit })

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"task-tiered-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"task-tiered-model":"param(\"duration\") == \"10\" ? tier(\"10s\", 10000000) : tier(\"base\", 5000000)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "task-tiered-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"duration":"10"}`),
		},
	}

	priceData, err := ModelPriceHelperPerCall(ctx, info)
	require.NoError(t, err)
	require.Equal(t, 200, priceData.Quota)
	require.Equal(t, 200, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "10s", info.TieredBillingSnapshot.EstimatedTier)
}

func TestModelPriceHelperPerCallUsesFrozenTaskBillingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const modelName = "task-tiered-frozen-config"
	savedModes, savedExprs, savedSchemas := billing_setting.GetBillingSettingsCopy()
	savedQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 20
	billing_setting.ReplaceBillingSettings(
		map[string]string{modelName: billing_setting.BillingModeTieredExpr},
		map[string]string{modelName: `tier("new", 5000000)`},
		map[string]string{},
	)
	t.Cleanup(func() {
		billing_setting.ReplaceBillingSettings(savedModes, savedExprs, savedSchemas)
		common.QuotaPerUnit = savedQuotaPerUnit
	})

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	context.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
		TaskBillingConfig: &relaycommon.TaskBillingConfigSnapshot{
			ModelName: modelName,
			Mode:      billing_setting.BillingModeTieredExpr,
			Expr:      `tier("frozen", 1000000)`,
		},
	}

	priceData, err := ModelPriceHelperPerCall(context, info)

	require.NoError(t, err)
	assert.Equal(t, 20, priceData.Quota)
	require.NotNil(t, info.TieredBillingSnapshot)
	assert.Equal(t, `tier("frozen", 1000000)`, info.TieredBillingSnapshot.ExprString)
	assert.Equal(t, "frozen", info.TieredBillingSnapshot.EstimatedTier)
}

func TestModelPriceHelperTieredUsesMultipartImageEditFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	savedQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 20
	t.Cleanup(func() { common.QuotaPerUnit = savedQuotaPerUnit })

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	const model = "gpt-image-2-vibe-4k"
	const expr = `param("size") == "2048x3072" && param("quality") == "high" ? tier("4k_high", 400000) : tier("fallback", 100000)`
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"gpt-image-2-vibe-4k":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"gpt-image-2-vibe-4k":"param(\"size\") == \"2048x3072\" && param(\"quality\") == \"high\" ? tier(\"4k_high\", 400000) : tier(\"fallback\", 100000)"}`,
	}))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", model))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("size", "2048x3072"))
	require.NoError(t, writer.WriteField("quality", "high"))
	filePart, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = filePart.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Set("group", "default")

	request, err := GetAndValidOpenAIImageRequest(ctx, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		OriginModelName: model,
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": writer.FormDataContentType()},
		Request:         request,
	}

	priceData, err := ModelPriceHelper(ctx, info, 0, request.GetTokenCountMeta())
	require.NoError(t, err)
	assert.Equal(t, 8, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	assert.Equal(t, "4k_high", info.TieredBillingSnapshot.EstimatedTier)
	assert.Equal(t, expr, info.TieredBillingSnapshot.ExprString)
	require.NotNil(t, info.BillingRequestInput)
	assert.Equal(t, "2048x3072", gjson.GetBytes(info.BillingRequestInput.Body, "size").String())
	assert.Equal(t, "high", gjson.GetBytes(info.BillingRequestInput.Body, "quality").String())
}
