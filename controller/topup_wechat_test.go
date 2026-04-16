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

type topupWechatTestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		EnableWeChatTopup bool             `json:"enable_wechat_topup"`
		WeChatMinTopUp    int              `json:"wechat_min_topup"`
		PayMethods        []map[string]any `json:"pay_methods"`
	} `json:"data"`
}

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

	var response topupWechatTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	if !response.Data.EnableWeChatTopup {
		t.Fatalf("expected enable_wechat_topup=true, got false, body: %s", recorder.Body.String())
	}
	if response.Data.WeChatMinTopUp != 2 {
		t.Fatalf("expected wechat_min_topup=2, got %d", response.Data.WeChatMinTopUp)
	}

	hasWeChatPay := false
	for _, method := range response.Data.PayMethods {
		methodType, ok := method["type"].(string)
		if ok && methodType == "wechat_pay" {
			hasWeChatPay = true
			break
		}
	}
	if !hasWeChatPay {
		t.Fatalf("expected wechat_pay in pay_methods, body: %s", recorder.Body.String())
	}
}

func TestGetTopUpInfoDoesNotExposeWeChatPayWhenConfigIncomplete(t *testing.T) {
	setupTopupControllerTestEnv(t)
	setting.WeChatPayEnabled = true
	setting.WeChatPayMchID = "mch-id"
	setting.WeChatPayAppID = "app-id"
	setting.WeChatPayAPIv3Key = "01234567890123456789012345678901"
	setting.WeChatPayPrivateKey = "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----"
	setting.WeChatPayMerchantSerialNo = "serial-no"
	setting.WeChatPayPublicKeyID = "pub-id"
	// 故意留空 PublicKey，配置不完整
	setting.WeChatPayPublicKey = ""
	setting.WeChatPayMinTopUp = 2

	ctx, recorder := newTopupTestContext(t, "GET", "/api/user/topup/info", nil, 1)
	GetTopUpInfo(ctx)

	var response topupWechatTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got body: %s", recorder.Body.String())
	}
	if response.Data.EnableWeChatTopup {
		t.Fatalf("expected enable_wechat_topup=false when config incomplete, body: %s", recorder.Body.String())
	}
	for _, method := range response.Data.PayMethods {
		methodType, ok := method["type"].(string)
		if ok && methodType == "wechat_pay" {
			t.Fatalf("did not expect wechat_pay in pay_methods when config incomplete, body: %s", recorder.Body.String())
		}
	}
}
