package wechatpay

import (
	"context"
	"fmt"

	wechatpaycore "github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

type Config struct {
	MchID              string
	AppID              string
	APIv3Key           string
	PrivateKeyPEM      string
	MerchantSerialNo   string
	PublicKeyID        string
	PublicKeyPEM       string
	DefaultNotifyURL   string
	DefaultDescription string
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

func (c Config) Validate() error {
	if c.MchID == "" || c.AppID == "" || c.APIv3Key == "" || c.PrivateKeyPEM == "" ||
		c.MerchantSerialNo == "" || c.PublicKeyID == "" || c.PublicKeyPEM == "" {
		return fmt.Errorf("wechat pay config is incomplete")
	}
	return nil
}

func NewClient(cfg Config) (Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	privateKey, err := utils.LoadPrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	cli, err := wechatpaycore.NewClient(
		context.Background(),
		option.WithWechatPayAutoAuthCipher(cfg.MchID, cfg.MerchantSerialNo, privateKey, cfg.APIv3Key),
	)
	if err != nil {
		return nil, err
	}
	return &sdkClient{cfg: cfg, cli: cli}, nil
}

func (c *sdkClient) resolveNativeOrderRequest(req NativeOrderRequest) NativeOrderRequest {
	description := req.Description
	if description == "" {
		description = c.cfg.DefaultDescription
	}
	if description == "" {
		description = "账户充值"
	}

	notifyURL := req.NotifyURL
	if notifyURL == "" {
		notifyURL = c.cfg.DefaultNotifyURL
	}
	req.Description = description
	req.NotifyURL = notifyURL
	return req
}

func (c *sdkClient) CreateNativeOrder(ctx context.Context, req NativeOrderRequest) (*NativeOrderResponse, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("wechat pay client is not initialized")
	}

	resolvedReq := c.resolveNativeOrderRequest(req)

	prepayReq := native.PrepayRequest{
		Appid:       wechatpaycore.String(c.cfg.AppID),
		Mchid:       wechatpaycore.String(c.cfg.MchID),
		Description: wechatpaycore.String(resolvedReq.Description),
		OutTradeNo:  wechatpaycore.String(resolvedReq.OutTradeNo),
		NotifyUrl:   wechatpaycore.String(resolvedReq.NotifyURL),
		Amount: &native.Amount{
			Total:    wechatpaycore.Int64(resolvedReq.AmountFen),
			Currency: wechatpaycore.String("CNY"),
		},
	}
	if resolvedReq.ClientIP != "" {
		prepayReq.SceneInfo = &native.SceneInfo{
			PayerClientIp: wechatpaycore.String(resolvedReq.ClientIP),
		}
	}

	svc := native.NativeApiService{Client: c.cli}
	resp, _, err := svc.Prepay(ctx, prepayReq)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.CodeUrl == nil {
		return nil, fmt.Errorf("wechat pay native order returned empty code url")
	}
	return &NativeOrderResponse{CodeURL: *resp.CodeUrl}, nil
}
