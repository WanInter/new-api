package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func setupSubscriptionAlipayControllerTestEnv(t *testing.T) {
	t.Helper()
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}); err != nil {
		t.Fatalf("failed to migrate subscription tables: %v", err)
	}
}

func seedSubscriptionAlipayUser(t *testing.T, id int) {
	t.Helper()
	user := &model.User{
		Id:       id,
		Username: fmt.Sprintf("sub-user-%d", id),
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Email:    fmt.Sprintf("sub-user-%d@example.com", id),
		Group:    "default",
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
}

func seedSubscriptionAlipayPlan(t *testing.T, price float64) *model.SubscriptionPlan {
	t.Helper()
	plan := &model.SubscriptionPlan{
		Title:         "Plan-1",
		PriceAmount:   price,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	if err := model.DB.Create(plan).Error; err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}
	return plan
}

func seedPendingSubscriptionAlipayOrder(t *testing.T, order *model.SubscriptionOrder) {
	t.Helper()
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("failed to create subscription order: %v", err)
	}
}

func newSubscriptionAlipayTestContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	payload, err := common.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", userID)
	return ctx, recorder
}

func TestSubscriptionRequestAlipayPayReturnsQRCode(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	plan := seedSubscriptionAlipayPlan(t, 88)
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			createQROrderFunc: func(_ context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.QROrderResponse, error) {
				if req.TotalAmount.StringFixed(2) != "88.00" {
					t.Fatalf("expected 88.00, got %s", req.TotalAmount.StringFixed(2))
				}
				return &alipaypkg.QROrderResponse{QRCode: "https://qr.alipay.com/test"}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newSubscriptionAlipayTestContext(t, http.MethodPost, "/api/subscription/alipay/pay", map[string]any{
		"plan_id":        plan.Id,
		"payment_method": "alipay_direct",
		"pay_mode":       "qr",
	}, 1)
	SubscriptionRequestAlipayPay(ctx)

	if !strings.Contains(recorder.Body.String(), "qr_code") {
		t.Fatalf("expected qr_code in response: %s", recorder.Body.String())
	}
}

func TestSubscriptionQueryAlipayPayReturnsSuccess(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	seedAlipayConfig()
	plan := seedSubscriptionAlipayPlan(t, 88)
	seedPendingSubscriptionAlipayOrder(t, &model.SubscriptionOrder{
		UserId:        1,
		PlanId:        plan.Id,
		Money:         88,
		TradeNo:       "ALIPAY-SUB-Q-1",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "qr",
		Status:        common.TopUpStatusPending,
		CreateTime:    common.GetTimestamp(),
	})

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			queryOrderFunc: func(_ context.Context, outTradeNo string) (*alipaypkg.QueryOrderResult, error) {
				return &alipaypkg.QueryOrderResult{
					OutTradeNo:     outTradeNo,
					TradeNo:        "ALI-SUB-QUERY-1",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("88.00"),
					BuyerPayAmount: decimal.RequireFromString("88.00"),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newSubscriptionAlipayTestContext(t, http.MethodPost, "/api/subscription/alipay/query", map[string]any{"trade_no": "ALIPAY-SUB-Q-1"}, 1)
	SubscriptionQueryAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), `"status":"success"`) {
		t.Fatalf("expected success response: %s", recorder.Body.String())
	}
}

func TestSubscriptionRequestAlipayPayUsesSubscriptionNotifyURL(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	plan := seedSubscriptionAlipayPlan(t, 88)
	seedAlipayConfig()
	setting.AlipayNotifyURL = "https://example.com/api/alipay/notify"

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			createQROrderFunc: func(_ context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.QROrderResponse, error) {
				if !strings.Contains(req.NotifyURL, "/api/subscription/alipay/notify") {
					t.Fatalf("expected subscription notify url, got %s", req.NotifyURL)
				}
				if strings.Contains(req.NotifyURL, "/api/alipay/notify") && !strings.Contains(req.NotifyURL, "/api/subscription/alipay/notify") {
					t.Fatalf("expected subscription notify url instead of topup notify url, got %s", req.NotifyURL)
				}
				return &alipaypkg.QROrderResponse{QRCode: "https://qr.alipay.com/test"}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newSubscriptionAlipayTestContext(t, http.MethodPost, "/api/subscription/alipay/pay", map[string]any{
		"plan_id":        plan.Id,
		"payment_method": "alipay_direct",
		"pay_mode":       "qr",
	}, 1)
	SubscriptionRequestAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), "qr_code") {
		t.Fatalf("expected qr_code in response: %s", recorder.Body.String())
	}
}

func TestSubscriptionRequestAlipayPayRejectsDisabledAlipay(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	plan := seedSubscriptionAlipayPlan(t, 88)
	seedAlipayConfig()
	setting.AlipayEnabled = false

	ctx, recorder := newSubscriptionAlipayTestContext(t, http.MethodPost, "/api/subscription/alipay/pay", map[string]any{
		"plan_id":        plan.Id,
		"payment_method": "alipay_direct",
		"pay_mode":       "qr",
	}, 1)
	SubscriptionRequestAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), "支付宝支付配置不完整") {
		t.Fatalf("expected disabled alipay error, got: %s", recorder.Body.String())
	}
}

func TestSubscriptionRequestAlipayPayConvertsUSDPlanPriceToCNY(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	plan := seedSubscriptionAlipayPlan(t, 10)
	plan.Currency = "USD"
	if err := model.DB.Save(plan).Error; err != nil {
		t.Fatalf("failed to update plan currency: %v", err)
	}
	model.InvalidateSubscriptionPlanCache(plan.Id)
	seedAlipayConfig()
	expectedAmount := decimal.NewFromFloat(plan.PriceAmount).
		Mul(decimal.NewFromFloat(operation_setting.USDExchangeRate)).
		Round(2).
		StringFixed(2)

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			createQROrderFunc: func(_ context.Context, req alipaypkg.CreateOrderRequest) (*alipaypkg.QROrderResponse, error) {
				if req.TotalAmount.StringFixed(2) != expectedAmount {
					t.Fatalf("expected %s, got %s", expectedAmount, req.TotalAmount.StringFixed(2))
				}
				return &alipaypkg.QROrderResponse{QRCode: "https://qr.alipay.com/test"}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newSubscriptionAlipayTestContext(t, http.MethodPost, "/api/subscription/alipay/pay", map[string]any{
		"plan_id":        plan.Id,
		"payment_method": "alipay_direct",
		"pay_mode":       "qr",
	}, 1)
	SubscriptionRequestAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), expectedAmount) {
		t.Fatalf("expected amount_yuan %s in response: %s", expectedAmount, recorder.Body.String())
	}
}

func TestSubscriptionQueryAlipayPayReturnsErrorWhenTradeClosed(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	seedAlipayConfig()
	plan := seedSubscriptionAlipayPlan(t, 88)
	seedPendingSubscriptionAlipayOrder(t, &model.SubscriptionOrder{
		UserId:        1,
		PlanId:        plan.Id,
		Money:         88,
		TradeNo:       "ALIPAY-SUB-Q-CLOSED",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "qr",
		Status:        common.TopUpStatusPending,
		CreateTime:    common.GetTimestamp(),
	})

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			queryOrderFunc: func(_ context.Context, outTradeNo string) (*alipaypkg.QueryOrderResult, error) {
				return &alipaypkg.QueryOrderResult{
					OutTradeNo:     outTradeNo,
					TradeNo:        "ALI-SUB-CLOSED",
					TradeStatus:    "TRADE_CLOSED",
					TotalAmount:    decimal.RequireFromString("88.00"),
					BuyerPayAmount: decimal.RequireFromString("0.00"),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newSubscriptionAlipayTestContext(t, http.MethodPost, "/api/subscription/alipay/query", map[string]any{"trade_no": "ALIPAY-SUB-Q-CLOSED"}, 1)
	SubscriptionQueryAlipayPay(ctx)
	if !strings.Contains(recorder.Body.String(), "订单已关闭") {
		t.Fatalf("expected closed-order error, got: %s", recorder.Body.String())
	}
}
