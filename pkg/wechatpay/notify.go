package wechatpay

import "fmt"

func (c Config) Validate() error {
	if c.MchID == "" || c.AppID == "" || c.APIv3Key == "" || c.PrivateKeyPEM == "" ||
		c.MerchantSerialNo == "" || c.PublicKeyID == "" || c.PublicKeyPEM == "" {
		return fmt.Errorf("wechat pay config is incomplete")
	}
	return nil
}

func (r *NotifyResult) ValidateBusinessFields(appID, mchID string) error {
	if r == nil {
		return fmt.Errorf("notify result is nil")
	}
	if r.Currency != "CNY" {
		return fmt.Errorf("unexpected currency: %s", r.Currency)
	}
	if r.AppID != appID || r.MchID != mchID {
		return fmt.Errorf("appid or mchid mismatch")
	}
	return nil
}
