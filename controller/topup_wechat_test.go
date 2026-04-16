package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wechatpay"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type topupWechatTestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		EnableWeChatTopup bool             `json:"enable_wechat_topup"`
		WeChatMinTopUp    int              `json:"wechat_min_topup"`
		PayMethods        []map[string]any `json:"pay_methods"`
	} `json:"data"`
}

type wechatPaySettingSnapshot struct {
	Enabled          bool
	MchID            string
	AppID            string
	APIv3Key         string
	PrivateKey       string
	MerchantSerialNo string
	PublicKeyID      string
	PublicKey        string
	MinTopUp         int
	UnitPrice        float64
	NotifyURL        string
	OrderDescription string
	QuotaDisplayType string
	PayMethods       []map[string]string
}

func snapshotWeChatPaySettings() wechatPaySettingSnapshot {
	payMethods := make([]map[string]string, len(operation_setting.PayMethods))
	for i, method := range operation_setting.PayMethods {
		cloned := make(map[string]string, len(method))
		for key, value := range method {
			cloned[key] = value
		}
		payMethods[i] = cloned
	}

	return wechatPaySettingSnapshot{
		Enabled:          setting.WeChatPayEnabled,
		MchID:            setting.WeChatPayMchID,
		AppID:            setting.WeChatPayAppID,
		APIv3Key:         setting.WeChatPayAPIv3Key,
		PrivateKey:       setting.WeChatPayPrivateKey,
		MerchantSerialNo: setting.WeChatPayMerchantSerialNo,
		PublicKeyID:      setting.WeChatPayPublicKeyID,
		PublicKey:        setting.WeChatPayPublicKey,
		MinTopUp:         setting.WeChatPayMinTopUp,
		UnitPrice:        setting.WeChatPayUnitPrice,
		NotifyURL:        setting.WeChatPayNotifyUrl,
		OrderDescription: setting.WeChatPayOrderDescription,
		QuotaDisplayType: operation_setting.GetGeneralSetting().QuotaDisplayType,
		PayMethods:       payMethods,
	}
}

func restoreWeChatPaySettings(snapshot wechatPaySettingSnapshot) {
	setting.WeChatPayEnabled = snapshot.Enabled
	setting.WeChatPayMchID = snapshot.MchID
	setting.WeChatPayAppID = snapshot.AppID
	setting.WeChatPayAPIv3Key = snapshot.APIv3Key
	setting.WeChatPayPrivateKey = snapshot.PrivateKey
	setting.WeChatPayMerchantSerialNo = snapshot.MerchantSerialNo
	setting.WeChatPayPublicKeyID = snapshot.PublicKeyID
	setting.WeChatPayPublicKey = snapshot.PublicKey
	setting.WeChatPayMinTopUp = snapshot.MinTopUp
	setting.WeChatPayUnitPrice = snapshot.UnitPrice
	setting.WeChatPayNotifyUrl = snapshot.NotifyURL
	setting.WeChatPayOrderDescription = snapshot.OrderDescription
	operation_setting.GetGeneralSetting().QuotaDisplayType = snapshot.QuotaDisplayType
	operation_setting.PayMethods = snapshot.PayMethods
}

func setupTopupControllerTestEnv(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(&model.Option{}, &model.User{}, &model.TopUp{}); err != nil {
		t.Fatalf("failed to migrate option table: %v", err)
	}

	snapshot := snapshotWeChatPaySettings()
	t.Cleanup(func() {
		restoreWeChatPaySettings(snapshot)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	operation_setting.PayMethods = []map[string]string{}
}

func seedTopupUser(t *testing.T, id int, group string) {
	t.Helper()

	user := &model.User{
		Id:       id,
		Username: fmt.Sprintf("user-%d", id),
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Email:    fmt.Sprintf("user-%d@example.com", id),
		Group:    group,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
}

func seedWeChatPayConfig() {
	setting.WeChatPayEnabled = true
	setting.WeChatPayUnitPrice = 7.2
	setting.WeChatPayMinTopUp = 1
	setting.WeChatPayMchID = "mch-id"
	setting.WeChatPayAppID = "app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "pem"
	setting.WeChatPayMerchantSerialNo = "serial"
	setting.WeChatPayPublicKeyID = "pub-id"
	setting.WeChatPayPublicKey = "pub"
}

func newTopupTestContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	ctx.Set("id", userID)

	return ctx, recorder
}

func assertSuccessMessage(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	if response.Data != expected {
		t.Fatalf("expected data %q, got %q, body: %s", expected, response.Data, recorder.Body.String())
	}
}

func assertSuccessContains(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			CodeURL string `json:"code_url"`
		} `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	if !strings.Contains(response.Data.CodeURL, expected) {
		t.Fatalf("expected code_url to contain %q, got body: %s", expected, recorder.Body.String())
	}
}

func assertErrorData(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if response.Success {
		t.Fatalf("expected error response, got body: %s", recorder.Body.String())
	}
	if response.Data != expected {
		t.Fatalf("expected error data %q, got %q, body: %s", expected, response.Data, recorder.Body.String())
	}
}

func assertTopupPending(t *testing.T, paymentMethod string) {
	t.Helper()

	var topUp model.TopUp
	if err := model.DB.First(&topUp).Error; err != nil {
		t.Fatalf("failed to query topup: %v", err)
	}
	if topUp.PaymentMethod != paymentMethod {
		t.Fatalf("expected payment_method %q, got %q", paymentMethod, topUp.PaymentMethod)
	}
	if topUp.Status != common.TopUpStatusPending {
		t.Fatalf("expected pending topup, got %q", topUp.Status)
	}
}

func assertTopupStatus(t *testing.T, expected string) {
	t.Helper()

	var topUp model.TopUp
	if err := model.DB.First(&topUp).Error; err != nil {
		t.Fatalf("failed to query topup: %v", err)
	}
	if topUp.Status != expected {
		t.Fatalf("expected topup status %q, got %q", expected, topUp.Status)
	}
}

type fakeWeChatPayClient struct {
	createNativeOrderFunc func(ctx context.Context, req wechatpay.NativeOrderRequest) (*wechatpay.NativeOrderResponse, error)
}

func (f fakeWeChatPayClient) CreateNativeOrder(ctx context.Context, req wechatpay.NativeOrderRequest) (*wechatpay.NativeOrderResponse, error) {
	return f.createNativeOrderFunc(ctx, req)
}

func (f fakeWeChatPayClient) VerifyAndDecryptNotify(ctx context.Context, headers map[string]string, body []byte) (*wechatpay.NotifyResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestGetTopUpInfoIncludesWeChatPayMethodWhenConfigured(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setting.WeChatPayEnabled = true
	setting.WeChatPayMchID = "mch-id"
	setting.WeChatPayAppID = "app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----"
	setting.WeChatPayMerchantSerialNo = "serial-no"
	setting.WeChatPayPublicKeyID = "pub-id"
	setting.WeChatPayPublicKey = "-----BEGIN PUBLIC KEY-----\nkey\n-----END PUBLIC KEY-----"
	setting.WeChatPayMinTopUp = 2

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	var response topupWechatTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	if !response.Data.EnableWeChatTopup {
		t.Fatalf("expected enable_wechat_topup=true, got false, body: %s", recorder.Body.String())
	}
	if response.Data.WeChatMinTopUp != 2 {
		t.Fatalf("expected wechat_min_topup=2, got %d", response.Data.WeChatMinTopUp)
	}

	hasWeChatPay := false
	for _, method := range response.Data.PayMethods {
		methodType, ok := method["type"].(string)
		if ok && methodType == "wechat_pay" {
			hasWeChatPay = true
			break
		}
	}
	if !hasWeChatPay {
		t.Fatalf("expected wechat_pay in pay_methods, body: %s", recorder.Body.String())
	}
}

func TestGetTopUpInfoReturnsScaledWeChatMinTopupInTokenMode(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedWeChatPayConfig()
	setting.WeChatPayMinTopUp = 2
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	var response topupWechatTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}

	expectedMinTopup := int(common.QuotaPerUnit * 2)
	if response.Data.WeChatMinTopUp != expectedMinTopup {
		t.Fatalf("expected wechat_min_topup=%d, got %d", expectedMinTopup, response.Data.WeChatMinTopUp)
	}

	found := false
	for _, method := range response.Data.PayMethods {
		methodType, ok := method["type"].(string)
		if !ok || methodType != "wechat_pay" {
			continue
		}
		found = true
		minTopup, ok := method["min_topup"].(string)
		if !ok {
			t.Fatalf("expected min_topup string in wechat_pay method, body: %s", recorder.Body.String())
		}
		if minTopup != fmt.Sprintf("%d", expectedMinTopup) {
			t.Fatalf("expected wechat pay min_topup=%d, got %s", expectedMinTopup, minTopup)
		}
	}
	if !found {
		t.Fatalf("expected wechat_pay in pay_methods, body: %s", recorder.Body.String())
	}
}

func TestGetTopUpInfoDoesNotExposeWeChatPayWhenConfigIncomplete(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setting.WeChatPayEnabled = true
	setting.WeChatPayMchID = "mch-id"
	setting.WeChatPayAppID = "app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----"
	setting.WeChatPayMerchantSerialNo = "serial-no"
	setting.WeChatPayPublicKeyID = "pub-id"
	// 故意留空 PublicKey，配置不完整
	setting.WeChatPayPublicKey = ""
	setting.WeChatPayMinTopUp = 2

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	var response topupWechatTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	if response.Data.EnableWeChatTopup {
		t.Fatalf("expected enable_wechat_topup=false when config incomplete, body: %s", recorder.Body.String())
	}
	for _, method := range response.Data.PayMethods {
		methodType, ok := method["type"].(string)
		if ok && methodType == "wechat_pay" {
			t.Fatalf("did not expect wechat_pay in pay_methods when config incomplete, body: %s", recorder.Body.String())
		}
	}
}

func TestRequestWeChatAmountReturnsCNYAmount(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	setting.WeChatPayEnabled = true
	setting.WeChatPayUnitPrice = 7.2
	setting.WeChatPayMinTopUp = 1
	setting.WeChatPayMchID = "mch-id"
	setting.WeChatPayAppID = "app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "pem"
	setting.WeChatPayMerchantSerialNo = "serial"
	setting.WeChatPayPublicKeyID = "pub-id"
	setting.WeChatPayPublicKey = "pub"

	ctx, recorder := newTopupTestContext(t, "POST", "/api/user/wechat/amount", map[string]any{"amount": 10}, 1)
	RequestWeChatAmount(ctx)

	assertSuccessMessage(t, recorder, "72")
}

func TestRequestWeChatAmountRejectsBelowTokenModeMinTopup(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedWeChatPayConfig()
	setting.WeChatPayMinTopUp = 2
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	ctx, recorder := newTopupTestContext(t, "POST", "/api/user/wechat/amount", map[string]any{
		"amount": int64(common.QuotaPerUnit*2) - 1,
	}, 1)
	RequestWeChatAmount(ctx)

	assertErrorData(t, recorder, "充值数量不能小于 1000000")
}

func TestRequestWeChatPayCreatesPendingOrderAndReturnsCodeURL(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedWeChatPayConfig()

	originalFactory := newWeChatPayClient
	newWeChatPayClient = func() (wechatpay.Client, error) {
		return fakeWeChatPayClient{
			createNativeOrderFunc: func(_ context.Context, req wechatpay.NativeOrderRequest) (*wechatpay.NativeOrderResponse, error) {
				if req.AmountFen != 7200 {
					t.Fatalf("expected 7200 fen, got %d", req.AmountFen)
				}
				return &wechatpay.NativeOrderResponse{CodeURL: "weixin://wxpay/mock-code-url"}, nil
			},
		}, nil
	}
	defer func() { newWeChatPayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, "POST", "/api/user/wechat/pay", map[string]any{
		"amount":         10,
		"payment_method": "wechat_pay",
	}, 1)
	RequestWeChatPay(ctx)

	assertSuccessContains(t, recorder, "mock-code-url")
	assertTopupPending(t, "wechat_pay")
}

func TestRequestWeChatPayMarksOrderFailedWhenCreateNativeOrderFails(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedWeChatPayConfig()

	originalFactory := newWeChatPayClient
	newWeChatPayClient = func() (wechatpay.Client, error) {
		return fakeWeChatPayClient{
			createNativeOrderFunc: func(_ context.Context, req wechatpay.NativeOrderRequest) (*wechatpay.NativeOrderResponse, error) {
				return nil, fmt.Errorf("upstream failed")
			},
		}, nil
	}
	defer func() { newWeChatPayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, "POST", "/api/user/wechat/pay", map[string]any{
		"amount":         10,
		"payment_method": "wechat_pay",
	}, 1)
	RequestWeChatPay(ctx)

	assertErrorData(t, recorder, "拉起支付失败")
	assertTopupStatus(t, common.TopUpStatusFailed)
}
