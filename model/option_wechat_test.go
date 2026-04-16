package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

type wechatOptionSettingSnapshot struct {
	Enabled          bool
	MchID            string
	AppID            string
	APIv3Key         string
	PrivateKey       string
	MerchantSerialNo string
	PublicKeyID      string
	PublicKey        string
	UnitPrice        float64
	MinTopUp         int
	NotifyURL        string
	OrderDescription string
	OptionMap        map[string]string
}

func cloneOptionMapForTest() map[string]string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	cloned := make(map[string]string, len(common.OptionMap))
	for key, value := range common.OptionMap {
		cloned[key] = value
	}
	return cloned
}

func snapshotWeChatOptionSettings() wechatOptionSettingSnapshot {
	return wechatOptionSettingSnapshot{
		Enabled:          setting.WeChatPayEnabled,
		MchID:            setting.WeChatPayMchID,
		AppID:            setting.WeChatPayAppID,
		APIv3Key:         setting.WeChatPayAPIv3Key,
		PrivateKey:       setting.WeChatPayPrivateKey,
		MerchantSerialNo: setting.WeChatPayMerchantSerialNo,
		PublicKeyID:      setting.WeChatPayPublicKeyID,
		PublicKey:        setting.WeChatPayPublicKey,
		UnitPrice:        setting.WeChatPayUnitPrice,
		MinTopUp:         setting.WeChatPayMinTopUp,
		NotifyURL:        setting.WeChatPayNotifyUrl,
		OrderDescription: setting.WeChatPayOrderDescription,
		OptionMap:        cloneOptionMapForTest(),
	}
}

func restoreWeChatOptionSettings(snapshot wechatOptionSettingSnapshot) {
	setting.WeChatPayEnabled = snapshot.Enabled
	setting.WeChatPayMchID = snapshot.MchID
	setting.WeChatPayAppID = snapshot.AppID
	setting.WeChatPayAPIv3Key = snapshot.APIv3Key
	setting.WeChatPayPrivateKey = snapshot.PrivateKey
	setting.WeChatPayMerchantSerialNo = snapshot.MerchantSerialNo
	setting.WeChatPayPublicKeyID = snapshot.PublicKeyID
	setting.WeChatPayPublicKey = snapshot.PublicKey
	setting.WeChatPayUnitPrice = snapshot.UnitPrice
	setting.WeChatPayMinTopUp = snapshot.MinTopUp
	setting.WeChatPayNotifyUrl = snapshot.NotifyURL
	setting.WeChatPayOrderDescription = snapshot.OrderDescription

	common.OptionMapRWMutex.Lock()
	common.OptionMap = snapshot.OptionMap
	common.OptionMapRWMutex.Unlock()
}

func setupOptionWechatTestEnv(t *testing.T) {
	t.Helper()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	if err := DB.AutoMigrate(&Option{}); err != nil {
		t.Fatalf("failed to migrate option table: %v", err)
	}
	if err := DB.Exec("DELETE FROM options").Error; err != nil {
		t.Fatalf("failed to clean options table: %v", err)
	}

	snapshot := snapshotWeChatOptionSettings()
	t.Cleanup(func() {
		restoreWeChatOptionSettings(snapshot)
		_ = DB.Exec("DELETE FROM options").Error
	})
}

func TestInitOptionMapAndUpdateOptionSyncWeChatPayConfig(t *testing.T) {
	setupOptionWechatTestEnv(t)

	setting.WeChatPayEnabled = true
	setting.WeChatPayMchID = "init-mch-id"
	setting.WeChatPayAppID = "init-app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "private-key"
	setting.WeChatPayMerchantSerialNo = "serial-no"
	setting.WeChatPayPublicKeyID = "pub-key-id"
	setting.WeChatPayPublicKey = "public-key"
	setting.WeChatPayUnitPrice = 3.5
	setting.WeChatPayMinTopUp = 5
	setting.WeChatPayNotifyUrl = "https://notify.example.com"
	setting.WeChatPayOrderDescription = "充值测试"

	InitOptionMap()

	if common.OptionMap["WeChatPayEnabled"] != "true" {
		t.Fatalf("expected WeChatPayEnabled=true, got %q", common.OptionMap["WeChatPayEnabled"])
	}
	if common.OptionMap["WeChatPayMinTopUp"] != "5" {
		t.Fatalf("expected WeChatPayMinTopUp=5, got %q", common.OptionMap["WeChatPayMinTopUp"])
	}
	if common.OptionMap["WeChatPayOrderDescription"] != "充值测试" {
		t.Fatalf("expected WeChatPayOrderDescription=充值测试, got %q", common.OptionMap["WeChatPayOrderDescription"])
	}

	if err := UpdateOption("WeChatPayEnabled", "false"); err != nil {
		t.Fatalf("failed to update WeChatPayEnabled: %v", err)
	}
	if err := UpdateOption("WeChatPayMinTopUp", "7"); err != nil {
		t.Fatalf("failed to update WeChatPayMinTopUp: %v", err)
	}
	if err := UpdateOption("WeChatPayNotifyUrl", "https://new-notify.example.com"); err != nil {
		t.Fatalf("failed to update WeChatPayNotifyUrl: %v", err)
	}

	if setting.WeChatPayEnabled {
		t.Fatal("expected WeChatPayEnabled to be false after UpdateOption")
	}
	if setting.WeChatPayMinTopUp != 7 {
		t.Fatalf("expected WeChatPayMinTopUp=7, got %d", setting.WeChatPayMinTopUp)
	}
	if setting.WeChatPayNotifyUrl != "https://new-notify.example.com" {
		t.Fatalf("expected WeChatPayNotifyUrl to be updated, got %q", setting.WeChatPayNotifyUrl)
	}
}
