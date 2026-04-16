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

func TestSDKClientVerifyAndDecryptNotifyReturnsNotImplemented(t *testing.T) {
	client := &sdkClient{}

	resp, err := client.VerifyAndDecryptNotify(context.Background(), map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected not implemented error")
	}
	if err.Error() != "not implemented" {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
}
