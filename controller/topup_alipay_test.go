package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	alipaypkg "github.com/QuantumNous/new-api/pkg/alipay"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type alipaySettingSnapshot struct {
	Enabled               bool
	Sandbox               bool
	AppID                 string
	PrivateKey            string
	PublicKey             string
	UnitPrice             float64
	MinTopUp              int
	NotifyURL             string
	ReturnURL             string
	SubscriptionReturnURL string
	OrderDescription      string
	PayMode               string
}

func snapshotAlipaySettings() alipaySettingSnapshot {
	return alipaySettingSnapshot{
		Enabled:               setting.AlipayEnabled,
		Sandbox:               setting.AlipaySandbox,
		AppID:                 setting.AlipayAppID,
		PrivateKey:            setting.AlipayPrivateKey,
		PublicKey:             setting.AlipayPublicKey,
		UnitPrice:             setting.AlipayUnitPrice,
		MinTopUp:              setting.AlipayMinTopUp,
		NotifyURL:             setting.AlipayNotifyURL,
		ReturnURL:             setting.AlipayReturnURL,
		SubscriptionReturnURL: setting.AlipaySubscriptionReturnURL,
		OrderDescription:      setting.AlipayOrderDescription,
		PayMode:               setting.AlipayPayMode,
	}
}

func restoreAlipaySettings(snapshot alipaySettingSnapshot) {
	setting.AlipayEnabled = snapshot.Enabled
	setting.AlipaySandbox = snapshot.Sandbox
	setting.AlipayAppID = snapshot.AppID
	setting.AlipayPrivateKey = snapshot.PrivateKey
	setting.AlipayPublicKey = snapshot.PublicKey
	setting.AlipayUnitPrice = snapshot.UnitPrice
	setting.AlipayMinTopUp = snapshot.MinTopUp
	setting.AlipayNotifyURL = snapshot.NotifyURL
	setting.AlipayReturnURL = snapshot.ReturnURL
	setting.AlipaySubscriptionReturnURL = snapshot.SubscriptionReturnURL
	setting.AlipayOrderDescription = snapshot.OrderDescription
	setting.AlipayPayMode = snapshot.PayMode
}

func setupAlipaySettingIsolation(t *testing.T) {
	t.Helper()
	snapshot := snapshotAlipaySettings()
	t.Cleanup(func() {
		restoreAlipaySettings(snapshot)
	})
}

func seedSubscriptionPlan(t *testing.T) *model.SubscriptionPlan {
	t.Helper()

	plan := &model.SubscriptionPlan{
		Title:         "Alipay Plan",
		PriceAmount:   7.2,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	if err := model.DB.Create(plan).Error; err != nil {
		t.Fatalf("failed to create subscription plan: %v", err)
	}
	return plan
}

func setupSubscriptionOrderTestEnv(t *testing.T) {
	t.Helper()

	setupTopupControllerTestEnv(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}); err != nil {
		t.Fatalf("failed to migrate subscription tables: %v", err)
	}
}

func TestGetTopUpInfoPrefersAlipayDirectOverLegacyAlipay(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	operation_setting.PayMethods = []map[string]string{{
		"name": "支付宝",
		"type": "alipay",
	}}
	setting.AlipayEnabled = true
	setting.AlipayAppID = "2026000000000000"
	setting.AlipayPrivateKey = "private-key"
	setting.AlipayPublicKey = "public-key"
	setting.AlipayMinTopUp = 3

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	body := recorder.Body.String()
	if strings.Contains(body, `"type":"alipay"`) {
		t.Fatalf("expected legacy alipay to be hidden: %s", body)
	}
	if !strings.Contains(body, `"type":"alipay_direct"`) {
		t.Fatalf("expected alipay_direct in response: %s", body)
	}
}

func TestGetTopUpInfoDoesNotDuplicateAlipayDirect(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)

	operation_setting.PayMethods = []map[string]string{
		{
			"name": "支付宝",
			"type": "alipay",
		},
		{
			"name":      "支付宝直连",
			"type":      "alipay_direct",
			"min_topup": "3",
		},
	}
	setting.AlipayEnabled = true
	setting.AlipayAppID = "2026000000000000"
	setting.AlipayPrivateKey = "private-key"
	setting.AlipayPublicKey = "public-key"
	setting.AlipayMinTopUp = 3

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			PayMethods []map[string]string `json:"pay_methods"`
		} `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}

	alipayDirectCount := 0
	alipayCount := 0
	for _, method := range response.Data.PayMethods {
		switch method["type"] {
		case "alipay":
			alipayCount++
		case "alipay_direct":
			alipayDirectCount++
		}
	}
	if alipayCount != 0 {
		t.Fatalf("expected no legacy alipay in pay_methods, got body: %s", recorder.Body.String())
	}
	if alipayDirectCount != 1 {
		t.Fatalf("expected exactly one alipay_direct, got %d, body: %s", alipayDirectCount, recorder.Body.String())
	}
}

func TestTopUpPersistsPaymentModeAndProviderPayload(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")

	order := &model.TopUp{
		UserId:          1,
		Amount:          10,
		Money:           7.2,
		TradeNo:         "ALIPAY-TOPUP-1",
		PaymentMethod:   "alipay_direct",
		PaymentMode:     "qr",
		ProviderPayload: `{"source":"query"}`,
		Status:          common.TopUpStatusPending,
	}
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("failed to create order: %v", err)
	}

	var saved model.TopUp
	if err := model.DB.First(&saved, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatalf("failed to query order: %v", err)
	}
	if saved.PaymentMode != "qr" || saved.ProviderPayload == "" {
		t.Fatalf("unexpected saved order: %+v", saved)
	}
}

func TestCompleteSubscriptionOrderSyncsMetadataToCreatedTopUp(t *testing.T) {
	setupSubscriptionOrderTestEnv(t)
	seedTopupUser(t, 1, "default")
	plan := seedSubscriptionPlan(t)

	order := &model.SubscriptionOrder{
		UserId:        1,
		PlanId:        plan.Id,
		Money:         7.2,
		TradeNo:       "SUB-ALIPAY-CREATE-1",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "qr",
		Status:        common.TopUpStatusPending,
		CreateTime:    common.GetTimestamp(),
	}
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("failed to create subscription order: %v", err)
	}

	notifyPayload := `{"source":"notify"}`
	if err := model.CompleteSubscriptionOrder(order.TradeNo, notifyPayload); err != nil {
		t.Fatalf("failed to complete subscription order: %v", err)
	}

	var topUp model.TopUp
	if err := model.DB.First(&topUp, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatalf("failed to query topup: %v", err)
	}
	if topUp.PaymentMode != "qr" {
		t.Fatalf("expected payment_mode qr, got %q", topUp.PaymentMode)
	}
	if topUp.ProviderPayload != notifyPayload {
		t.Fatalf("expected provider_payload %s, got %s", notifyPayload, topUp.ProviderPayload)
	}

	var savedOrder model.SubscriptionOrder
	if err := model.DB.First(&savedOrder, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatalf("failed to query subscription order: %v", err)
	}
	if savedOrder.ProviderPayload != notifyPayload {
		t.Fatalf("expected subscription order provider_payload %s, got %s", notifyPayload, savedOrder.ProviderPayload)
	}
}

func TestCompleteSubscriptionOrderSyncsMetadataToExistingTopUp(t *testing.T) {
	setupSubscriptionOrderTestEnv(t)
	seedTopupUser(t, 1, "default")
	plan := seedSubscriptionPlan(t)

	orderPayload := `{"source":"order"}`
	order := &model.SubscriptionOrder{
		UserId:          1,
		PlanId:          plan.Id,
		Money:           8.8,
		TradeNo:         "SUB-ALIPAY-UPDATE-1",
		PaymentMethod:   "alipay_direct",
		PaymentMode:     "app",
		ProviderPayload: orderPayload,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("failed to create subscription order: %v", err)
	}

	existingTopUp := &model.TopUp{
		UserId:        1,
		Amount:        0,
		Money:         order.Money,
		TradeNo:       order.TradeNo,
		PaymentMethod: order.PaymentMethod,
		Status:        common.TopUpStatusPending,
		CreateTime:    order.CreateTime,
	}
	if err := model.DB.Create(existingTopUp).Error; err != nil {
		t.Fatalf("failed to create existing topup: %v", err)
	}

	if err := model.CompleteSubscriptionOrder(order.TradeNo, ""); err != nil {
		t.Fatalf("failed to complete subscription order: %v", err)
	}

	var topUp model.TopUp
	if err := model.DB.First(&topUp, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatalf("failed to query topup: %v", err)
	}
	if topUp.PaymentMode != "app" {
		t.Fatalf("expected payment_mode app, got %q", topUp.PaymentMode)
	}
	if topUp.ProviderPayload != orderPayload {
		t.Fatalf("expected provider_payload %s, got %s", orderPayload, topUp.ProviderPayload)
	}
}

type fakeAlipayClient struct {
	createPageOrderFunc func(ctx context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.PageOrderResponse, error)
	createQROrderFunc   func(ctx context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.QROrderResponse, error)
	queryOrderFunc      func(ctx context.Context, outTradeNo string) (*alipaypkg.QueryOrderResult, error)
	verifyNotifyFunc    func(values url.Values) (*alipaypkg.NotificationResult, error)
}

func (f fakeAlipayClient) CreatePageOrder(ctx context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.PageOrderResponse, error) {
	if f.createPageOrderFunc == nil {
		return nil, fmt.Errorf("createPageOrderFunc is nil")
	}
	return f.createPageOrderFunc(ctx, req)
}

func (f fakeAlipayClient) CreateQROrder(ctx context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.QROrderResponse, error) {
	if f.createQROrderFunc == nil {
		return nil, fmt.Errorf("createQROrderFunc is nil")
	}
	return f.createQROrderFunc(ctx, req)
}

func (f fakeAlipayClient) QueryOrder(ctx context.Context, outTradeNo string) (*alipaypkg.QueryOrderResult, error) {
	if f.queryOrderFunc == nil {
		return nil, fmt.Errorf("queryOrderFunc is nil")
	}
	return f.queryOrderFunc(ctx, outTradeNo)
}

func (f fakeAlipayClient) VerifyNotification(values url.Values) (*alipaypkg.NotificationResult, error) {
	if f.verifyNotifyFunc == nil {
		return nil, fmt.Errorf("verifyNotifyFunc is nil")
	}
	return f.verifyNotifyFunc(values)
}

func seedAlipayConfig() {
	setting.AlipayEnabled = true
	setting.AlipayAppID = "2026000000000000"
	setting.AlipayPrivateKey = "private-key"
	setting.AlipayPublicKey = "public-key"
	setting.AlipayUnitPrice = 7.2
	setting.AlipayMinTopUp = 1
	setting.AlipayOrderDescription = "账户充值"
}

func newAlipayNotifyContext(t *testing.T, form url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/alipay/notify", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ctx, recorder
}

func TestRequestAlipayPayReturnsPageURL(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			createPageOrderFunc: func(_ context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.PageOrderResponse, error) {
				if req.OutTradeNo == "" {
					t.Fatal("expected out trade no")
				}
				return &alipaypkg.PageOrderResponse{PayURL: "https://openapi.alipay.com/gateway.do?foo=bar"}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, http.MethodPost, "/api/user/alipay/pay", map[string]any{
		"amount":         10,
		"payment_method": "alipay_direct",
		"pay_mode":       "page",
	}, 1)
	RequestAlipayPay(ctx)

	if !strings.Contains(recorder.Body.String(), "pay_url") {
		t.Fatalf("expected pay_url in response: %s", recorder.Body.String())
	}
}

func TestQueryAlipayPayMarksSuccessAfterTradeQuery(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedAlipayConfig()

	topUp := &model.TopUp{
		UserId:        1,
		Amount:        10,
		Money:         72.0,
		TradeNo:       "ALIPAY-TOPUP-Q-1",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "qr",
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := model.DB.Create(topUp).Error; err != nil {
		t.Fatalf("failed to seed topup: %v", err)
	}

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			queryOrderFunc: func(_ context.Context, outTradeNo string) (*alipaypkg.QueryOrderResult, error) {
				if outTradeNo != topUp.TradeNo {
					t.Fatalf("unexpected out trade no: %s", outTradeNo)
				}
				return &alipaypkg.QueryOrderResult{
					OutTradeNo:     outTradeNo,
					TradeNo:        "ALI-QUERY-1",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("72.00"),
					BuyerPayAmount: decimal.RequireFromString("72.00"),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, http.MethodPost, "/api/user/alipay/query", map[string]any{"trade_no": topUp.TradeNo}, 1)
	QueryAlipayPay(ctx)

	if !strings.Contains(recorder.Body.String(), `"status":"success"`) {
		t.Fatalf("expected success status in response: %s", recorder.Body.String())
	}
	assertTopupNotifyStatus(t, topUp.TradeNo, common.TopUpStatusSuccess)
}

func TestGetTopUpInfoReturnsScaledAlipayMinTopupInTokenMode(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedAlipayConfig()
	setting.AlipayMinTopUp = 2
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	ctx, recorder := newTopupTestContext(t, http.MethodGet, "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			AlipayMinTopUp int              `json:"alipay_min_topup"`
			PayMethods     []map[string]any `json:"pay_methods"`
		} `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	expectedMinTopup := int(common.QuotaPerUnit * 2)
	if response.Data.AlipayMinTopUp != expectedMinTopup {
		t.Fatalf("expected alipay_min_topup=%d, got %d", expectedMinTopup, response.Data.AlipayMinTopUp)
	}
	found := false
	for _, method := range response.Data.PayMethods {
		methodType, ok := method["type"].(string)
		if !ok || methodType != "alipay_direct" {
			continue
		}
		found = true
		minTopup, ok := method["min_topup"].(string)
		if !ok {
			t.Fatalf("expected min_topup string in alipay_direct method, body: %s", recorder.Body.String())
		}
		if minTopup != fmt.Sprintf("%d", expectedMinTopup) {
			t.Fatalf("expected alipay_direct min_topup=%d, got %s", expectedMinTopup, minTopup)
		}
	}
	if !found {
		t.Fatalf("expected alipay_direct in pay_methods, body: %s", recorder.Body.String())
	}
}

func TestRequestAlipayPayKeepsPendingWhenCreatePageOrderFails(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			createPageOrderFunc: func(_ context.Context, _ alipaypkg.CreateOrderRequest) (*alipaypkg.PageOrderResponse, error) {
				return nil, fmt.Errorf("upstream timeout")
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, http.MethodPost, "/api/user/alipay/pay", map[string]any{
		"amount":         10,
		"payment_method": "alipay_direct",
		"pay_mode":       "page",
	}, 1)
	RequestAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), "拉起支付失败") {
		t.Fatalf("expected upstream error response, got: %s", recorder.Body.String())
	}
	assertTopupPending(t, "alipay_direct")
}

func TestQueryAlipayPayReturnsErrorWhenTradeClosed(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedAlipayConfig()

	topUp := &model.TopUp{
		UserId:        1,
		Amount:        10,
		Money:         72.0,
		TradeNo:       "ALIPAY-TOPUP-Q-CLOSED",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "qr",
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := model.DB.Create(topUp).Error; err != nil {
		t.Fatalf("failed to seed topup: %v", err)
	}

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			queryOrderFunc: func(_ context.Context, outTradeNo string) (*alipaypkg.QueryOrderResult, error) {
				return &alipaypkg.QueryOrderResult{
					OutTradeNo:     outTradeNo,
					TradeNo:        "ALI-QUERY-CLOSED",
					TradeStatus:    "TRADE_CLOSED",
					TotalAmount:    decimal.RequireFromString("72.00"),
					BuyerPayAmount: decimal.RequireFromString("0.00"),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, http.MethodPost, "/api/user/alipay/query", map[string]any{"trade_no": topUp.TradeNo}, 1)
	QueryAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), "订单已关闭") {
		t.Fatalf("expected closed-order error, got: %s", recorder.Body.String())
	}
	assertTopupNotifyStatus(t, topUp.TradeNo, common.TopUpStatusPending)
}

func TestRequestAlipayPayUsesConfiguredMode(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedAlipayConfig()
	setting.AlipayPayMode = "page"

	pageCalled := false
	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			createPageOrderFunc: func(_ context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.PageOrderResponse, error) {
				pageCalled = true
				return &alipaypkg.PageOrderResponse{PayURL: "https://openapi.alipay.com/gateway.do?foo=bar"}, nil
			},
			createQROrderFunc: func(_ context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.QROrderResponse, error) {
				t.Fatal("should not call QR order when AlipayPayMode=page")
				return nil, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, http.MethodPost, "/api/user/alipay/pay", map[string]any{
		"amount":         10,
		"payment_method": "alipay_direct",
		"pay_mode":       "qr",
	}, 1)
	RequestAlipayPay(ctx)
	if !pageCalled {
		t.Fatal("expected page order to be used from configured mode")
	}
	if !strings.Contains(recorder.Body.String(), `"pay_mode":"page"`) {
		t.Fatalf("expected response pay_mode=page, got %s", recorder.Body.String())
	}
}

func TestGetTopUpInfoIncludesAlipayPayMode(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedAlipayConfig()
	setting.AlipayPayMode = "qr"

	ctx, recorder := newTopupTestContext(t, http.MethodGet, "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)
	if !strings.Contains(recorder.Body.String(), `"alipay_pay_mode":"qr"`) {
		t.Fatalf("expected alipay_pay_mode=qr, got %s", recorder.Body.String())
	}
}
