package controller

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type wechatPaySettingSnapshot struct {
	Enabled          bool
	MchID            string
	AppID            string
	APIv3Key         string
	PrivateKey       string
	MerchantSerialNo string
	PublicKeyID      string
	PublicKey        string
	MinTopUp         int
	UnitPrice        float64
	NotifyURL        string
	OrderDescription string
	PayMethods       []map[string]string
}

func snapshotWeChatPaySettings() wechatPaySettingSnapshot {
	payMethods := make([]map[string]string, len(operation_setting.PayMethods))
	for i, method := range operation_setting.PayMethods {
		cloned := make(map[string]string, len(method))
		for key, value := range method {
			cloned[key] = value
		}
		payMethods[i] = cloned
	}

	return wechatPaySettingSnapshot{
		Enabled:          setting.WeChatPayEnabled,
		MchID:            setting.WeChatPayMchID,
		AppID:            setting.WeChatPayAppID,
		APIv3Key:         setting.WeChatPayAPIv3Key,
		PrivateKey:       setting.WeChatPayPrivateKey,
		MerchantSerialNo: setting.WeChatPayMerchantSerialNo,
		PublicKeyID:      setting.WeChatPayPublicKeyID,
		PublicKey:        setting.WeChatPayPublicKey,
		MinTopUp:         setting.WeChatPayMinTopUp,
		UnitPrice:        setting.WeChatPayUnitPrice,
		NotifyURL:        setting.WeChatPayNotifyUrl,
		OrderDescription: setting.WeChatPayOrderDescription,
		PayMethods:       payMethods,
	}
}

func restoreWeChatPaySettings(snapshot wechatPaySettingSnapshot) {
	setting.WeChatPayEnabled = snapshot.Enabled
	setting.WeChatPayMchID = snapshot.MchID
	setting.WeChatPayAppID = snapshot.AppID
	setting.WeChatPayAPIv3Key = snapshot.APIv3Key
	setting.WeChatPayPrivateKey = snapshot.PrivateKey
	setting.WeChatPayMerchantSerialNo = snapshot.MerchantSerialNo
	setting.WeChatPayPublicKeyID = snapshot.PublicKeyID
	setting.WeChatPayPublicKey = snapshot.PublicKey
	setting.WeChatPayMinTopUp = snapshot.MinTopUp
	setting.WeChatPayUnitPrice = snapshot.UnitPrice
	setting.WeChatPayNotifyUrl = snapshot.NotifyURL
	setting.WeChatPayOrderDescription = snapshot.OrderDescription
	operation_setting.PayMethods = snapshot.PayMethods
}

func setupTopupControllerTestEnv(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(&model.Option{}); err != nil {
		t.Fatalf("failed to migrate option table: %v", err)
	}

	snapshot := snapshotWeChatPaySettings()
	t.Cleanup(func() {
		restoreWeChatPaySettings(snapshot)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	operation_setting.PayMethods = []map[string]string{}
}

func newTopupTestContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	ctx.Set("id", userID)

	return ctx, recorder
}

func TestGetTopUpInfoIncludesWeChatPayMethodWhenConfigured(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setting.WeChatPayEnabled = true
	setting.WeChatPayMchID = "mch-id"
	setting.WeChatPayAppID = "app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----"
	setting.WeChatPayMerchantSerialNo = "serial-no"
	setting.WeChatPayPublicKeyID = "pub-id"
	setting.WeChatPayPublicKey = "-----BEGIN PUBLIC KEY-----\nkey\n-----END PUBLIC KEY-----"
	setting.WeChatPayMinTopUp = 2

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	body := recorder.Body.String()
	if !strings.Contains(body, "wechat_pay") {
		t.Fatalf("expected wechat_pay in response: %s", body)
	}
}

func TestInitOptionMapAndUpdateOptionSyncWeChatPayConfig(t *testing.T) {
	setupTopupControllerTestEnv(t)

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

	model.InitOptionMap()

	if common.OptionMap["WeChatPayEnabled"] != "true" {
		t.Fatalf("expected WeChatPayEnabled=true, got %q", common.OptionMap["WeChatPayEnabled"])
	}
	if common.OptionMap["WeChatPayMinTopUp"] != "5" {
		t.Fatalf("expected WeChatPayMinTopUp=5, got %q", common.OptionMap["WeChatPayMinTopUp"])
	}
	if common.OptionMap["WeChatPayOrderDescription"] != "充值测试" {
		t.Fatalf("expected WeChatPayOrderDescription=充值测试, got %q", common.OptionMap["WeChatPayOrderDescription"])
	}

	if err := model.UpdateOption("WeChatPayEnabled", "false"); err != nil {
		t.Fatalf("failed to update WeChatPayEnabled: %v", err)
	}
	if err := model.UpdateOption("WeChatPayMinTopUp", "7"); err != nil {
		t.Fatalf("failed to update WeChatPayMinTopUp: %v", err)
	}
	if err := model.UpdateOption("WeChatPayNotifyUrl", "https://new-notify.example.com"); err != nil {
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
