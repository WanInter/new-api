package setting

var (
	AlipayEnabled               bool
	AlipaySandbox               bool
	AlipayAppID                 string
	AlipayPrivateKey            string
	AlipayPublicKey             string
	AlipayUnitPrice             float64 = 1.0
	AlipayMinTopUp              int     = 1
	AlipayPayMode               string  = "page"
	AlipayNotifyURL             string
	AlipayReturnURL             string
	AlipaySubscriptionReturnURL string
	AlipayOrderDescription      string = "账户充值"
)
