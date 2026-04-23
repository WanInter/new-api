package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	alipaypkg "github.com/QuantumNous/new-api/pkg/alipay"
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
	mode, err := alipaypkg.NormalizePayMode(req.PayMode)
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
	if !isAlipayConfigured() {
		common.ApiErrorMsg(c, "支付宝支付配置不完整")
		return
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
	createReq := alipaypkg.CreateOrderRequest{
		OutTradeNo:  tradeNo,
		Subject:     plan.Title,
		TotalAmount: decimal.NewFromFloat(plan.PriceAmount).Round(2),
		NotifyURL:   service.GetCallbackAddress() + "/api/subscription/alipay/notify",
		ReturnURL:   firstNonEmpty(setting.AlipaySubscriptionReturnURL, service.GetCallbackAddress()+"/console/topup"),
	}
	if mode == alipaypkg.PayModePage {
		pageResp, err := client.CreatePageOrder(c.Request.Context(), createReq)
		if err != nil {
			common.ApiErrorMsg(c, "拉起支付失败")
			return
		}
		common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "pay_url": pageResp.PayURL, "amount_yuan": createReq.TotalAmount.StringFixed(2), "pay_mode": mode})
		return
	}
	qrResp, err := client.CreateQROrder(c.Request.Context(), createReq)
	if err != nil {
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "qr_code": qrResp.QRCode, "amount_yuan": createReq.TotalAmount.StringFixed(2), "pay_mode": mode})
}

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
	if result.AppID != "" && result.AppID != setting.AlipayAppID {
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
