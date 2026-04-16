package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wechatpay"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type fakeWeChatPayNotifyClient struct {
	verifyAndDecryptNotifyFunc func(ctx context.Context, headers map[string]string, body []byte) (*wechatpay.NotifyResult, error)
}

func (f fakeWeChatPayNotifyClient) CreateNativeOrder(ctx context.Context, req wechatpay.NativeOrderRequest) (*wechatpay.NativeOrderResponse, error) {
	return nil, nil
}

func (f fakeWeChatPayNotifyClient) VerifyAndDecryptNotify(ctx context.Context, headers map[string]string, body []byte) (*wechatpay.NotifyResult, error) {
	return f.verifyAndDecryptNotifyFunc(ctx, headers, body)
}

func newTopupNotifyContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/wechat/notify", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Wechatpay-Signature", "signature")
	ctx.Request.Header.Set("Wechatpay-Nonce", "nonce")
	ctx.Request.Header.Set("Wechatpay-Timestamp", "1710000000")
	ctx.Request.Header.Set("Wechatpay-Serial", setting.WeChatPayPublicKeyID)

	return ctx, recorder
}

func seedPendingTopup(t *testing.T, tradeNo string, money float64, amount int64, paymentMethod string) {
	t.Helper()

	topUp := &model.TopUp{
		UserId:        1,
		Amount:        amount,
		Money:         money,
		TradeNo:       tradeNo,
		PaymentMethod: paymentMethod,
		CreateTime:    time.Now().Unix(),
		Status:        common.TopUpStatusPending,
	}
	if err := model.DB.Create(topUp).Error; err != nil {
		t.Fatalf("failed to seed topup: %v", err)
	}
}

func assertTopupNotifyUserQuota(t *testing.T, userID int, expected int) {
	t.Helper()

	var user model.User
	if err := model.DB.First(&user, "id = ?", userID).Error; err != nil {
		t.Fatalf("failed to query user: %v", err)
	}
	if user.Quota != expected {
		t.Fatalf("expected user quota %d, got %d", expected, user.Quota)
	}
}

func assertTopupNotifyStatus(t *testing.T, tradeNo string, expected string) {
	t.Helper()

	var topUp model.TopUp
	if err := model.DB.First(&topUp, "trade_no = ?", tradeNo).Error; err != nil {
		t.Fatalf("failed to query topup: %v", err)
	}
	if topUp.Status != expected {
		t.Fatalf("expected topup status %q, got %q", expected, topUp.Status)
	}
}

func TestWeChatPayNotifyRejectsAmountMismatch(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "WXPAY-ORDER-1", 72.00, 10, "wechat_pay")
	seedWeChatPayConfig()

	originalFactory := newWeChatPayClient
	newWeChatPayClient = func() (wechatpay.Client, error) {
		return fakeWeChatPayNotifyClient{
			verifyAndDecryptNotifyFunc: func(_ context.Context, _ map[string]string, _ []byte) (*wechatpay.NotifyResult, error) {
				return &wechatpay.NotifyResult{
					OutTradeNo:    "WXPAY-ORDER-1",
					TransactionID: "TX123",
					TradeState:    "SUCCESS",
					TradeType:     "NATIVE",
					AppID:         setting.WeChatPayAppID,
					MchID:         setting.WeChatPayMchID,
					AmountTotal:   7100,
					Currency:      "CNY",
				}, nil
			},
		}, nil
	}
	defer func() { newWeChatPayClient = originalFactory }()

	ctx, recorder := newTopupNotifyContext(t, []byte(`{"id":"EVT-1"}`))
	WeChatPayNotify(ctx)

	if recorder.Code == http.StatusOK || recorder.Code == http.StatusNoContent {
		t.Fatalf("expected non-2xx for amount mismatch, got %d", recorder.Code)
	}
	assertTopupNotifyUserQuota(t, 1, 0)
	assertTopupNotifyStatus(t, "WXPAY-ORDER-1", common.TopUpStatusPending)
}

func TestWeChatPayNotifyCompletesRechargeOnce(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "WXPAY-ORDER-1", 72.00, 10, "wechat_pay")
	seedWeChatPayConfig()

	originalFactory := newWeChatPayClient
	newWeChatPayClient = func() (wechatpay.Client, error) {
		return fakeWeChatPayNotifyClient{
			verifyAndDecryptNotifyFunc: func(_ context.Context, _ map[string]string, _ []byte) (*wechatpay.NotifyResult, error) {
				return &wechatpay.NotifyResult{
					OutTradeNo:    "WXPAY-ORDER-1",
					TransactionID: "TX123",
					TradeState:    "SUCCESS",
					TradeType:     "NATIVE",
					AppID:         setting.WeChatPayAppID,
					MchID:         setting.WeChatPayMchID,
					AmountTotal:   7200,
					Currency:      "CNY",
				}, nil
			},
		}, nil
	}
	defer func() { newWeChatPayClient = originalFactory }()

	ctx, recorder := newTopupNotifyContext(t, []byte(`{"id":"EVT-1"}`))
	WeChatPayNotify(ctx)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected first notify status 204, got %d, body=%s", recorder.Code, recorder.Body.String())
	}

	ctx2, recorder2 := newTopupNotifyContext(t, []byte(`{"id":"EVT-2"}`))
	WeChatPayNotify(ctx2)
	if recorder2.Code != http.StatusNoContent {
		t.Fatalf("expected repeated notify status 204, got %d, body=%s", recorder2.Code, recorder2.Body.String())
	}

	assertTopupNotifyUserQuota(t, 1, int(10*common.QuotaPerUnit))
	assertTopupNotifyStatus(t, "WXPAY-ORDER-1", common.TopUpStatusSuccess)
}
