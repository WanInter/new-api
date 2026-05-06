package controller

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	alipaypkg "github.com/QuantumNous/new-api/pkg/alipay"
	"github.com/QuantumNous/new-api/setting"
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
					AppID:          setting.AlipayAppID,
					SellerID:       "2088000000000000",
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

	form := url.Values{
		"out_trade_no": []string{"ALIPAY-TOPUP-1"},
		"sign":         []string{"signed"},
	}
	ctx, recorder := newAlipayNotifyContext(t, form)
	AlipayNotify(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d", recorder.Code)
	}

	ctx2, recorder2 := newAlipayNotifyContext(t, form)
	AlipayNotify(ctx2)
	if recorder2.Code != http.StatusOK {
		t.Fatalf("expected repeated status 200, got %d", recorder2.Code)
	}

	assertTopupNotifyStatus(t, "ALIPAY-TOPUP-1", common.TopUpStatusSuccess)
	assertTopupNotifyUserQuota(t, 1, int(10*common.QuotaPerUnit))
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
					AppID:          setting.AlipayAppID,
					SellerID:       "2088000000000000",
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

func TestAlipayNotifyRejectsAppIDMismatch(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setupAlipaySettingIsolation(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "ALIPAY-TOPUP-APPID", 72.00, 10, "alipay_direct")
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipaypkg.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values url.Values) (*alipaypkg.NotificationResult, error) {
				return &alipaypkg.NotificationResult{
					AppID:          "wrong-app-id",
					SellerID:       "2088000000000000",
					OutTradeNo:     values.Get("out_trade_no"),
					TradeNo:        "202604230009",
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
		"out_trade_no": []string{"ALIPAY-TOPUP-APPID"},
		"sign":         []string{"signed"},
	})
	AlipayNotify(ctx)
	if recorder.Code == http.StatusOK {
		t.Fatalf("expected failure for app id mismatch")
	}
	assertTopupNotifyStatus(t, "ALIPAY-TOPUP-APPID", common.TopUpStatusPending)
}
