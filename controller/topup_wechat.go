package controller

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wechatpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

const (
	wechatPayMethod         = "wechat_pay"
	wechatPayOrderMinAmount = 1
)

type WeChatPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

var newWeChatPayClient = func() (wechatpay.Client, error) {
	cfg := wechatpay.Config{
		MchID:            setting.WeChatPayMchID,
		AppID:            setting.WeChatPayAppID,
		APIv3Key:         setting.WeChatPayAPIv3Key,
		PrivateKeyPEM:    setting.WeChatPayPrivateKey,
		MerchantSerialNo: setting.WeChatPayMerchantSerialNo,
		PublicKeyID:      setting.WeChatPayPublicKeyID,
		PublicKeyPEM:     setting.WeChatPayPublicKey,
		DefaultNotifyURL: func() string {
			if setting.WeChatPayNotifyUrl != "" {
				return setting.WeChatPayNotifyUrl
			}
			return service.GetCallbackAddress() + "/api/wechat/notify"
		}(),
		DefaultDescription: func() string {
			if setting.WeChatPayOrderDescription != "" {
				return setting.WeChatPayOrderDescription
			}
			return "账户充值"
		}(),
	}
	return wechatpay.NewClient(cfg)
}

func getWeChatPayUserGroup(userID int) (string, error) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		return "", err
	}
	return user.Group, nil
}

func getWeChatPayMoney(amount int64, group string) (decimal.Decimal, int64, int64) {
	originalAmount := amount
	normalized := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		normalized = normalized.Div(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok && ds > 0 {
		discount = ds
	}
	yuan := normalized.
		Mul(decimal.NewFromFloat(setting.WeChatPayUnitPrice)).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromFloat(discount))
	fen := yuan.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	return yuan, fen, normalized.IntPart()
}

func getWeChatPayMinTopup() int64 {
	minTopup := decimal.NewFromInt(int64(setting.WeChatPayMinTopUp))
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	return minTopup.IntPart()
}

func RequestWeChatAmount(c *gin.Context) {
	var req WeChatPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if !isWeChatPayConfigured() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "管理员未开启微信支付"})
		return
	}
	minTopup := getWeChatPayMinTopup()
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopup)})
		return
	}

	id := c.GetInt("id")
	group, err := getWeChatPayUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	yuan, fen, _ := getWeChatPayMoney(req.Amount, group)
	if fen < wechatPayOrderMinAmount {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	common.ApiSuccess(c, yuan.String())
}

func RequestWeChatPay(c *gin.Context) {
	var req WeChatPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.PaymentMethod != wechatPayMethod {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	if !isWeChatPayConfigured() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "管理员未开启微信支付"})
		return
	}
	minTopup := getWeChatPayMinTopup()
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopup)})
		return
	}

	id := c.GetInt("id")
	group, err := getWeChatPayUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	yuan, fen, normalizedAmount := getWeChatPayMoney(req.Amount, group)
	if fen < wechatPayOrderMinAmount {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	client, err := newWeChatPayClient()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付配置错误"})
		return
	}

	tradeNo := fmt.Sprintf("WXPAY-%d-%d-%s", id, time.Now().UnixMilli(), randstr.String(6))
	topUp := &model.TopUp{
		UserId:        id,
		Amount:        normalizedAmount,
		Money:         yuan.InexactFloat64(),
		TradeNo:       tradeNo,
		PaymentMethod: wechatPayMethod,
		CreateTime:    time.Now().Unix(),
		Status:        common.TopUpStatusPending,
	}
	if err = topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	resp, err := client.CreateNativeOrder(c.Request.Context(), wechatpay.NativeOrderRequest{
		OutTradeNo: tradeNo,
		AmountFen:  fen,
		ClientIP:   c.ClientIP(),
	})
	if err != nil {
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	common.ApiSuccess(c, gin.H{
		"code_url": resp.CodeURL,
		"trade_no": tradeNo,
	})
}

func WeChatPayNotify(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	client, err := newWeChatPayClient()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	headers := map[string]string{
		"Wechatpay-Signature":      c.GetHeader("Wechatpay-Signature"),
		"Wechatpay-Nonce":          c.GetHeader("Wechatpay-Nonce"),
		"Wechatpay-Timestamp":      c.GetHeader("Wechatpay-Timestamp"),
		"Wechatpay-Serial":         c.GetHeader("Wechatpay-Serial"),
		"Wechatpay-Signature-Type": c.GetHeader("Wechatpay-Signature-Type"),
	}
	result, err := client.VerifyAndDecryptNotify(c.Request.Context(), headers, body)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if err = result.ValidateBusinessFields(setting.WeChatPayAppID, setting.WeChatPayMchID); err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	topUp := model.GetTopUpByTradeNo(result.OutTradeNo)
	if topUp == nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	expectedFen := decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if expectedFen != result.AmountTotal {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	LockOrder(result.OutTradeNo)
	defer UnlockOrder(result.OutTradeNo)

	if err = model.RechargeWeChatPay(result.OutTradeNo); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.AbortWithStatus(http.StatusNoContent)
}
