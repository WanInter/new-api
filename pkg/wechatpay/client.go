package wechatpay

import (
	"context"
	"fmt"

	wechatpaycore "github.com/wechatpay-apiv3/wechatpay-go/core"
)

type Config struct {
	MchID            string
	AppID            string
	APIv3Key         string
	PrivateKeyPEM    string
	MerchantSerialNo string
	PublicKeyID      string
	PublicKeyPEM     string
}

type NativeOrderRequest struct {
	OutTradeNo  string
	Description string
	NotifyURL   string
	AmountFen   int64
	ClientIP    string
}

type NativeOrderResponse struct {
	CodeURL string
}

type NotifyResult struct {
	OutTradeNo    string
	TransactionID string
	TradeState    string
	TradeType     string
	AppID         string
	MchID         string
	AmountTotal   int64
	Currency      string
}

type Client interface {
	CreateNativeOrder(ctx context.Context, req NativeOrderRequest) (*NativeOrderResponse, error)
	VerifyAndDecryptNotify(ctx context.Context, headers map[string]string, body []byte) (*NotifyResult, error)
}

type sdkClient struct {
	cfg Config
	cli *wechatpaycore.Client
}

func NewClient(cfg Config) (Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &sdkClient{cfg: cfg}, nil
}

func (c *sdkClient) CreateNativeOrder(ctx context.Context, req NativeOrderRequest) (*NativeOrderResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *sdkClient) VerifyAndDecryptNotify(ctx context.Context, headers map[string]string, body []byte) (*NotifyResult, error) {
	return nil, fmt.Errorf("not implemented")
}
