package setting

var (
	WeChatPayEnabled          bool
	WeChatPayMchID            string
	WeChatPayAppID            string
	WeChatPayAPIv3Key         string
	WeChatPayPrivateKey       string
	WeChatPayMerchantSerialNo string
	WeChatPayPublicKeyID      string
	WeChatPayPublicKey        string
	WeChatPayUnitPrice        float64 = 1.0
	WeChatPayMinTopUp         int     = 1
	WeChatPayNotifyUrl        string
	WeChatPayOrderDescription string = "账户充值"
)
