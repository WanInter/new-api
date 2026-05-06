package setting

import "os"

func InitPaymentSensitiveFromEnv() {
	if value := os.Getenv("ALIPAY_APP_ID"); value != "" {
		AlipayAppID = value
	}
	if value := os.Getenv("ALIPAY_PRIVATE_KEY"); value != "" {
		AlipayPrivateKey = value
	}
	if value := os.Getenv("ALIPAY_PUBLIC_KEY"); value != "" {
		AlipayPublicKey = value
	}

	if value := os.Getenv("WECHAT_PAY_MCH_ID"); value != "" {
		WeChatPayMchID = value
	}
	if value := os.Getenv("WECHAT_PAY_APP_ID"); value != "" {
		WeChatPayAppID = value
	}
	if value := os.Getenv("WECHAT_PAY_API_V3_KEY"); value != "" {
		WeChatPayAPIv3Key = value
	}
	if value := os.Getenv("WECHAT_PAY_PRIVATE_KEY"); value != "" {
		WeChatPayPrivateKey = value
	}
	if value := os.Getenv("WECHAT_PAY_MERCHANT_SERIAL_NO"); value != "" {
		WeChatPayMerchantSerialNo = value
	}
	if value := os.Getenv("WECHAT_PAY_PUBLIC_KEY_ID"); value != "" {
		WeChatPayPublicKeyID = value
	}
	if value := os.Getenv("WECHAT_PAY_PUBLIC_KEY"); value != "" {
		WeChatPayPublicKey = value
	}
}

func ShouldIgnoreEmptyPaymentSensitiveOption(key string, value string) bool {
	if value != "" {
		return false
	}

	switch key {
	case "AlipayAppID",
		"AlipayPrivateKey",
		"AlipayPublicKey",
		"WeChatPayMchID",
		"WeChatPayAppID",
		"WeChatPayAPIv3Key",
		"WeChatPayPrivateKey",
		"WeChatPayMerchantSerialNo",
		"WeChatPayPublicKeyID",
		"WeChatPayPublicKey":
		return true
	default:
		return false
	}
}
