package wechatpay

import "testing"

func TestValidateConfigRequiresAllFields(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for empty config")
	}
}

func TestBuildNotifyResultRejectsNonCNY(t *testing.T) {
	result := &NotifyResult{Currency: "USD"}
	if err := result.ValidateBusinessFields("wx-app", "mch-id"); err == nil {
		t.Fatal("expected currency validation error")
	}
}

func TestNewClientReturnsConfigErrorFirst(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected config validation error")
	}
}
