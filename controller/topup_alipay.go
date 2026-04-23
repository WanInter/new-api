package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	alipaypkg "github.com/QuantumNous/new-api/pkg/alipay"
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

var newAlipayClient = func() (alipaypkg.Client, error) {
	cfg := alipaypkg.Config{
		AppID:                        setting.AlipayAppID,
		PrivateKey:                   setting.AlipayPrivateKey,
		PublicKey:                    setting.AlipayPublicKey,
		Sandbox:                      setting.AlipaySandbox,
		DefaultNotifyURL:             firstNonEmpty(setting.AlipayNotifyURL, service.GetCallbackAddress()+"/api/alipay/notify"),
		DefaultReturnURL:             firstNonEmpty(setting.AlipayReturnURL, service.GetCallbackAddress()+"/console/topup"),
		DefaultSubscriptionReturnURL: firstNonEmpty(setting.AlipaySubscriptionReturnURL, service.GetCallbackAddress()+"/console/topup"),
		DefaultOrderDescription:      firstNonEmpty(setting.AlipayOrderDescription, "账户充值"),
	}
	return alipaypkg.NewClient(cfg)
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
	mode, err := alipaypkg.NormalizePayMode(req.PayMode)
	if err != nil || req.PaymentMethod != paymentMethodAlipayDirect {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
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
	money, normalizedAmount := getAlipayPayMoney(req.Amount, group)
	if money.LessThan(decimal.RequireFromString("0.01")) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
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

	createReq := alipaypkg.CreateOrderRequest{
		OutTradeNo:  tradeNo,
		Subject:     firstNonEmpty(setting.AlipayOrderDescription, "账户充值"),
		TotalAmount: money,
		NotifyURL:   firstNonEmpty(setting.AlipayNotifyURL, service.GetCallbackAddress()+"/api/alipay/notify"),
		ReturnURL:   firstNonEmpty(setting.AlipayReturnURL, service.GetCallbackAddress()+"/console/topup"),
	}
	if mode == alipaypkg.PayModePage {
		pageResp, err := client.CreatePageOrder(c.Request.Context(), createReq)
		if err != nil {
			topUp.Status = common.TopUpStatusFailed
			_ = topUp.Update()
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
			return
		}
		common.ApiSuccess(c, gin.H{
			"trade_no":    tradeNo,
			"pay_url":     pageResp.PayURL,
			"amount_yuan": money.StringFixed(2),
			"pay_mode":    mode,
		})
		return
	}

	qrResp, err := client.CreateQROrder(c.Request.Context(), createReq)
	if err != nil {
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	common.ApiSuccess(c, gin.H{
		"trade_no":    tradeNo,
		"qr_code":     qrResp.QRCode,
		"amount_yuan": money.StringFixed(2),
		"pay_mode":    mode,
	})
}

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
	client, err := newAlipayClient()
	if err != nil {
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	result, err := client.VerifyNotification(c.Request.PostForm)
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
