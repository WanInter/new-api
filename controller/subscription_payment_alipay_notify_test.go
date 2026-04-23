package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	alipaypkg "github.com/QuantumNous/new-api/pkg/alipay"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func newSubscriptionAlipayNotifyContext(t *testing.T, form url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/alipay/notify", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ctx, recorder
}

func TestSubscriptionAlipayNotifyCompletesOrderOnce(t *testing.T) {
	setupSubscriptionAlipayControllerTestEnv(t)
	seedSubscriptionAlipayUser(t, 1)
	plan := seedSubscriptionAlipayPlan(t, 88)
	seedPendingSubscriptionAlipayOrder(t, &model.SubscriptionOrder{
		UserId:        1,
		PlanId:        plan.Id,
		Money:         88,
		TradeNo:       "ALIPAY-SUB-1",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "page",
		Status:        common.TopUpStatusPending,
		CreateTime:    common.GetTimestamp(),
	})
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values url.Values) (*alipaypkg.NotificationResult, error) {
				return &alipaypkg.NotificationResult{
					AppID:          setting.AlipayAppID,
					SellerID:       "2088000000000000",
					OutTradeNo:     values.Get("out_trade_no"),
					TradeNo:        "202604230099",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("88.00"),
					BuyerPayAmount: decimal.RequireFromString("88.00"),
					RawForm:        values.Encode(),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	form := url.Values{"out_trade_no": []string{"ALIPAY-SUB-1"}, "sign": []string{"signed"}}
	ctx, recorder := newSubscriptionAlipayNotifyContext(t, form)
	SubscriptionAlipayNotify(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected first 200, got %d", recorder.Code)
	}
	ctx2, recorder2 := newSubscriptionAlipayNotifyContext(t, form)
	SubscriptionAlipayNotify(ctx2)
	if recorder2.Code != http.StatusOK {
		t.Fatalf("expected repeated 200, got %d", recorder2.Code)
	}

	var order model.SubscriptionOrder
	if err := model.DB.First(&order, "trade_no = ?", "ALIPAY-SUB-1").Error; err != nil {
		t.Fatalf("failed to query subscription order: %v", err)
	}
	if order.Status != common.TopUpStatusSuccess {
		t.Fatalf("expected order success, got %s", order.Status)
	}
	var count int64
	if err := model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("failed to count subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one user subscription, got %d", count)
	}
}
