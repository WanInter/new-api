package helper

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

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
