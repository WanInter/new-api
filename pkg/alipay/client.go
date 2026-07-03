package alipay

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"
	sdk "github.com/smartwalle/alipay/v3"
)

const (
	PayModePage = "page"
	PayModeQR   = "qr"
)

const (
	alipayProductCodePage         = "FAST_INSTANT_TRADE_PAY"
	alipayProductCodePreCreate    = "FACE_TO_FACE_PAYMENT"
	defaultAlipayOrderDescription = "账户充值"
	alipayTradeStatusSuccess      = "TRADE_SUCCESS"
	alipayTradeStatusFinished     = "TRADE_FINISHED"
)

type Config struct {
	AppID                        string
	PrivateKey                   string
	PublicKey                    string
	Sandbox                      bool
	DefaultNotifyURL             string
	DefaultReturnURL             string
	DefaultSubscriptionReturnURL string
	DefaultOrderDescription      string
}

type CreateOrderRequest struct {
	OutTradeNo  string
	Subject     string
	TotalAmount decimal.Decimal
	NotifyURL   string
	ReturnURL   string
}

type PageOrderResponse struct {
	PayURL string
}

type QROrderResponse struct {
	QRCode string
}

type QueryOrderResult struct {
	OutTradeNo     string          `json:"out_trade_no"`
	TradeNo        string          `json:"trade_no"`
	TradeStatus    string          `json:"trade_status"`
	TotalAmount    decimal.Decimal `json:"total_amount"`
	BuyerPayAmount decimal.Decimal `json:"buyer_pay_amount"`
	BuyerLogonID   string          `json:"buyer_logon_id,omitempty"`
	BuyerUserID    string          `json:"buyer_user_id,omitempty"`
	BuyerUserName  string          `json:"buyer_user_name,omitempty"`
}

type NotificationResult struct {
	AppID          string          `json:"app_id,omitempty"`
	SellerID       string          `json:"seller_id,omitempty"`
	OutTradeNo     string          `json:"out_trade_no"`
	TradeNo        string          `json:"trade_no,omitempty"`
	TradeStatus    string          `json:"trade_status"`
	TotalAmount    decimal.Decimal `json:"total_amount"`
	BuyerPayAmount decimal.Decimal `json:"buyer_pay_amount"`
	BuyerLogonID   string          `json:"buyer_logon_id,omitempty"`
	BuyerUserID    string          `json:"buyer_user_id,omitempty"`
	BuyerUserName  string          `json:"buyer_user_name,omitempty"`
	RawForm        string          `json:"-"` // 支付宝回调表单的完整规范化查询串（url.Values.Encode），保留多值 key 语义
}

type Client interface {
	CreatePageOrder(ctx context.Context, req CreateOrderRequest) (*PageOrderResponse, error)
	CreateQROrder(ctx context.Context, req CreateOrderRequest) (*QROrderResponse, error)
	QueryOrder(ctx context.Context, outTradeNo string) (*QueryOrderResult, error)
	VerifyNotification(values url.Values) (*NotificationResult, error)
}

type sdkClient struct {
	cfg Config
	cli *sdk.Client
	api alipayAPI
}

var _ Client = (*sdkClient)(nil)

type alipayAPI interface {
	TradePagePay(param sdk.TradePagePay) (*url.URL, error)
	TradePreCreate(ctx context.Context, param sdk.TradePreCreate) (*sdk.TradePreCreateRsp, error)
	TradeQuery(ctx context.Context, param sdk.TradeQuery) (*sdk.TradeQueryRsp, error)
	VerifySign(ctx context.Context, values url.Values) error
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.PrivateKey) == "" || strings.TrimSpace(c.PublicKey) == "" {
		return fmt.Errorf("alipay config is incomplete")
	}
	return nil
}

func NormalizePayMode(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case PayModePage:
		return PayModePage, nil
	case PayModeQR:
		return PayModeQR, nil
	default:
		return "", fmt.Errorf("invalid alipay pay mode: %s", value)
	}
}

func NewClient(cfg Config) (Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cli, err := sdk.New(cfg.AppID, cfg.PrivateKey, !cfg.Sandbox)
	if err != nil {
		return nil, err
	}
	if err := cli.LoadAliPayPublicKey(cfg.PublicKey); err != nil {
		return nil, err
	}
	return &sdkClient{cfg: cfg, cli: cli, api: cli}, nil
}

func (c *sdkClient) CreatePageOrder(ctx context.Context, req CreateOrderRequest) (*PageOrderResponse, error) {
	if err := c.validateContext(ctx); err != nil {
		return nil, err
	}
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resolvedReq, err := c.resolveCreateOrderRequest(req)
	if err != nil {
		return nil, err
	}

	payURL, err := c.api.TradePagePay(sdk.TradePagePay{
		Trade: sdk.Trade{
			Subject:     resolvedReq.Subject,
			OutTradeNo:  resolvedReq.OutTradeNo,
			TotalAmount: resolvedReq.TotalAmount.StringFixed(2),
			ProductCode: alipayProductCodePage,
			NotifyURL:   resolvedReq.NotifyURL,
			ReturnURL:   resolvedReq.ReturnURL,
		},
	})
	if err != nil {
		return nil, err
	}
	if payURL == nil {
		return nil, fmt.Errorf("alipay trade page pay returned empty url")
	}
	return &PageOrderResponse{PayURL: payURL.String()}, nil
}

func (c *sdkClient) CreateQROrder(ctx context.Context, req CreateOrderRequest) (*QROrderResponse, error) {
	if err := c.validateContext(ctx); err != nil {
		return nil, err
	}
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resolvedReq, err := c.resolveCreateOrderRequest(req)
	if err != nil {
		return nil, err
	}

	result, err := c.api.TradePreCreate(ctx, sdk.TradePreCreate{
		Trade: sdk.Trade{
			Subject:     resolvedReq.Subject,
			OutTradeNo:  resolvedReq.OutTradeNo,
			TotalAmount: resolvedReq.TotalAmount.StringFixed(2),
			ProductCode: alipayProductCodePreCreate,
			NotifyURL:   resolvedReq.NotifyURL,
		},
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("alipay trade precreate returned empty result")
	}
	if result.IsFailure() {
		return nil, fmt.Errorf("alipay trade precreate failed: %s - %s", result.Code, result.SubMsg)
	}
	if strings.TrimSpace(result.QRCode) == "" {
		return nil, fmt.Errorf("alipay trade precreate returned empty qr code")
	}
	return &QROrderResponse{
		QRCode: result.QRCode,
	}, nil
}

func (c *sdkClient) QueryOrder(ctx context.Context, outTradeNo string) (*QueryOrderResult, error) {
	if err := c.validateContext(ctx); err != nil {
		return nil, err
	}
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(outTradeNo) == "" {
		return nil, fmt.Errorf("alipay out_trade_no is required")
	}

	result, err := c.api.TradeQuery(ctx, sdk.TradeQuery{OutTradeNo: strings.TrimSpace(outTradeNo)})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("alipay trade query returned empty result")
	}
	if result.IsFailure() {
		return nil, fmt.Errorf("alipay trade query failed: %s - %s", result.Code, result.SubMsg)
	}

	totalAmount, err := parseDecimalOrZero(result.TotalAmount)
	if err != nil {
		return nil, fmt.Errorf("parse total_amount: %w", err)
	}
	buyerPayAmount, err := parseDecimalOrZero(result.BuyerPayAmount)
	if err != nil {
		return nil, fmt.Errorf("parse buyer_pay_amount: %w", err)
	}

	return &QueryOrderResult{
		OutTradeNo:     result.OutTradeNo,
		TradeNo:        result.TradeNo,
		TradeStatus:    string(result.TradeStatus),
		TotalAmount:    totalAmount,
		BuyerPayAmount: buyerPayAmount,
		BuyerLogonID:   result.BuyerLogonId,
		BuyerUserID:    result.BuyerUserId,
		BuyerUserName:  result.BuyerUserName,
	}, nil
}

func (c *sdkClient) VerifyNotification(values url.Values) (*NotificationResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	result, err := ParseNotification(values)
	if err != nil {
		return nil, err
	}
	if err = c.api.VerifySign(context.Background(), values); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *sdkClient) validateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return nil
}

func (c *sdkClient) ensureReady() error {
	if c == nil || c.api == nil {
		return fmt.Errorf("alipay client is not initialized")
	}
	return nil
}

func (c *sdkClient) resolveCreateOrderRequest(req CreateOrderRequest) (CreateOrderRequest, error) {
	resolvedReq := req
	resolvedReq.OutTradeNo = strings.TrimSpace(resolvedReq.OutTradeNo)
	resolvedReq.Subject = strings.TrimSpace(resolvedReq.Subject)
	resolvedReq.NotifyURL = strings.TrimSpace(resolvedReq.NotifyURL)
	resolvedReq.ReturnURL = strings.TrimSpace(resolvedReq.ReturnURL)

	if resolvedReq.OutTradeNo == "" {
		return CreateOrderRequest{}, fmt.Errorf("alipay out_trade_no is required")
	}
	if !resolvedReq.TotalAmount.GreaterThan(decimal.Zero) {
		return CreateOrderRequest{}, fmt.Errorf("alipay total amount must be greater than 0")
	}

	if resolvedReq.Subject == "" {
		resolvedReq.Subject = strings.TrimSpace(c.cfg.DefaultOrderDescription)
	}
	if resolvedReq.Subject == "" {
		resolvedReq.Subject = defaultAlipayOrderDescription
	}
	if resolvedReq.NotifyURL == "" {
		resolvedReq.NotifyURL = strings.TrimSpace(c.cfg.DefaultNotifyURL)
	}
	if resolvedReq.ReturnURL == "" {
		resolvedReq.ReturnURL = strings.TrimSpace(c.cfg.DefaultReturnURL)
	}
	return resolvedReq, nil
}

func parseDecimalOrZero(value string) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(value)
}

func (r NotificationResult) ValidatePaid() error {
	if r.TradeStatus != alipayTradeStatusSuccess && r.TradeStatus != alipayTradeStatusFinished {
		return fmt.Errorf("unexpected trade status: %s", r.TradeStatus)
	}
	if !r.TotalAmount.GreaterThan(decimal.Zero) {
		return fmt.Errorf("invalid total amount")
	}
	return nil
}
