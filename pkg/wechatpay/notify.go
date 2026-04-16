package wechatpay

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	wechatpaynotify "github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

const (
	wechatPayTradeStateSuccess     = "SUCCESS"
	wechatPayTradeTypeNative       = "NATIVE"
	wechatPayNotifyEventTxnSuccess = "TRANSACTION.SUCCESS"
)

func (r *NotifyResult) ValidateBusinessFields(appID, mchID string) error {
	if r == nil {
		return fmt.Errorf("notify result is nil")
	}
	if r.TradeState != wechatPayTradeStateSuccess {
		return fmt.Errorf("unexpected trade state: %s", r.TradeState)
	}
	if r.TradeType != wechatPayTradeTypeNative {
		return fmt.Errorf("unexpected trade type: %s", r.TradeType)
	}
	if r.Currency != "CNY" {
		return fmt.Errorf("unexpected currency: %s", r.Currency)
	}
	if r.AppID != appID || r.MchID != mchID {
		return fmt.Errorf("appid or mchid mismatch")
	}
	return nil
}

func (c *sdkClient) VerifyAndDecryptNotify(ctx context.Context, headers map[string]string, body []byte) (*NotifyResult, error) {
	serial := headers["Wechatpay-Serial"]
	if serial != c.cfg.PublicKeyID {
		return nil, fmt.Errorf("wechatpay serial mismatch")
	}

	publicKey, err := utils.LoadPublicKey(c.cfg.PublicKeyPEM)
	if err != nil {
		return nil, err
	}
	verifier := verifiers.NewSHA256WithRSAPubkeyVerifier(c.cfg.PublicKeyID, *publicKey)
	handler, err := wechatpaynotify.NewRSANotifyHandler(c.cfg.APIv3Key, verifier)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://wechatpay.local/notify", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	transaction := new(payments.Transaction)
	notifyRequest, err := handler.ParseNotifyRequest(ctx, request, transaction)
	if err != nil {
		return nil, err
	}
	if notifyRequest.EventType != wechatPayNotifyEventTxnSuccess {
		return nil, fmt.Errorf("unexpected event type: %s", notifyRequest.EventType)
	}

	result := &NotifyResult{
		OutTradeNo:    wechatStringValue(transaction.OutTradeNo),
		TransactionID: wechatStringValue(transaction.TransactionId),
		TradeState:    wechatStringValue(transaction.TradeState),
		TradeType:     wechatStringValue(transaction.TradeType),
		AppID:         wechatStringValue(transaction.Appid),
		MchID:         wechatStringValue(transaction.Mchid),
	}
	if transaction.Amount != nil {
		result.AmountTotal = wechatInt64Value(transaction.Amount.Total)
		result.Currency = wechatStringValue(transaction.Amount.Currency)
	}
	if err = result.ValidateBusinessFields(c.cfg.AppID, c.cfg.MchID); err != nil {
		return nil, err
	}
	return result, nil
}

func wechatStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func wechatInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
