package alipay

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"
)

func ParseNotification(values url.Values) (*NotificationResult, error) {
	if strings.TrimSpace(values.Get("sign")) == "" {
		return nil, fmt.Errorf("missing alipay sign")
	}
	if strings.TrimSpace(values.Get("out_trade_no")) == "" {
		return nil, fmt.Errorf("missing out_trade_no")
	}

	result := &NotificationResult{
		AppID:       values.Get("app_id"),
		SellerID:    values.Get("seller_id"),
		OutTradeNo:  values.Get("out_trade_no"),
		TradeNo:     values.Get("trade_no"),
		TradeStatus: values.Get("trade_status"),
		RawForm:     normalizeValues(values),
	}

	if total := values.Get("total_amount"); total != "" {
		amount, err := decimal.NewFromString(total)
		if err != nil {
			return nil, err
		}
		result.TotalAmount = amount
	}

	if paid := values.Get("buyer_pay_amount"); paid != "" {
		amount, err := decimal.NewFromString(paid)
		if err != nil {
			return nil, err
		}
		result.BuyerPayAmount = amount
	}

	return result, nil
}

func normalizeValues(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	copied := make(url.Values, len(values))
	for key, list := range values {
		copied[key] = append([]string(nil), list...)
	}
	return copied.Encode()
}
