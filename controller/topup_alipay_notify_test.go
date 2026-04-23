package controller

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	alipaypkg "github.com/QuantumNous/new-api/pkg/alipay"
	"github.com/shopspring/decimal"
)

func TestAlipayNotifyCompletesRechargeOnce(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "ALIPAY-TOPUP-1", 72.00, 10, "alipay_direct")
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values url.Values) (*alipaypkg.NotificationResult, error) {
				return &alipaypkg.NotificationResult{
					OutTradeNo:     values.Get("out_trade_no"),
					TradeNo:        "202604230001",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("72.00"),
					BuyerPayAmount: decimal.RequireFromString("72.00"),
					RawForm:        values.Encode(),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newAlipayNotifyContext(t, url.Values{
		"out_trade_no": []string{"ALIPAY-TOPUP-1"},
		"sign":         []string{"signed"},
	})
	AlipayNotify(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	assertTopupNotifyStatus(t, "ALIPAY-TOPUP-1", common.TopUpStatusSuccess)
}

func TestAlipayNotifyRejectsAmountMismatch(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "ALIPAY-TOPUP-1", 72.00, 10, "alipay_direct")
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values url.Values) (*alipaypkg.NotificationResult, error) {
				return &alipaypkg.NotificationResult{
					OutTradeNo:     values.Get("out_trade_no"),
					TradeNo:        "202604230001",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("71.00"),
					BuyerPayAmount: decimal.RequireFromString("71.00"),
					RawForm:        values.Encode(),
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newAlipayNotifyContext(t, url.Values{
		"out_trade_no": []string{"ALIPAY-TOPUP-1"},
		"sign":         []string{"signed"},
	})
	AlipayNotify(ctx)
	if recorder.Code == http.StatusOK {
		t.Fatalf("expected failure for amount mismatch")
	}
	assertTopupNotifyStatus(t, "ALIPAY-TOPUP-1", common.TopUpStatusPending)
}
