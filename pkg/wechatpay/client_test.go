package wechatpay

import (
	"context"
	"testing"
)

func TestValidateConfigRequiresAllFields(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for empty config")
	}
}

func TestNotifyResultValidateBusinessFieldsRejectsNonCNY(t *testing.T) {
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

func TestSDKClientCreateNativeOrderRequiresInitializedClient(t *testing.T) {
	client := &sdkClient{}

	resp, err := client.CreateNativeOrder(context.Background(), NativeOrderRequest{})
	if err == nil {
		t.Fatal("expected initialization error")
	}
	if err.Error() != "wechat pay client is not initialized" {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
}

func TestSDKClientResolveNativeOrderDefaultsUsesConfigValues(t *testing.T) {
	client := &sdkClient{
		cfg: Config{
			DefaultNotifyURL:   "https://example.com/api/wechat/notify",
			DefaultDescription: "账户充值",
		},
	}

	resolved := client.resolveNativeOrderRequest(NativeOrderRequest{
		OutTradeNo: "trade-no",
		AmountFen:  100,
	})

	if resolved.Description != "账户充值" {
		t.Fatalf("expected default description, got %q", resolved.Description)
	}
	if resolved.NotifyURL != "https://example.com/api/wechat/notify" {
		t.Fatalf("expected default notify url, got %q", resolved.NotifyURL)
	}
}

func TestSDKClientResolveNativeOrderDefaultsPrefersRequestValues(t *testing.T) {
	client := &sdkClient{
		cfg: Config{
			DefaultNotifyURL:   "https://example.com/api/wechat/notify",
			DefaultDescription: "账户充值",
		},
	}

	resolved := client.resolveNativeOrderRequest(NativeOrderRequest{
		OutTradeNo:  "trade-no",
		AmountFen:   100,
		Description: "自定义描述",
		NotifyURL:   "https://override.example.com/notify",
	})

	if resolved.Description != "自定义描述" {
		t.Fatalf("expected request description, got %q", resolved.Description)
	}
	if resolved.NotifyURL != "https://override.example.com/notify" {
		t.Fatalf("expected request notify url, got %q", resolved.NotifyURL)
	}
}

func TestSDKClientVerifyAndDecryptNotifyRejectsSerialMismatch(t *testing.T) {
	client := &sdkClient{
		cfg: Config{
			PublicKeyID: "wechat-public-key-id",
		},
	}

	resp, err := client.VerifyAndDecryptNotify(context.Background(), map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected serial mismatch error")
	}
	if err.Error() != "wechatpay serial mismatch" {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
}
