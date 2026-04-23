# Alipay Direct Payment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为项目新增支付宝官方直连支付能力，覆盖钱包充值与订阅购买，支持电脑网站支付、扫码支付、异步通知与主动查单，并在官方渠道启用时优先隐藏 EPay 历史支付宝入口。

**Architecture:** 延续现有微信支付的“独立支付渠道”模式，不重构统一支付抽象。后端新增 `pkg/alipay` 适配层并扩展 `TopUp` / `SubscriptionOrder` 订单语义；前端复用现有充值页与订阅购买弹窗，新增一个通用支付宝弹窗组件承载模式选择、二维码展示、自动轮询与手动补查。

**Tech Stack:** Go 1.22+, Gin, GORM, SQLite/MySQL/PostgreSQL 兼容事务逻辑，`github.com/smartwalle/alipay/v3`，React 18 + Semi UI + Bun + `qrcode.react`。

---

## File Structure

### Backend

- Create: `setting/payment_alipay.go`
  - 支付宝官方直连运行时配置。
- Create: `pkg/alipay/client.go`
  - page 支付、扫码预下单、查单接口与统一 DTO。
- Create: `pkg/alipay/notify.go`
  - 异步通知验签、字段解析、金额标准化。
- Create: `pkg/alipay/client_test.go`
  - 配置校验、模式校验、验签边界与金额解析测试。
- Create: `controller/topup_alipay.go`
  - 钱包充值金额预览、下单、查单、通知处理。
- Create: `controller/topup_alipay_test.go`
  - `GetTopUpInfo` 官方优先、金额预览、page/qr 下单、查单路径测试。
- Create: `controller/topup_alipay_notify_test.go`
  - 钱包充值异步通知、金额不一致、重复回调幂等测试。
- Create: `controller/subscription_payment_alipay.go`
  - 订阅支付下单、查单、通知处理。
- Create: `controller/subscription_payment_alipay_test.go`
  - 订阅 page/qr 下单、查单完成与错误测试。
- Create: `controller/subscription_payment_alipay_notify_test.go`
  - 订阅异步通知幂等与金额校验测试。
- Create: `model/topup_alipay_test.go`
  - `RechargeAlipay` 入账与 `provider_payload` 保存测试。
- Modify: `model/topup.go`
  - 扩展字段并新增 `RechargeAlipay`。
- Modify: `model/subscription.go`
  - `SubscriptionOrder` 扩展 `PaymentMode`，完成订单时同步透传。
- Modify: `model/option.go`
  - 新配置项初始化与运行时更新。
- Modify: `router/api-router.go`
  - 新增钱包充值/订阅支付宝路由。
- Modify: `controller/topup.go`
  - 官方渠道优先注入 `alipay_direct`，并隐藏 EPay `alipay`。
- Modify: `go.mod`, `go.sum`
  - 引入支付宝 SDK。

### Frontend

- Create: `web/src/pages/Setting/Payment/SettingsPaymentGatewayAlipay.jsx`
  - 后台支付宝官方直连设置卡片。
- Create: `web/src/components/topup/modals/AlipayCheckoutModal.jsx`
  - 模式选择、二维码展示、自动轮询与手动补查的通用弹窗。
- Modify: `web/src/components/settings/PaymentSetting.jsx`
  - 拉取并分发支付宝设置。
- Modify: `web/src/components/topup/index.jsx`
  - 钱包充值页接入支付宝官方直连状态、拉起支付与查单。
- Modify: `web/src/components/topup/RechargeCard.jsx`
  - 支付按钮正确识别 `alipay_direct`。
- Modify: `web/src/components/topup/modals/PaymentConfirmModal.jsx`
  - 钱包充值确认文案支持 `alipay_direct`。
- Modify: `web/src/components/topup/modals/TopupHistoryModal.jsx`
  - 账单映射 `alipay_direct`。
- Modify: `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`
  - 订阅购买弹窗新增支付宝官方直连入口。
- Modify: `web/src/components/topup/SubscriptionPlansCard.jsx`
  - 订阅下单、查单、轮询与弹窗联动。
- Modify: `web/src/i18n/locales/zh-CN.json`
- Modify: `web/src/i18n/locales/en.json`
  - 支付宝官方直连相关新增文案。

---

### Task 1: 建立支付宝配置与 SDK 边界

**Files:**
- Create: `setting/payment_alipay.go`
- Create: `pkg/alipay/client.go`
- Create: `pkg/alipay/notify.go`
- Create: `pkg/alipay/client_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 先写 `pkg/alipay` 的失败测试（RED）**

```go
package alipay

import (
	"net/url"
	"testing"

	"github.com/shopspring/decimal"
)

func TestConfigValidateRequiresCoreKeys(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected config validation error")
	}
}

func TestNormalizePayModeRejectsUnknownMode(t *testing.T) {
	if _, err := NormalizePayMode("mobile"); err == nil {
		t.Fatal("expected invalid pay mode error")
	}
}

func TestNotificationResultValidateRequiresSuccessAndAmount(t *testing.T) {
	result := NotificationResult{
		OutTradeNo:   "T-1",
		TradeStatus:  "WAIT_BUYER_PAY",
		TotalAmount:  decimal.RequireFromString("7.20"),
		BuyerPayAmount: decimal.RequireFromString("7.20"),
	}
	if err := result.ValidatePaid(); err == nil {
		t.Fatal("expected unpaid notification error")
	}
}

func TestVerifyNotificationRejectsMissingSignature(t *testing.T) {
	values := url.Values{}
	values.Set("out_trade_no", "T-1")
	_, err := ParseNotification(values)
	if err == nil {
		t.Fatal("expected missing sign error")
	}
}
```

- [ ] **Step 2: 运行测试，确认当前缺少类型与方法**

Run:

```bash
timeout 60s go test ./pkg/alipay -run 'Test(ConfigValidateRequiresCoreKeys|NormalizePayModeRejectsUnknownMode|NotificationResultValidateRequiresSuccessAndAmount|VerifyNotificationRejectsMissingSignature)' -count=1
```

Expected:

```text
undefined: Config
undefined: NormalizePayMode
undefined: NotificationResult
undefined: ParseNotification
```

- [ ] **Step 3: 在 `setting/payment_alipay.go` 中建立运行时配置变量**

```go
package setting

var (
	AlipayEnabled                 bool
	AlipaySandbox                 bool
	AlipayAppID                   string
	AlipayPrivateKey              string
	AlipayPublicKey               string
	AlipayUnitPrice               float64 = 1.0
	AlipayMinTopUp                int     = 1
	AlipayNotifyURL               string
	AlipayReturnURL               string
	AlipaySubscriptionReturnURL   string
	AlipayOrderDescription        string = "账户充值"
)
```

- [ ] **Step 4: 在 `pkg/alipay/client.go` 中写出稳定接口与最小实现骨架**

```go
package alipay

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/smartwalle/alipay/v3"
	"github.com/shopspring/decimal"
)

const (
	PayModePage = "page"
	PayModeQR   = "qr"
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
	OutTradeNo string
	Subject    string
	TotalAmount decimal.Decimal
	NotifyURL  string
	ReturnURL  string
}

type PageOrderResponse struct {
	PayURL string
}

type QROrderResponse struct {
	QRCode   string
	TradeNo  string
}

type QueryOrderResult struct {
	OutTradeNo     string
	TradeNo        string
	TradeStatus    string
	TotalAmount    decimal.Decimal
	BuyerPayAmount decimal.Decimal
}

type NotificationResult struct {
	OutTradeNo      string
	TradeNo         string
	TradeStatus     string
	TotalAmount     decimal.Decimal
	BuyerPayAmount  decimal.Decimal
	RawForm         string
}

type Client interface {
	CreatePageOrder(ctx context.Context, req CreateOrderRequest) (*PageOrderResponse, error)
	CreateQROrder(ctx context.Context, req CreateOrderRequest) (*QROrderResponse, error)
	QueryOrder(ctx context.Context, outTradeNo string) (*QueryOrderResult, error)
	VerifyNotification(values map[string]string) (*NotificationResult, error)
}

type sdkClient struct {
	cfg Config
	cli *sdk.Client
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
	return &sdkClient{cfg: cfg, cli: cli}, nil
}
```

- [ ] **Step 5: 在 `pkg/alipay/notify.go` 中实现通知解析与支付成功校验**

```go
package alipay

import (
	"fmt"
	"net/url"
	"sort"
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

func (r NotificationResult) ValidatePaid() error {
	if r.TradeStatus != "TRADE_SUCCESS" && r.TradeStatus != "TRADE_FINISHED" {
		return fmt.Errorf("unexpected trade status: %s", r.TradeStatus)
	}
	if !r.TotalAmount.GreaterThan(decimal.Zero) {
		return fmt.Errorf("invalid total amount")
	}
	return nil
}

func normalizeValues(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, values.Get(key)))
	}
	return strings.Join(pairs, "&")
}
```

- [ ] **Step 6: 让测试先转绿，再补一个基于 SDK 初始化的边界测试**

追加测试：

```go
func TestNewClientReturnsSDKErrorOnInvalidKey(t *testing.T) {
	_, err := NewClient(Config{
		AppID:      "2026000000000000",
		PrivateKey: "invalid",
		PublicKey:  "invalid",
	})
	if err == nil {
		t.Fatal("expected sdk init error")
	}
}
```

Run:

```bash
timeout 60s go test ./pkg/alipay -count=1
```

Expected:

```text
ok   github.com/QuantumNous/new-api/pkg/alipay
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum setting/payment_alipay.go pkg/alipay/client.go pkg/alipay/notify.go pkg/alipay/client_test.go
git commit -m "feat: add alipay sdk boundary"
```

---

### Task 2: 扩展订单模型、OptionMap 与官方优先的支付方式暴露

**Files:**
- Modify: `model/topup.go`
- Modify: `model/subscription.go`
- Modify: `model/option.go`
- Modify: `controller/topup.go`
- Create: `controller/topup_alipay_test.go`

- [ ] **Step 1: 先写官方优先与新字段的失败测试（RED）**

```go
func TestGetTopUpInfoPrefersAlipayDirectOverLegacyAlipay(t *testing.T) {
	setupTopupControllerTestEnv(t)
	operation_setting.PayMethods = []map[string]string{{
		"name": "支付宝",
		"type": "alipay",
	}}
	setting.AlipayEnabled = true
	setting.AlipayAppID = "2026000000000000"
	setting.AlipayPrivateKey = "private-key"
	setting.AlipayPublicKey = "public-key"
	setting.AlipayMinTopUp = 3

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	body := recorder.Body.String()
	if strings.Contains(body, `"type":"alipay"`) {
		t.Fatalf("expected legacy alipay to be hidden: %s", body)
	}
	if !strings.Contains(body, `"type":"alipay_direct"`) {
		t.Fatalf("expected alipay_direct in response: %s", body)
	}
}
```

- [ ] **Step 2: 运行测试，确认当前还没有 `alipay_direct` 暴露逻辑**

Run:

```bash
timeout 60s go test ./controller -run TestGetTopUpInfoPrefersAlipayDirectOverLegacyAlipay -count=1
```

Expected:

```text
expected alipay_direct in response
```

- [ ] **Step 3: 先扩展 `TopUp` 与 `SubscriptionOrder` 结构，确保 AutoMigrate 能新增字段**

在 `model/topup.go` 的 `TopUp` 结构中加入：

```go
type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentMode     string  `json:"payment_mode" gorm:"type:varchar(32);default:''"`
	ProviderPayload string  `json:"provider_payload" gorm:"type:text"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
}
```

在 `model/subscription.go` 的 `SubscriptionOrder` 结构中加入：

```go
type SubscriptionOrder struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	PlanId          int     `json:"plan_id" gorm:"index"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentMode     string  `json:"payment_mode" gorm:"type:varchar(32);default:''"`
	Status          string  `json:"status"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	ProviderPayload string  `json:"provider_payload" gorm:"type:text"`
}
```

- [ ] **Step 4: 把支付宝配置接入 `OptionMap`，并提供配置完整性判断**

在 `model/option.go` 的 `InitOptionMap` 和 `updateOptionMap` 中加入：

```go
common.OptionMap["AlipayEnabled"] = strconv.FormatBool(setting.AlipayEnabled)
common.OptionMap["AlipaySandbox"] = strconv.FormatBool(setting.AlipaySandbox)
common.OptionMap["AlipayAppID"] = setting.AlipayAppID
common.OptionMap["AlipayPrivateKey"] = setting.AlipayPrivateKey
common.OptionMap["AlipayPublicKey"] = setting.AlipayPublicKey
common.OptionMap["AlipayUnitPrice"] = strconv.FormatFloat(setting.AlipayUnitPrice, 'f', -1, 64)
common.OptionMap["AlipayMinTopUp"] = strconv.Itoa(setting.AlipayMinTopUp)
common.OptionMap["AlipayNotifyURL"] = setting.AlipayNotifyURL
common.OptionMap["AlipayReturnURL"] = setting.AlipayReturnURL
common.OptionMap["AlipaySubscriptionReturnURL"] = setting.AlipaySubscriptionReturnURL
common.OptionMap["AlipayOrderDescription"] = setting.AlipayOrderDescription
```

以及：

```go
case "AlipayEnabled":
	setting.AlipayEnabled = value == "true"
case "AlipaySandbox":
	setting.AlipaySandbox = value == "true"
case "AlipayAppID":
	setting.AlipayAppID = value
case "AlipayPrivateKey":
	setting.AlipayPrivateKey = value
case "AlipayPublicKey":
	setting.AlipayPublicKey = value
case "AlipayUnitPrice":
	setting.AlipayUnitPrice, _ = strconv.ParseFloat(value, 64)
case "AlipayMinTopUp":
	setting.AlipayMinTopUp, _ = strconv.Atoi(value)
case "AlipayNotifyURL":
	setting.AlipayNotifyURL = value
case "AlipayReturnURL":
	setting.AlipayReturnURL = value
case "AlipaySubscriptionReturnURL":
	setting.AlipaySubscriptionReturnURL = value
case "AlipayOrderDescription":
	setting.AlipayOrderDescription = value
```

在 `controller/topup.go` 中新增：

```go
func isAlipayConfigured() bool {
	return setting.AlipayEnabled &&
		setting.AlipayAppID != "" &&
		setting.AlipayPrivateKey != "" &&
		setting.AlipayPublicKey != ""
}
```

- [ ] **Step 5: 修改 `GetTopUpInfo`，官方启用时隐藏 EPay 的 `alipay` 并注入 `alipay_direct`**

把 `payMethods := operation_setting.PayMethods` 改成深拷贝后过滤：

```go
func clonePayMethods(methods []map[string]string) []map[string]string {
	result := make([]map[string]string, 0, len(methods))
	for _, method := range methods {
		cloned := make(map[string]string, len(method))
		for key, value := range method {
			cloned[key] = value
		}
		result = append(result, cloned)
	}
	return result
}
```

并在 `GetTopUpInfo` 中加入：

```go
payMethods := clonePayMethods(operation_setting.PayMethods)
if isAlipayConfigured() {
	filtered := make([]map[string]string, 0, len(payMethods))
	for _, method := range payMethods {
		if method["type"] == "alipay" {
			continue
		}
		filtered = append(filtered, method)
	}
	payMethods = filtered
	payMethods = append(payMethods, map[string]string{
		"name":      "支付宝",
		"type":      "alipay_direct",
		"color":     "rgba(var(--semi-blue-5), 1)",
		"min_topup": strconv.Itoa(setting.AlipayMinTopUp),
	})
}
```

同时在返回值里增加：

```go
"enable_alipay_topup": isAlipayConfigured(),
"alipay_min_topup":    setting.AlipayMinTopUp,
```

- [ ] **Step 6: 增加模型层字段保存的最小测试，确保新字段不会破坏老逻辑**

在 `model/topup_alipay_test.go` 先添加结构持久化测试：

```go
func TestTopUpPersistsPaymentModeAndProviderPayload(t *testing.T) {
	db := setupTopupModelTestDB(t)
	seedTopupModelUser(t, db, 1, 0)
	order := &TopUp{
		UserId:          1,
		Amount:          10,
		Money:           7.2,
		TradeNo:         "ALIPAY-TOPUP-1",
		PaymentMethod:   "alipay_direct",
		PaymentMode:     "qr",
		ProviderPayload: `{"source":"query"}`,
		Status:          common.TopUpStatusPending,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("failed to create order: %v", err)
	}
	var saved TopUp
	if err := db.First(&saved, "trade_no = ?", order.TradeNo).Error; err != nil {
		t.Fatalf("failed to query order: %v", err)
	}
	if saved.PaymentMode != "qr" || saved.ProviderPayload == "" {
		t.Fatalf("unexpected saved order: %+v", saved)
	}
}
```

Run:

```bash
timeout 60s go test ./controller ./model -run 'Test(GetTopUpInfoPrefersAlipayDirectOverLegacyAlipay|TopUpPersistsPaymentModeAndProviderPayload)' -count=1
```

Expected:

```text
ok   github.com/QuantumNous/new-api/controller
ok   github.com/QuantumNous/new-api/model
```

- [ ] **Step 7: Commit**

```bash
git add model/topup.go model/subscription.go model/option.go controller/topup.go controller/topup_alipay_test.go model/topup_alipay_test.go
git commit -m "feat: expose alipay direct payment metadata"
```

---

### Task 3: 实现钱包充值的金额预览、下单、查单、通知与入账

**Files:**
- Create: `controller/topup_alipay.go`
- Create: `controller/topup_alipay_notify_test.go`
- Modify: `model/topup.go`
- Modify: `router/api-router.go`
- Modify: `controller/topup_alipay_test.go`

- [ ] **Step 1: 先写钱包充值控制器的失败测试（RED）**

在 `controller/topup_alipay_test.go` 追加：

```go
type fakeAlipayClient struct {
	createPageOrderFunc func(ctx context.Context, req alipay.CreateOrderRequest) (*alipay.PageOrderResponse, error)
	createQROrderFunc   func(ctx context.Context, req alipay.CreateOrderRequest) (*alipay.QROrderResponse, error)
	queryOrderFunc      func(ctx context.Context, outTradeNo string) (*alipay.QueryOrderResult, error)
	verifyNotifyFunc    func(values map[string]string) (*alipay.NotificationResult, error)
}

func (f fakeAlipayClient) CreatePageOrder(ctx context.Context, req alipay.CreateOrderRequest) (*alipay.PageOrderResponse, error) {
	return f.createPageOrderFunc(ctx, req)
}
func (f fakeAlipayClient) CreateQROrder(ctx context.Context, req alipay.CreateOrderRequest) (*alipay.QROrderResponse, error) {
	return f.createQROrderFunc(ctx, req)
}
func (f fakeAlipayClient) QueryOrder(ctx context.Context, outTradeNo string) (*alipay.QueryOrderResult, error) {
	return f.queryOrderFunc(ctx, outTradeNo)
}
func (f fakeAlipayClient) VerifyNotification(values map[string]string) (*alipay.NotificationResult, error) {
	return f.verifyNotifyFunc(values)
}

func seedAlipayConfig() {
	setting.AlipayEnabled = true
	setting.AlipayAppID = "2026000000000000"
	setting.AlipayPrivateKey = "private-key"
	setting.AlipayPublicKey = "public-key"
	setting.AlipayUnitPrice = 7.2
	setting.AlipayMinTopUp = 1
	setting.AlipayOrderDescription = "账户充值"
}

func newAlipayNotifyContext(t *testing.T, form url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/alipay/notify", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ctx, recorder
}

func TestRequestAlipayPayReturnsPageURL(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedAlipayConfig()
	originalFactory := newAlipayClient
	newAlipayClient = func() (alipay.Client, error) {
		return fakeAlipayClient{
			createPageOrderFunc: func(_ context.Context, req alipay.CreateOrderRequest) (*alipay.PageOrderResponse, error) {
				if req.OutTradeNo == "" {
					t.Fatal("expected out trade no")
				}
				return &alipay.PageOrderResponse{PayURL: "https://openapi.alipay.com/gateway.do?foo=bar"}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newTopupTestContext(t, "POST", "/api/user/alipay/pay", map[string]any{
		"amount": 10,
		"payment_method": "alipay_direct",
		"pay_mode": "page",
	}, 1)
	RequestAlipayPay(ctx)

	if !strings.Contains(recorder.Body.String(), "pay_url") {
		t.Fatalf("expected pay_url in response: %s", recorder.Body.String())
	}
}
```

在 `controller/topup_alipay_notify_test.go` 新增：

```go
func TestAlipayNotifyCompletesRechargeOnce(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "ALIPAY-TOPUP-1", 72.00, 10, "alipay_direct")
	seedAlipayConfig()

	originalFactory := newAlipayClient
	newAlipayClient = func() (alipay.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values map[string]string) (*alipay.NotificationResult, error) {
				return &alipay.NotificationResult{
					OutTradeNo:      values["out_trade_no"],
					TradeNo:         "202604230001",
					TradeStatus:     "TRADE_SUCCESS",
					TotalAmount:     decimal.RequireFromString("72.00"),
					BuyerPayAmount:  decimal.RequireFromString("72.00"),
					RawForm:         "trade_status=TRADE_SUCCESS",
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
```

- [ ] **Step 2: 运行测试，确认路由与 handler 尚不存在**

Run:

```bash
timeout 60s go test ./controller -run 'Test(RequestAlipayPayReturnsPageURL|AlipayNotifyCompletesRechargeOnce)' -count=1
```

Expected:

```text
undefined: newAlipayClient
undefined: RequestAlipayPay
undefined: AlipayNotify
```

- [ ] **Step 3: 在 `controller/topup_alipay.go` 中实现请求结构、金额计算与 SDK 工厂**

```go
package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	paypkg "github.com/QuantumNous/new-api/pkg/alipay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

const paymentMethodAlipayDirect = "alipay_direct"

type AlipayPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	PayMode       string `json:"pay_mode"`
}

type AlipayQueryRequest struct {
	TradeNo string `json:"trade_no"`
}

var newAlipayClient = func() (paypkg.Client, error) {
	cfg := paypkg.Config{
		AppID:                        setting.AlipayAppID,
		PrivateKey:                   setting.AlipayPrivateKey,
		PublicKey:                    setting.AlipayPublicKey,
		Sandbox:                      setting.AlipaySandbox,
		DefaultNotifyURL:             firstNonEmpty(setting.AlipayNotifyURL, service.GetCallbackAddress()+"/api/alipay/notify"),
		DefaultReturnURL:             firstNonEmpty(setting.AlipayReturnURL, service.GetCallbackAddress()+"/console/topup"),
		DefaultSubscriptionReturnURL: firstNonEmpty(setting.AlipaySubscriptionReturnURL, service.GetCallbackAddress()+"/console/topup"),
		DefaultOrderDescription:      firstNonEmpty(setting.AlipayOrderDescription, "账户充值"),
	}
	return paypkg.NewClient(cfg)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func getAlipayPayMoney(amount int64, group string) (decimal.Decimal, int64) {
	normalized := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		normalized = normalized.Div(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}
	money := normalized.
		Mul(decimal.NewFromFloat(setting.AlipayUnitPrice)).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromFloat(discount)).
		Round(2)
	return money, normalized.IntPart()
}
```

- [ ] **Step 4: 实现钱包充值 amount / pay / query / notify handler**

在同文件继续加入：

```go
func RequestAlipayAmount(c *gin.Context) {
	var req AlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if !isAlipayConfigured() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "管理员未开启支付宝支付"})
		return
	}
	if req.Amount < int64(setting.AlipayMinTopUp) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", setting.AlipayMinTopUp)})
		return
	}
	id := c.GetInt("id")
	group, err := getWeChatPayUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	money, _ := getAlipayPayMoney(req.Amount, group)
	if money.LessThan(decimal.RequireFromString("0.01")) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	common.ApiSuccess(c, money.StringFixed(2))
}

func RequestAlipayPay(c *gin.Context) {
	var req AlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	mode, err := paypkg.NormalizePayMode(req.PayMode)
	if err != nil || req.PaymentMethod != paymentMethodAlipayDirect {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	id := c.GetInt("id")
	group, err := getWeChatPayUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	money, normalizedAmount := getAlipayPayMoney(req.Amount, group)
	client, err := newAlipayClient()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝支付配置不完整"})
		return
	}
	tradeNo := fmt.Sprintf("ALIPAY-TOPUP-%d-%d-%s", id, time.Now().UnixMilli(), randstr.String(6))
	topUp := &model.TopUp{
		UserId:        id,
		Amount:        normalizedAmount,
		Money:         money.InexactFloat64(),
		TradeNo:       tradeNo,
		PaymentMethod: paymentMethodAlipayDirect,
		PaymentMode:   mode,
		CreateTime:    time.Now().Unix(),
		Status:        common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	createReq := paypkg.CreateOrderRequest{
		OutTradeNo: tradeNo,
		Subject:    firstNonEmpty(setting.AlipayOrderDescription, "账户充值"),
		TotalAmount: money,
		NotifyURL:  firstNonEmpty(setting.AlipayNotifyURL, service.GetCallbackAddress()+"/api/alipay/notify"),
		ReturnURL:  firstNonEmpty(setting.AlipayReturnURL, service.GetCallbackAddress()+"/console/topup"),
	}
	if mode == paypkg.PayModePage {
		pageResp, err := client.CreatePageOrder(c.Request.Context(), createReq)
		if err != nil {
			topUp.Status = common.TopUpStatusFailed
			_ = topUp.Update()
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
			return
		}
		common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "pay_url": pageResp.PayURL, "amount_yuan": money.StringFixed(2), "pay_mode": mode})
		return
	}
	qrResp, err := client.CreateQROrder(c.Request.Context(), createReq)
	if err != nil {
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "qr_code": qrResp.QRCode, "amount_yuan": money.StringFixed(2), "pay_mode": mode})
}
```

以及：

```go
func QueryAlipayPay(c *gin.Context) {
	var req AlipayQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	topUp := model.GetTopUpByTradeNo(req.TradeNo)
	if topUp == nil || topUp.UserId != c.GetInt("id") || topUp.PaymentMethod != paymentMethodAlipayDirect {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值订单不存在"})
		return
	}
	client, err := newAlipayClient()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝支付配置不完整"})
		return
	}
	result, err := client.QueryOrder(c.Request.Context(), req.TradeNo)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "查单失败"})
		return
	}
	expected := decimal.NewFromFloat(topUp.Money).Round(2)
	if !result.TotalAmount.Equal(expected) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付金额校验失败"})
		return
	}
	if result.TradeStatus == "TRADE_SUCCESS" || result.TradeStatus == "TRADE_FINISHED" {
		LockOrder(req.TradeNo)
		defer UnlockOrder(req.TradeNo)
		if err := model.RechargeAlipay(req.TradeNo, common.GetJsonString(result)); err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
			return
		}
		common.ApiSuccess(c, gin.H{"status": "success"})
		return
	}
	common.ApiSuccess(c, gin.H{"status": "pending"})
}

func AlipayNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	values := make(map[string]string, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		values[key] = c.Request.PostForm.Get(key)
	}
	client, err := newAlipayClient()
	if err != nil {
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	result, err := client.VerifyNotification(values)
	if err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if err := result.ValidatePaid(); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	topUp := model.GetTopUpByTradeNo(result.OutTradeNo)
	if topUp == nil || topUp.PaymentMethod != paymentMethodAlipayDirect {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if !decimal.NewFromFloat(topUp.Money).Round(2).Equal(result.TotalAmount.Round(2)) {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	LockOrder(result.OutTradeNo)
	defer UnlockOrder(result.OutTradeNo)
	if err := model.RechargeAlipay(result.OutTradeNo, result.RawForm); err != nil {
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}
```

- [ ] **Step 5: 在 `model/topup.go` 中实现 `RechargeAlipay`，确保 `provider_payload` 与 `payment_mode` 能保留下来**

```go
func RechargeAlipay(tradeNo string, providerPayload string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	var quotaToAdd int
	topUp := &TopUp{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}
		if topUp.PaymentMethod != "alipay_direct" {
			return errors.New("支付渠道错误")
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}
		topUp.Status = common.TopUpStatusSuccess
		topUp.CompleteTime = common.GetTimestamp()
		if providerPayload != "" {
			topUp.ProviderPayload = providerPayload
		}
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error
	})
	if err != nil {
		return err
	}
	RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("支付宝充值成功，充值额度: %v，支付金额：%.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	return nil
}
```

- [ ] **Step 6: 注册钱包充值路由并补充查单/通知测试到绿色**

在 `router/api-router.go` 中加入：

```go
apiRouter.POST("/alipay/notify", controller.AlipayNotify)
selfRoute.POST("/alipay/amount", controller.RequestAlipayAmount)
selfRoute.POST("/alipay/pay", middleware.CriticalRateLimit(), controller.RequestAlipayPay)
selfRoute.POST("/alipay/query", controller.QueryAlipayPay)
```

在 `controller/topup_alipay_notify_test.go` 再追加金额不一致测试：

```go
func TestAlipayNotifyRejectsAmountMismatch(t *testing.T) {
	setupTopupControllerTestEnv(t)
	seedTopupUser(t, 1, "default")
	seedPendingTopup(t, "ALIPAY-TOPUP-1", 72.00, 10, "alipay_direct")
	seedAlipayConfig()
	originalFactory := newAlipayClient
	newAlipayClient = func() (alipay.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values map[string]string) (*alipay.NotificationResult, error) {
				return &alipay.NotificationResult{
					OutTradeNo:     values["out_trade_no"],
					TradeNo:        "202604230001",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("71.00"),
					BuyerPayAmount: decimal.RequireFromString("71.00"),
					RawForm:        "trade_status=TRADE_SUCCESS",
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
}
```

Run:

```bash
timeout 60s go test ./controller ./model -run 'Test(RequestAlipayPayReturnsPageURL|AlipayNotifyCompletesRechargeOnce|AlipayNotifyRejectsAmountMismatch|TopUpPersistsPaymentModeAndProviderPayload)' -count=1
```

Expected:

```text
ok   github.com/QuantumNous/new-api/controller
ok   github.com/QuantumNous/new-api/model
```

- [ ] **Step 7: Commit**

```bash
git add controller/topup_alipay.go controller/topup_alipay_test.go controller/topup_alipay_notify_test.go model/topup.go router/api-router.go
git commit -m "feat: add alipay direct topup flow"
```

---

### Task 4: 实现订阅支付的下单、查单、通知与完成逻辑

**Files:**
- Create: `controller/subscription_payment_alipay.go`
- Create: `controller/subscription_payment_alipay_test.go`
- Create: `controller/subscription_payment_alipay_notify_test.go`
- Modify: `model/subscription.go`
- Modify: `router/api-router.go`

- [ ] **Step 1: 先写订阅支付的失败测试（RED）**

```go
func setupSubscriptionControllerTestEnv(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db
	if err := db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}
}

func seedSubscriptionUser(t *testing.T, id int) {
	t.Helper()
	user := &model.User{Id: id, Username: fmt.Sprintf("sub-user-%d", id), Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Email: fmt.Sprintf("sub-user-%d@example.com", id), Group: "default"}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to seed subscription user: %v", err)
	}
}

func seedSubscriptionPlan(t *testing.T, id int, price float64) *model.SubscriptionPlan {
	t.Helper()
	plan := &model.SubscriptionPlan{Id: id, Title: fmt.Sprintf("Plan-%d", id), PriceAmount: price, Currency: "CNY", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true}
	if err := model.DB.Create(plan).Error; err != nil {
		t.Fatalf("failed to seed plan: %v", err)
	}
	return plan
}

func seedPendingSubscriptionOrder(t *testing.T, order *model.SubscriptionOrder) {
	t.Helper()
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("failed to seed subscription order: %v", err)
	}
}

func newSubscriptionTestContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
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

func newSubscriptionNotifyContext(t *testing.T, form url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/alipay/notify", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ctx, recorder
}

func TestSubscriptionRequestAlipayPayReturnsQRCode(t *testing.T) {
	setupSubscriptionControllerTestEnv(t)
	seedSubscriptionUser(t, 1)
	plan := seedSubscriptionPlan(t, 1, 88)
	seedAlipayConfig()
	originalFactory := newAlipayClient
	newAlipayClient = func() (alipay.Client, error) {
		return fakeAlipayClient{
			createQROrderFunc: func(_ context.Context, req alipay.CreateOrderRequest) (*alipay.QROrderResponse, error) {
				if req.TotalAmount.StringFixed(2) != "88.00" {
					t.Fatalf("expected 88.00, got %s", req.TotalAmount.StringFixed(2))
				}
				return &alipay.QROrderResponse{QRCode: "https://qr.alipay.com/test", TradeNo: req.OutTradeNo}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()

	ctx, recorder := newSubscriptionTestContext(t, "POST", "/api/subscription/alipay/pay", map[string]any{
		"plan_id": plan.Id,
		"payment_method": "alipay_direct",
		"pay_mode": "qr",
	}, 1)
	SubscriptionRequestAlipayPay(ctx)

	if !strings.Contains(recorder.Body.String(), "qr_code") {
		t.Fatalf("expected qr_code in response: %s", recorder.Body.String())
	}
}
```

- [ ] **Step 2: 运行测试，确认控制器尚不存在**

Run:

```bash
timeout 60s go test ./controller -run TestSubscriptionRequestAlipayPayReturnsQRCode -count=1
```

Expected:

```text
undefined: SubscriptionRequestAlipayPay
```

- [ ] **Step 3: 在 `controller/subscription_payment_alipay.go` 中实现订阅下单**

```go
package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	paypkg "github.com/QuantumNous/new-api/pkg/alipay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

type SubscriptionAlipayPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
	PayMode       string `json:"pay_mode"`
}

type SubscriptionAlipayQueryRequest struct {
	TradeNo string `json:"trade_no"`
}

func SubscriptionRequestAlipayPay(c *gin.Context) {
	var req SubscriptionAlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	mode, err := paypkg.NormalizePayMode(req.PayMode)
	if err != nil || req.PaymentMethod != paymentMethodAlipayDirect {
		common.ApiErrorMsg(c, "不支持的支付渠道")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}
	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}
	client, err := newAlipayClient()
	if err != nil {
		common.ApiErrorMsg(c, "支付宝支付配置不完整")
		return
	}
	tradeNo := "ALIPAY-SUB-" + randstr.String(8) + time.Now().Format("20060102150405")
	order := &model.SubscriptionOrder{
		UserId:        userId,
		PlanId:        plan.Id,
		Money:         plan.PriceAmount,
		TradeNo:       tradeNo,
		PaymentMethod: paymentMethodAlipayDirect,
		PaymentMode:   mode,
		CreateTime:    time.Now().Unix(),
		Status:        common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	createReq := paypkg.CreateOrderRequest{
		OutTradeNo: tradeNo,
		Subject:    plan.Title,
		TotalAmount: decimal.NewFromFloat(plan.PriceAmount).Round(2),
		NotifyURL:  firstNonEmpty(setting.AlipayNotifyURL, service.GetCallbackAddress()+"/api/subscription/alipay/notify"),
		ReturnURL:  firstNonEmpty(setting.AlipaySubscriptionReturnURL, service.GetCallbackAddress()+"/console/topup"),
	}
	if mode == paypkg.PayModePage {
		pageResp, err := client.CreatePageOrder(c.Request.Context(), createReq)
		if err != nil {
			_ = model.ExpireSubscriptionOrder(tradeNo)
			common.ApiErrorMsg(c, "拉起支付失败")
			return
		}
		common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "pay_url": pageResp.PayURL, "amount_yuan": createReq.TotalAmount.StringFixed(2), "pay_mode": mode})
		return
	}
	qrResp, err := client.CreateQROrder(c.Request.Context(), createReq)
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo)
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "qr_code": qrResp.QRCode, "amount_yuan": createReq.TotalAmount.StringFixed(2), "pay_mode": mode})
}
```

- [ ] **Step 4: 实现订阅查单与通知，并在成功时调用 `CompleteSubscriptionOrder`**

```go
func SubscriptionQueryAlipayPay(c *gin.Context) {
	var req SubscriptionAlipayQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	order := model.GetSubscriptionOrderByTradeNo(req.TradeNo)
	if order == nil || order.UserId != c.GetInt("id") || order.PaymentMethod != paymentMethodAlipayDirect {
		common.ApiErrorMsg(c, "订单不存在")
		return
	}
	client, err := newAlipayClient()
	if err != nil {
		common.ApiErrorMsg(c, "支付宝支付配置不完整")
		return
	}
	result, err := client.QueryOrder(c.Request.Context(), req.TradeNo)
	if err != nil {
		common.ApiErrorMsg(c, "查单失败")
		return
	}
	if !result.TotalAmount.Equal(decimal.NewFromFloat(order.Money).Round(2)) {
		common.ApiErrorMsg(c, "支付金额校验失败")
		return
	}
	if result.TradeStatus == "TRADE_SUCCESS" || result.TradeStatus == "TRADE_FINISHED" {
		LockOrder(req.TradeNo)
		defer UnlockOrder(req.TradeNo)
		if err := model.CompleteSubscriptionOrder(req.TradeNo, common.GetJsonString(result)); err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"status": "success"})
		return
	}
	common.ApiSuccess(c, gin.H{"status": "pending"})
}

func SubscriptionAlipayNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	values := make(map[string]string, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		values[key] = c.Request.PostForm.Get(key)
	}
	client, err := newAlipayClient()
	if err != nil {
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	result, err := client.VerifyNotification(values)
	if err != nil || result.ValidatePaid() != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	order := model.GetSubscriptionOrderByTradeNo(result.OutTradeNo)
	if order == nil || order.PaymentMethod != paymentMethodAlipayDirect {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if !decimal.NewFromFloat(order.Money).Round(2).Equal(result.TotalAmount.Round(2)) {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	LockOrder(result.OutTradeNo)
	defer UnlockOrder(result.OutTradeNo)
	if err := model.CompleteSubscriptionOrder(result.OutTradeNo, result.RawForm); err != nil {
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}
```

- [ ] **Step 5: 补一条 `CompleteSubscriptionOrder` 透传 `payment_mode` 的测试并修正 `upsertSubscriptionTopUpTx`**

在 `model/subscription.go` 的 `upsertSubscriptionTopUpTx` 中补上：

```go
if topup.PaymentMode == "" {
	topup.PaymentMode = order.PaymentMode
}
if order.ProviderPayload != "" {
	topup.ProviderPayload = order.ProviderPayload
}
```

并在 `controller/subscription_payment_alipay_notify_test.go` 中加入：

```go
func TestSubscriptionAlipayNotifyCompletesOrderOnce(t *testing.T) {
	setupSubscriptionControllerTestEnv(t)
	seedSubscriptionUser(t, 1)
	plan := seedSubscriptionPlan(t, 1, 88)
	seedPendingSubscriptionOrder(t, &model.SubscriptionOrder{
		UserId:        1,
		PlanId:        plan.Id,
		Money:         88,
		TradeNo:       "ALIPAY-SUB-1",
		PaymentMethod: "alipay_direct",
		PaymentMode:   "page",
		Status:        common.TopUpStatusPending,
	})
	seedAlipayConfig()
	originalFactory := newAlipayClient
	newAlipayClient = func() (alipay.Client, error) {
		return fakeAlipayClient{
			verifyNotifyFunc: func(values map[string]string) (*alipay.NotificationResult, error) {
				return &alipay.NotificationResult{
					OutTradeNo:     values["out_trade_no"],
					TradeNo:        "202604230099",
					TradeStatus:    "TRADE_SUCCESS",
					TotalAmount:    decimal.RequireFromString("88.00"),
					BuyerPayAmount: decimal.RequireFromString("88.00"),
					RawForm:        "trade_status=TRADE_SUCCESS",
				}, nil
			},
		}, nil
	}
	defer func() { newAlipayClient = originalFactory }()
	ctx, recorder := newSubscriptionNotifyContext(t, url.Values{
		"out_trade_no": []string{"ALIPAY-SUB-1"},
		"sign":         []string{"signed"},
	})
	SubscriptionAlipayNotify(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
}
```

- [ ] **Step 6: 注册订阅支付宝路由并把测试跑绿**

在 `router/api-router.go` 中加入：

```go
subscriptionRoute.POST("/alipay/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestAlipayPay)
subscriptionRoute.POST("/alipay/query", controller.SubscriptionQueryAlipayPay)
apiRouter.POST("/subscription/alipay/notify", controller.SubscriptionAlipayNotify)
```

Run:

```bash
timeout 60s go test ./controller ./model -run 'Test(SubscriptionRequestAlipayPayReturnsQRCode|SubscriptionAlipayNotifyCompletesOrderOnce)' -count=1
```

Expected:

```text
ok   github.com/QuantumNous/new-api/controller
ok   github.com/QuantumNous/new-api/model
```

- [ ] **Step 7: Commit**

```bash
git add controller/subscription_payment_alipay.go controller/subscription_payment_alipay_test.go controller/subscription_payment_alipay_notify_test.go model/subscription.go router/api-router.go
git commit -m "feat: add alipay direct subscription flow"
```

---

### Task 5: 增加后台支付宝官方直连设置页

**Files:**
- Create: `web/src/pages/Setting/Payment/SettingsPaymentGatewayAlipay.jsx`
- Modify: `web/src/components/settings/PaymentSetting.jsx`
- Modify: `web/src/i18n/locales/zh-CN.json`
- Modify: `web/src/i18n/locales/en.json`

- [ ] **Step 1: 先写设置页最小 UI 与保存字段，沿用微信支付设置卡片模式**

创建 `web/src/pages/Setting/Payment/SettingsPaymentGatewayAlipay.jsx`：

```jsx
import React, { useEffect, useRef, useState } from 'react';
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import { API, removeTrailingSlash, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentGatewayAlipay(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const formApiRef = useRef(null);
  const [inputs, setInputs] = useState({
    AlipayEnabled: false,
    AlipaySandbox: false,
    AlipayAppID: '',
    AlipayPrivateKey: '',
    AlipayPublicKey: '',
    AlipayUnitPrice: 1,
    AlipayMinTopUp: 1,
    AlipayNotifyURL: '',
    AlipayReturnURL: '',
    AlipaySubscriptionReturnURL: '',
    AlipayOrderDescription: '',
  });

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const current = {
        AlipayEnabled: props.options.AlipayEnabled === true || props.options.AlipayEnabled === 'true',
        AlipaySandbox: props.options.AlipaySandbox === true || props.options.AlipaySandbox === 'true',
        AlipayAppID: props.options.AlipayAppID || '',
        AlipayPrivateKey: props.options.AlipayPrivateKey || '',
        AlipayPublicKey: props.options.AlipayPublicKey || '',
        AlipayUnitPrice: parseFloat(props.options.AlipayUnitPrice || 1),
        AlipayMinTopUp: parseFloat(props.options.AlipayMinTopUp || 1),
        AlipayNotifyURL: props.options.AlipayNotifyURL || '',
        AlipayReturnURL: props.options.AlipayReturnURL || '',
        AlipaySubscriptionReturnURL: props.options.AlipaySubscriptionReturnURL || '',
        AlipayOrderDescription: props.options.AlipayOrderDescription || '',
      };
      setInputs(current);
      formApiRef.current.setValues(current);
    }
  }, [props.options]);

  const submitAlipay = async () => {
    setLoading(true);
    try {
      const options = [
        { key: 'AlipayEnabled', value: inputs.AlipayEnabled ? 'true' : 'false' },
        { key: 'AlipaySandbox', value: inputs.AlipaySandbox ? 'true' : 'false' },
        { key: 'AlipayAppID', value: inputs.AlipayAppID || '' },
        { key: 'AlipayUnitPrice', value: String(inputs.AlipayUnitPrice || 1) },
        { key: 'AlipayMinTopUp', value: String(inputs.AlipayMinTopUp || 1) },
        { key: 'AlipayNotifyURL', value: removeTrailingSlash(inputs.AlipayNotifyURL || '') },
        { key: 'AlipayReturnURL', value: removeTrailingSlash(inputs.AlipayReturnURL || '') },
        { key: 'AlipaySubscriptionReturnURL', value: removeTrailingSlash(inputs.AlipaySubscriptionReturnURL || '') },
        { key: 'AlipayOrderDescription', value: inputs.AlipayOrderDescription || '' },
      ];
      if (inputs.AlipayPrivateKey) {
        options.push({ key: 'AlipayPrivateKey', value: inputs.AlipayPrivateKey });
      }
      if (inputs.AlipayPublicKey) {
        options.push({ key: 'AlipayPublicKey', value: inputs.AlipayPublicKey });
      }
      const results = await Promise.all(options.map((item) => API.put('/api/option/', item)));
      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => showError(res.data.message));
      } else {
        showSuccess(t('更新成功'));
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Spin spinning={loading}>
      <Form initValues={inputs} onValueChange={setInputs} getFormApi={(api) => (formApiRef.current = api)}>
        <Form.Section text={t('支付宝官方直连设置')}>
          <Banner type='info' description={t('推荐先在沙箱环境验证 page 与 qr 两种模式，再切换正式环境。')} />
          <Row gutter={16} style={{ marginTop: 16 }}>
            <Col span={8}><Form.Switch field='AlipayEnabled' label={t('启用支付宝官方直连')} /></Col>
            <Col span={8}><Form.Switch field='AlipaySandbox' label={t('启用沙箱模式')} /></Col>
            <Col span={8}><Form.Input field='AlipayAppID' label={t('支付宝 AppID')} /></Col>
          </Row>
          <Row gutter={16} style={{ marginTop: 16 }}>
            <Col span={12}><Form.Input field='AlipayPrivateKey' type='password' label={t('应用私钥')} /></Col>
            <Col span={12}><Form.Input field='AlipayPublicKey' type='password' label={t('支付宝公钥')} /></Col>
          </Row>
          <Row gutter={16} style={{ marginTop: 16 }}>
            <Col span={6}><Form.InputNumber field='AlipayUnitPrice' label={t('单价（元）')} min={0.01} step={0.01} /></Col>
            <Col span={6}><Form.InputNumber field='AlipayMinTopUp' label={t('最小充值数量')} min={1} step={1} /></Col>
            <Col span={12}><Form.Input field='AlipayOrderDescription' label={t('订单描述')} /></Col>
          </Row>
          <Row gutter={16} style={{ marginTop: 16 }}>
            <Col span={8}><Form.Input field='AlipayNotifyURL' label={t('异步通知地址')} /></Col>
            <Col span={8}><Form.Input field='AlipayReturnURL' label={t('充值回跳地址')} /></Col>
            <Col span={8}><Form.Input field='AlipaySubscriptionReturnURL' label={t('订阅回跳地址')} /></Col>
          </Row>
          <Button style={{ marginTop: 16 }} onClick={submitAlipay}>{t('保存支付宝设置')}</Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
```

- [ ] **Step 2: 在 `PaymentSetting.jsx` 中挂载支付宝配置与初始值解析**

加入 import：

```jsx
import SettingsPaymentGatewayAlipay from '../../pages/Setting/Payment/SettingsPaymentGatewayAlipay';
```

在 `inputs` 初始值里加入：

```jsx
AlipayEnabled: false,
AlipaySandbox: false,
AlipayAppID: '',
AlipayPrivateKey: '',
AlipayPublicKey: '',
AlipayUnitPrice: 1,
AlipayMinTopUp: 1,
AlipayNotifyURL: '',
AlipayReturnURL: '',
AlipaySubscriptionReturnURL: '',
AlipayOrderDescription: '',
```

在数值解析 switch 里加入：

```jsx
case 'AlipayUnitPrice':
case 'AlipayMinTopUp':
  newInputs[item.key] = parseFloat(item.value);
  break;
```

在渲染里加入卡片：

```jsx
<Card style={{ marginTop: '10px' }}>
  <SettingsPaymentGatewayAlipay options={inputs} refresh={onRefresh} />
</Card>
```

- [ ] **Step 3: 增加中英文文案，避免页面上出现原始 key**

在 `web/src/i18n/locales/zh-CN.json` 追加：

```json
{
  "支付宝官方直连设置": "支付宝官方直连设置",
  "推荐先在沙箱环境验证 page 与 qr 两种模式，再切换正式环境。": "推荐先在沙箱环境验证 page 与 qr 两种模式，再切换正式环境。",
  "启用支付宝官方直连": "启用支付宝官方直连",
  "启用沙箱模式": "启用沙箱模式",
  "支付宝 AppID": "支付宝 AppID",
  "应用私钥": "应用私钥",
  "支付宝公钥": "支付宝公钥",
  "单价（元）": "单价（元）",
  "最小充值数量": "最小充值数量",
  "订单描述": "订单描述",
  "异步通知地址": "异步通知地址",
  "充值回跳地址": "充值回跳地址",
  "订阅回跳地址": "订阅回跳地址",
  "保存支付宝设置": "保存支付宝设置"
}
```

在 `web/src/i18n/locales/en.json` 追加：

```json
{
  "支付宝官方直连设置": "Alipay Direct Settings",
  "推荐先在沙箱环境验证 page 与 qr 两种模式，再切换正式环境。": "Validate page and QR modes in Alipay sandbox before switching to production.",
  "启用支付宝官方直连": "Enable Alipay Direct",
  "启用沙箱模式": "Enable Sandbox",
  "支付宝 AppID": "Alipay AppID",
  "应用私钥": "App Private Key",
  "支付宝公钥": "Alipay Public Key",
  "单价（元）": "Unit Price (CNY)",
  "最小充值数量": "Minimum Top-up Quantity",
  "订单描述": "Order Description",
  "异步通知地址": "Notify URL",
  "充值回跳地址": "Top-up Return URL",
  "订阅回跳地址": "Subscription Return URL",
  "保存支付宝设置": "Save Alipay Settings"
}
```

- [ ] **Step 4: 跑前端构建确认设置页无语法错误**

Run:

```bash
cd web && bun run build
```

Expected:

```text
✓ built in
```

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/Setting/Payment/SettingsPaymentGatewayAlipay.jsx web/src/components/settings/PaymentSetting.jsx web/src/i18n/locales/zh-CN.json web/src/i18n/locales/en.json
git commit -m "feat: add alipay direct admin settings"
```

---

### Task 6: 接入钱包充值页的支付宝官方直连弹窗、轮询与手动补查

**Files:**
- Create: `web/src/components/topup/modals/AlipayCheckoutModal.jsx`
- Modify: `web/src/components/topup/index.jsx`
- Modify: `web/src/components/topup/RechargeCard.jsx`
- Modify: `web/src/components/topup/modals/PaymentConfirmModal.jsx`
- Modify: `web/src/components/topup/modals/TopupHistoryModal.jsx`
- Modify: `web/src/i18n/locales/zh-CN.json`
- Modify: `web/src/i18n/locales/en.json`

- [ ] **Step 1: 先创建通用支付宝弹窗组件，承载模式选择、二维码与状态检查**

创建 `web/src/components/topup/modals/AlipayCheckoutModal.jsx`：

```jsx
import React, { useEffect, useMemo, useState } from 'react';
import { Button, Modal, Radio, Space, Typography } from '@douyinfe/semi-ui';
import { QRCodeSVG } from 'qrcode.react';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export default function AlipayCheckoutModal({
  visible,
  title,
  amount,
  tradeNo,
  payMode,
  defaultMode = 'page',
  qrCode,
  checking,
  creating,
  onClose,
  onCreate,
  onCheck,
}) {
  const { t } = useTranslation();
  const [selectedMode, setSelectedMode] = useState(defaultMode);

  useEffect(() => {
    if (visible) {
      setSelectedMode(defaultMode || 'page');
    }
  }, [visible, defaultMode]);

  const showQRCode = visible && payMode === 'qr' && !!qrCode;
  const showManualCheck = visible && !!tradeNo;

  return (
    <Modal visible={visible} title={title} footer={null} centered maskClosable={false} onCancel={onClose}>
      <div className='flex flex-col gap-3'>
        {!tradeNo && (
          <>
            <Text>{t('选择支付方式')}</Text>
            <Radio.Group value={selectedMode} onChange={(event) => setSelectedMode(event.target.value)}>
              <Space vertical>
                <Radio value='page'>{t('支付宝收银台支付')}</Radio>
                <Radio value='qr'>{t('支付宝扫码支付')}</Radio>
              </Space>
            </Radio.Group>
            <Text>{t('应付金额')}：¥{amount}</Text>
            <Button type='primary' loading={creating} onClick={() => onCreate(selectedMode)}>{t('确认并发起支付')}</Button>
          </>
        )}
        {tradeNo && (
          <>
            <Text>{t('订单号')}：{tradeNo}</Text>
            <Text>{t('应付金额')}：¥{amount}</Text>
            {showQRCode && <QRCodeSVG value={qrCode} size={220} />}
            <Text type='tertiary'>
              {selectedMode === 'page'
                ? t('已打开支付宝收银台，请完成支付后返回检查状态。')
                : t('请使用支付宝扫码支付，支付成功后到账可能有短暂延迟。')}
            </Text>
            {showManualCheck && (
              <Button loading={checking} onClick={onCheck}>{t('我已完成支付，检查状态')}</Button>
            )}
            <Button onClick={onClose}>{t('关闭')}</Button>
          </>
        )}
      </div>
    </Modal>
  );
}
```

- [ ] **Step 2: 在 `topup/index.jsx` 中接入支付宝状态与下单逻辑**

增加状态：

```jsx
const [enableAlipayTopUp, setEnableAlipayTopUp] = useState(false);
const [alipayMinTopUp, setAlipayMinTopUp] = useState(1);
const [alipayPayOpen, setAlipayPayOpen] = useState(false);
const [alipayPayData, setAlipayPayData] = useState({ tradeNo: '', qrCode: '', amount: '0.00', payMode: '' });
const [alipayCreating, setAlipayCreating] = useState(false);
const [alipayChecking, setAlipayChecking] = useState(false);
```

在 `getTopupInfo` 成功回调里加入：

```jsx
const enableAlipayTopUp = data.enable_alipay_topup || false;
setEnableAlipayTopUp(enableAlipayTopUp);
setAlipayMinTopUp(data.alipay_min_topup || 1);
```

修改 `preTopUp`：

```jsx
} else if (payment === 'alipay_direct') {
  if (!enableAlipayTopUp) {
    showError(t('管理员未开启支付宝支付！'));
    return;
  }
  if (topUpCount < alipayMinTopUp) {
    showError(t('充值数量不能小于') + alipayMinTopUp);
    return;
  }
  setPayWay(payment);
  setAlipayPayData({ tradeNo: '', qrCode: '', amount: Number(amount || 0).toFixed(2), payMode: '' });
  setAlipayPayOpen(true);
  return;
}
```

新增创建与查询函数：

```jsx
const createAlipayTopup = async (payMode) => {
  setAlipayCreating(true);
  try {
    const res = await API.post('/api/user/alipay/pay', {
      amount: parseInt(topUpCount),
      payment_method: 'alipay_direct',
      pay_mode: payMode,
    });
    if (res.data?.success) {
      const data = res.data.data || {};
      setAlipayPayData({
        tradeNo: data.trade_no || '',
        qrCode: data.qr_code || '',
        amount: data.amount_yuan || Number(amount || 0).toFixed(2),
        payMode: data.pay_mode || payMode,
      });
      if (data.pay_url) {
        window.open(data.pay_url, '_blank');
        showSuccess(t('已打开支付页面'));
      }
    } else {
      showError(res.data?.data || t('支付请求失败'));
    }
  } catch (error) {
    showError(t('支付请求失败'));
  } finally {
    setAlipayCreating(false);
  }
};

const queryAlipayTopup = async () => {
  if (!alipayPayData.tradeNo) return;
  setAlipayChecking(true);
  try {
    const res = await API.post('/api/user/alipay/query', { trade_no: alipayPayData.tradeNo });
    if (res.data?.success && res.data?.data?.status === 'success') {
      showSuccess(t('支付成功'));
      setAlipayPayOpen(false);
      await getUserQuota();
      setOpenHistory(true);
      return;
    }
    if (res.data?.success && res.data?.data?.status === 'pending') {
      showInfo(t('等待支付中'));
      return;
    }
    showError(res.data?.message || res.data?.data || t('查单失败'));
  } catch (error) {
    showError(t('查单失败'));
  } finally {
    setAlipayChecking(false);
  }
};
```

- [ ] **Step 3: 给支付宝二维码/收银台状态弹窗加自动轮询**

在 `topup/index.jsx` 中增加：

```jsx
useEffect(() => {
  if (!alipayPayOpen || !alipayPayData.tradeNo) return;
  const timer = setInterval(() => {
    queryAlipayTopup().then();
  }, 5000);
  return () => clearInterval(timer);
}, [alipayPayOpen, alipayPayData.tradeNo]);
```

并在渲染区加入：

```jsx
<AlipayCheckoutModal
  visible={alipayPayOpen}
  title={t('支付宝支付')}
  amount={alipayPayData.amount}
  tradeNo={alipayPayData.tradeNo}
  qrCode={alipayPayData.qrCode}
  payMode={alipayPayData.payMode}
  creating={alipayCreating}
  checking={alipayChecking}
  onClose={() => setAlipayPayOpen(false)}
  onCreate={createAlipayTopup}
  onCheck={queryAlipayTopup}
/>
```

- [ ] **Step 4: 修正支付方式渲染与账单名称映射**

在 `RechargeCard.jsx` 中把 Alipay 图标判断改成同时支持历史和官方：

```jsx
payMethod.type === 'alipay' || payMethod.type === 'alipay_direct' ? (
  <SiAlipay size={18} color='#1677FF' />
)
```

在 `PaymentConfirmModal.jsx` 中把文案判断改成：

```jsx
if (payWay === 'alipay' || payWay === 'alipay_direct') {
```

在 `TopupHistoryModal.jsx` 中更新映射：

```jsx
const PAYMENT_METHOD_MAP = {
  stripe: 'Stripe',
  creem: 'Creem',
  waffo: 'Waffo',
  alipay: '支付宝（易支付）',
  alipay_direct: '支付宝',
  wxpay: '微信',
  wechat_pay: '微信支付',
};
```

- [ ] **Step 5: 补齐充值页相关文案并执行构建验证**

在 `zh-CN.json` / `en.json` 中新增：

```json
{
  "支付宝支付": "支付宝支付",
  "支付宝收银台支付": "支付宝收银台支付",
  "支付宝扫码支付": "支付宝扫码支付",
  "确认并发起支付": "确认并发起支付",
  "已打开支付宝收银台，请完成支付后返回检查状态。": "已打开支付宝收银台，请完成支付后返回检查状态。",
  "请使用支付宝扫码支付，支付成功后到账可能有短暂延迟。": "请使用支付宝扫码支付，支付成功后到账可能有短暂延迟。",
  "我已完成支付，检查状态": "我已完成支付，检查状态",
  "等待支付中": "等待支付中",
  "支付成功": "支付成功",
  "管理员未开启支付宝支付！": "管理员未开启支付宝支付！",
  "支付宝（易支付）": "支付宝（易支付）"
}
```

Run:

```bash
cd web && bun run build
```

Expected:

```text
✓ built in
```

- [ ] **Step 6: Commit**

```bash
git add web/src/components/topup/modals/AlipayCheckoutModal.jsx web/src/components/topup/index.jsx web/src/components/topup/RechargeCard.jsx web/src/components/topup/modals/PaymentConfirmModal.jsx web/src/components/topup/modals/TopupHistoryModal.jsx web/src/i18n/locales/zh-CN.json web/src/i18n/locales/en.json
git commit -m "feat: add alipay direct topup ui"
```

---

### Task 7: 接入订阅购买页的支付宝官方直连与全量验证

**Files:**
- Modify: `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`
- Modify: `web/src/components/topup/SubscriptionPlansCard.jsx`
- Modify: `web/src/components/topup/index.jsx`
- Modify: `web/src/i18n/locales/zh-CN.json`
- Modify: `web/src/i18n/locales/en.json`

- [ ] **Step 1: 先在订阅购买弹窗中增加支付宝入口和状态选择占位**

在 `SubscriptionPurchaseModal.jsx` 中加入 props：

```jsx
enableAlipayTopUp = false,
onPayAlipay,
```

并在顶部 import：

```jsx
import { SiAlipay, SiStripe } from 'react-icons/si';
```

在 `hasAnyPayment` 计算中加入：

```jsx
const hasAlipay = enableAlipayTopUp;
const hasAnyPayment = hasStripe || hasCreem || hasEpay || hasAlipay;
```

在支付按钮区加入：

```jsx
{hasAlipay && (
  <Button
    theme='light'
    className='flex-1'
    icon={<SiAlipay size={14} color='#1677FF' />}
    onClick={onPayAlipay}
    loading={paying}
    disabled={purchaseLimitReached}
  >
    {t('支付宝')}
  </Button>
)}
```

- [ ] **Step 2: 在 `SubscriptionPlansCard.jsx` 中复用 `AlipayCheckoutModal` 处理订阅下单与查单**

增加状态：

```jsx
const [alipayOpen, setAlipayOpen] = useState(false);
const [alipayPayData, setAlipayPayData] = useState({ tradeNo: '', qrCode: '', amount: '0.00', payMode: '' });
const [alipayCreating, setAlipayCreating] = useState(false);
const [alipayChecking, setAlipayChecking] = useState(false);
```

增加函数：

```jsx
const openAlipay = () => {
  if (!selectedPlan?.plan) {
    showError(t('请选择套餐'));
    return;
  }
  setAlipayPayData({
    tradeNo: '',
    qrCode: '',
    amount: Number(selectedPlan.plan.price_amount || 0).toFixed(2),
    payMode: '',
  });
  setAlipayOpen(true);
};

const createSubscriptionAlipay = async (payMode) => {
  setAlipayCreating(true);
  try {
    const res = await API.post('/api/subscription/alipay/pay', {
      plan_id: selectedPlan.plan.id,
      payment_method: 'alipay_direct',
      pay_mode: payMode,
    });
    if (res.data?.success) {
      const data = res.data.data || {};
      setAlipayPayData({
        tradeNo: data.trade_no || '',
        qrCode: data.qr_code || '',
        amount: data.amount_yuan || Number(selectedPlan.plan.price_amount || 0).toFixed(2),
        payMode: data.pay_mode || payMode,
      });
      if (data.pay_url) {
        window.open(data.pay_url, '_blank');
        showSuccess(t('已打开支付页面'));
      }
    } else {
      showError(res.data?.message || res.data?.data || t('支付失败'));
    }
  } catch (e) {
    showError(t('支付请求失败'));
  } finally {
    setAlipayCreating(false);
  }
};

const querySubscriptionAlipay = async () => {
  if (!alipayPayData.tradeNo) return;
  setAlipayChecking(true);
  try {
    const res = await API.post('/api/subscription/alipay/query', { trade_no: alipayPayData.tradeNo });
    if (res.data?.success && res.data?.data?.status === 'success') {
      showSuccess(t('支付成功'));
      setAlipayOpen(false);
      closeBuy();
      await reloadSubscriptionSelf?.();
      return;
    }
    if (res.data?.success && res.data?.data?.status === 'pending') {
      showError(t('等待支付中'));
      return;
    }
    showError(res.data?.message || res.data?.data || t('查单失败'));
  } catch (e) {
    showError(t('查单失败'));
  } finally {
    setAlipayChecking(false);
  }
};
```

并在组件末尾加入：

```jsx
<AlipayCheckoutModal
  visible={alipayOpen}
  title={t('订阅支付宝支付')}
  amount={alipayPayData.amount}
  tradeNo={alipayPayData.tradeNo}
  qrCode={alipayPayData.qrCode}
  payMode={alipayPayData.payMode}
  creating={alipayCreating}
  checking={alipayChecking}
  onClose={() => setAlipayOpen(false)}
  onCreate={createSubscriptionAlipay}
  onCheck={querySubscriptionAlipay}
/>
```

- [ ] **Step 3: 在 `topup/index.jsx` 向订阅组件透传支付宝开关**

把 props 增加为：

```jsx
<SubscriptionPlansCard
  t={t}
  loading={subscriptionLoading}
  plans={subscriptionPlans}
  payMethods={payMethods}
  enableOnlineTopUp={enableOnlineTopUp}
  enableStripeTopUp={enableStripeTopUp}
  enableCreemTopUp={enableCreemTopUp}
  enableAlipayTopUp={enableAlipayTopUp}
  billingPreference={billingPreference}
  onChangeBillingPreference={updateBillingPreference}
  activeSubscriptions={activeSubscriptions}
  allSubscriptions={allSubscriptions}
  reloadSubscriptionSelf={getSubscriptionSelf}
/>
```

并把 `RechargeCard` 传给 `SubscriptionPlansCard` 的参数链保持一致。

- [ ] **Step 4: 给订阅支付宝增加自动轮询并完成端到端验证**

在 `SubscriptionPlansCard.jsx` 中加入：

```jsx
useEffect(() => {
  if (!alipayOpen || !alipayPayData.tradeNo) return;
  const timer = setInterval(() => {
    querySubscriptionAlipay().then();
  }, 5000);
  return () => clearInterval(timer);
}, [alipayOpen, alipayPayData.tradeNo]);
```

在 `SubscriptionPurchaseModal` 调用处加入：

```jsx
<SubscriptionPurchaseModal
  t={t}
  visible={open}
  onCancel={closeBuy}
  selectedPlan={selectedPlan}
  paying={paying}
  selectedEpayMethod={selectedEpayMethod}
  setSelectedEpayMethod={setSelectedEpayMethod}
  epayMethods={epayMethods}
  enableOnlineTopUp={enableOnlineTopUp}
  enableStripeTopUp={enableStripeTopUp}
  enableCreemTopUp={enableCreemTopUp}
  enableAlipayTopUp={enableAlipayTopUp}
  purchaseLimitInfo={selectedPlan?.purchaseLimitInfo}
  onPayStripe={payStripe}
  onPayCreem={payCreem}
  onPayEpay={payEpay}
  onPayAlipay={openAlipay}
/>
```

- [ ] **Step 5: 跑后后端测试和前端构建，确认全链路没有回归**

Run:

```bash
timeout 60s go test ./pkg/alipay ./controller ./model -count=1
cd web && bun run build
```

Expected:

```text
ok   github.com/QuantumNous/new-api/pkg/alipay
ok   github.com/QuantumNous/new-api/controller
ok   github.com/QuantumNous/new-api/model
✓ built in
```

- [ ] **Step 6: Commit**

```bash
git add web/src/components/topup/modals/SubscriptionPurchaseModal.jsx web/src/components/topup/SubscriptionPlansCard.jsx web/src/components/topup/index.jsx web/src/i18n/locales/zh-CN.json web/src/i18n/locales/en.json
git commit -m "feat: add alipay direct subscription ui"
```

---

## Self-Review

### Spec coverage

- 配置项与后台设置：Task 1、Task 2、Task 5
- 钱包充值 page/qr/查单/通知：Task 3、Task 6
- 订阅购买 page/qr/查单/通知：Task 4、Task 7
- 官方优先隐藏 EPay `alipay`：Task 2
- 金额校验、幂等、provider payload：Task 2、Task 3、Task 4
- 自动轮询 + 手动补查：Task 6、Task 7
- SQLite/MySQL/PostgreSQL 兼容：Task 2、Task 3、Task 4

### Placeholder scan

已检查本计划无占位描述。

### Type consistency

- 渠道标识统一使用 `alipay_direct`
- 支付模式统一使用 `page` / `qr`
- 钱包充值查单函数统一使用 `QueryAlipayPay`
- 订阅查单函数统一使用 `SubscriptionQueryAlipayPay`
- 支付宝弹窗组件统一使用 `AlipayCheckoutModal`

