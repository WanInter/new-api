package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTopupModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	DB = db
	LOG_DB = db

	if err = db.AutoMigrate(&User{}, &TopUp{}, &Log{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedTopupModelUser(t *testing.T, db *gorm.DB, id int, quota int) *User {
	t.Helper()

	user := &User{
		Id:       id,
		Username: fmt.Sprintf("user-%d", id),
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Email:    fmt.Sprintf("user-%d@example.com", id),
		Group:    "default",
		Quota:    quota,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedTopupModelOrder(t *testing.T, db *gorm.DB, topUp *TopUp) {
	t.Helper()

	if err := db.Create(topUp).Error; err != nil {
		t.Fatalf("failed to seed topup: %v", err)
	}
}

func assertTopupModelUserQuota(t *testing.T, db *gorm.DB, userID int, expected int) {
	t.Helper()

	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		t.Fatalf("failed to query user: %v", err)
	}
	if user.Quota != expected {
		t.Fatalf("expected user quota %d, got %d", expected, user.Quota)
	}
}

func assertTopupModelStatus(t *testing.T, db *gorm.DB, tradeNo string, expected string) {
	t.Helper()

	var topUp TopUp
	if err := db.First(&topUp, "trade_no = ?", tradeNo).Error; err != nil {
		t.Fatalf("failed to query topup: %v", err)
	}
	if topUp.Status != expected {
		t.Fatalf("expected topup status %q, got %q", expected, topUp.Status)
	}
}

func TestRechargeWeChatPayMarksSuccessAndIncreasesQuota(t *testing.T) {
	db := setupTopupModelTestDB(t)
	user := seedTopupModelUser(t, db, 1, 0)
	seedTopupModelOrder(t, db, &TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           72,
		TradeNo:         "WXPAY-ORDER-1",
		PaymentMethod:   "wechat_pay",
		PaymentProvider: PaymentProviderWeChatPay,
		Status:          common.TopUpStatusPending,
	})

	if err := RechargeWeChatPay("WXPAY-ORDER-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	assertTopupModelUserQuota(t, db, user.Id, int(10*common.QuotaPerUnit))
	assertTopupModelStatus(t, db, "WXPAY-ORDER-1", common.TopUpStatusSuccess)
}
