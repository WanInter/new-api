package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

type paymentEnvSettingSnapshot struct {
	AlipayAppID                 string
	AlipayPrivateKey            string
	AlipayPublicKey             string
	WeChatPayMchID              string
	WeChatPayAppID              string
	WeChatPayAPIv3Key           string
	WeChatPayPrivateKey         string
	WeChatPayMerchantSerialNo   string
	WeChatPayPublicKeyID        string
	WeChatPayPublicKey          string
	OptionMap                   map[string]string
}

func snapshotPaymentEnvSettings() paymentEnvSettingSnapshot {
	return paymentEnvSettingSnapshot{
		AlipayAppID:               setting.AlipayAppID,
		AlipayPrivateKey:          setting.AlipayPrivateKey,
		AlipayPublicKey:           setting.AlipayPublicKey,
		WeChatPayMchID:            setting.WeChatPayMchID,
		WeChatPayAppID:            setting.WeChatPayAppID,
		WeChatPayAPIv3Key:         setting.WeChatPayAPIv3Key,
		WeChatPayPrivateKey:       setting.WeChatPayPrivateKey,
		WeChatPayMerchantSerialNo: setting.WeChatPayMerchantSerialNo,
		WeChatPayPublicKeyID:      setting.WeChatPayPublicKeyID,
		WeChatPayPublicKey:        setting.WeChatPayPublicKey,
		OptionMap:                 cloneOptionMapForTest(),
	}
}

func restorePaymentEnvSettings(snapshot paymentEnvSettingSnapshot) {
	setting.AlipayAppID = snapshot.AlipayAppID
	setting.AlipayPrivateKey = snapshot.AlipayPrivateKey
	setting.AlipayPublicKey = snapshot.AlipayPublicKey
	setting.WeChatPayMchID = snapshot.WeChatPayMchID
	setting.WeChatPayAppID = snapshot.WeChatPayAppID
	setting.WeChatPayAPIv3Key = snapshot.WeChatPayAPIv3Key
	setting.WeChatPayPrivateKey = snapshot.WeChatPayPrivateKey
	setting.WeChatPayMerchantSerialNo = snapshot.WeChatPayMerchantSerialNo
	setting.WeChatPayPublicKeyID = snapshot.WeChatPayPublicKeyID
	setting.WeChatPayPublicKey = snapshot.WeChatPayPublicKey

	common.OptionMapRWMutex.Lock()
	common.OptionMap = snapshot.OptionMap
	common.OptionMapRWMutex.Unlock()
}

func setOrClearEnv(t *testing.T, key string, value string) {
	t.Helper()
	if value == "" {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset env %s: %v", key, err)
		}
		return
	}
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("failed to set env %s: %v", key, err)
	}
}

func setupPaymentEnvOptionTest(t *testing.T) {
	t.Helper()
	setupOptionWechatTestEnv(t)

	snapshot := snapshotPaymentEnvSettings()
	envKeys := []string{
		"ALIPAY_APP_ID",
		"ALIPAY_PRIVATE_KEY",
		"ALIPAY_PUBLIC_KEY",
		"WECHAT_PAY_MCH_ID",
		"WECHAT_PAY_APP_ID",
		"WECHAT_PAY_API_V3_KEY",
		"WECHAT_PAY_PRIVATE_KEY",
		"WECHAT_PAY_MERCHANT_SERIAL_NO",
		"WECHAT_PAY_PUBLIC_KEY_ID",
		"WECHAT_PAY_PUBLIC_KEY",
	}
	originalEnv := make(map[string]string, len(envKeys))
	for _, key := range envKeys {
		originalEnv[key] = os.Getenv(key)
	}

	t.Cleanup(func() {
		for _, key := range envKeys {
			setOrClearEnv(t, key, originalEnv[key])
		}
		restorePaymentEnvSettings(snapshot)
		_ = DB.Exec("DELETE FROM options").Error
	})
}

func TestInitOptionMapUsesPaymentEnvDefaults(t *testing.T) {
	setupPaymentEnvOptionTest(t)

	setting.AlipayAppID = ""
	setting.AlipayPrivateKey = ""
	setting.AlipayPublicKey = ""
	setting.WeChatPayMchID = ""
	setting.WeChatPayAppID = ""
	setting.WeChatPayAPIv3Key = ""
	setting.WeChatPayPrivateKey = ""
	setting.WeChatPayMerchantSerialNo = ""
	setting.WeChatPayPublicKeyID = ""
	setting.WeChatPayPublicKey = ""

	setOrClearEnv(t, "ALIPAY_APP_ID", "env-alipay-app-id")
	setOrClearEnv(t, "ALIPAY_PRIVATE_KEY", "env-alipay-private-key")
	setOrClearEnv(t, "ALIPAY_PUBLIC_KEY", "env-alipay-public-key")
	setOrClearEnv(t, "WECHAT_PAY_MCH_ID", "env-wechat-mch-id")
	setOrClearEnv(t, "WECHAT_PAY_APP_ID", "env-wechat-app-id")
	setOrClearEnv(t, "WECHAT_PAY_API_V3_KEY", "env-wechat-api-v3-key")
	setOrClearEnv(t, "WECHAT_PAY_PRIVATE_KEY", "env-wechat-private-key")
	setOrClearEnv(t, "WECHAT_PAY_MERCHANT_SERIAL_NO", "env-wechat-serial-no")
	setOrClearEnv(t, "WECHAT_PAY_PUBLIC_KEY_ID", "env-wechat-public-key-id")
	setOrClearEnv(t, "WECHAT_PAY_PUBLIC_KEY", "env-wechat-public-key")

	setting.InitPaymentSensitiveFromEnv()
	InitOptionMap()

	if setting.AlipayAppID != "env-alipay-app-id" {
		t.Fatalf("expected AlipayAppID from env, got %q", setting.AlipayAppID)
	}
	if setting.WeChatPayAPIv3Key != "env-wechat-api-v3-key" {
		t.Fatalf("expected WeChatPayAPIv3Key from env, got %q", setting.WeChatPayAPIv3Key)
	}
	if common.OptionMap["AlipayPrivateKey"] != "env-alipay-private-key" {
		t.Fatalf("expected AlipayPrivateKey in option map from env, got %q", common.OptionMap["AlipayPrivateKey"])
	}
	if common.OptionMap["WeChatPayPublicKey"] != "env-wechat-public-key" {
		t.Fatalf("expected WeChatPayPublicKey in option map from env, got %q", common.OptionMap["WeChatPayPublicKey"])
	}
}

func TestLoadOptionsFromDatabaseDoesNotOverridePaymentEnvDefaultsWithEmptyValue(t *testing.T) {
	setupPaymentEnvOptionTest(t)

	setOrClearEnv(t, "ALIPAY_PRIVATE_KEY", "env-alipay-private-key")
	setOrClearEnv(t, "WECHAT_PAY_API_V3_KEY", "env-wechat-api-v3-key")
	setting.InitPaymentSensitiveFromEnv()
	InitOptionMap()

	if err := DB.Create(&Option{Key: "AlipayPrivateKey", Value: ""}).Error; err != nil {
		t.Fatalf("failed to seed alipay option: %v", err)
	}
	if err := DB.Create(&Option{Key: "WeChatPayAPIv3Key", Value: ""}).Error; err != nil {
		t.Fatalf("failed to seed wechat option: %v", err)
	}

	loadOptionsFromDatabase()

	if setting.AlipayPrivateKey != "env-alipay-private-key" {
		t.Fatalf("expected empty DB value not to override AlipayPrivateKey env default, got %q", setting.AlipayPrivateKey)
	}
	if setting.WeChatPayAPIv3Key != "env-wechat-api-v3-key" {
		t.Fatalf("expected empty DB value not to override WeChatPayAPIv3Key env default, got %q", setting.WeChatPayAPIv3Key)
	}
}

func TestLoadOptionsFromDatabaseOverridesPaymentEnvDefaultsWithNonEmptyValue(t *testing.T) {
	setupPaymentEnvOptionTest(t)

	setOrClearEnv(t, "ALIPAY_PUBLIC_KEY", "env-alipay-public-key")
	setOrClearEnv(t, "WECHAT_PAY_PRIVATE_KEY", "env-wechat-private-key")
	setting.InitPaymentSensitiveFromEnv()
	InitOptionMap()

	if err := DB.Create(&Option{Key: "AlipayPublicKey", Value: "db-alipay-public-key"}).Error; err != nil {
		t.Fatalf("failed to seed alipay option: %v", err)
	}
	if err := DB.Create(&Option{Key: "WeChatPayPrivateKey", Value: "db-wechat-private-key"}).Error; err != nil {
		t.Fatalf("failed to seed wechat option: %v", err)
	}

	loadOptionsFromDatabase()

	if setting.AlipayPublicKey != "db-alipay-public-key" {
		t.Fatalf("expected DB value to override AlipayPublicKey env default, got %q", setting.AlipayPublicKey)
	}
	if setting.WeChatPayPrivateKey != "db-wechat-private-key" {
		t.Fatalf("expected DB value to override WeChatPayPrivateKey env default, got %q", setting.WeChatPayPrivateKey)
	}
}
