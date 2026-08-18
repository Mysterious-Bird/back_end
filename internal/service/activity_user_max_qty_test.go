package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupActivityUserMaxQtyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Activity{}, &model.Order{}, &model.OrderItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestComputeActivityRemaining_UserMaxQtyAcrossProducts(t *testing.T) {
	db := setupActivityUserMaxQtyTestDB(t)
	now := time.Now()
	act := model.Activity{
		Name: "电影票活动", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
		Status: model.ActivityStatusOn, UserMaxQty: 1,
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	aid := act.ID
	apA := &model.ActivityProduct{ActivityID: aid, ProductID: 1}
	apB := &model.ActivityProduct{ActivityID: aid, ProductID: 2}

	out, err := computeActivityRemaining(db, apA, 99, nil, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.RemainingQty != 1 {
		t.Fatalf("guest remain=%d want 1", out.RemainingQty)
	}

	accountID := uint64(7)
	actID := aid
	apAID := uint64(11)
	order := model.Order{
		OrderNo: "T1", AccountID: accountID, MerchantID: 1,
		Status: model.OrderStatusPendingGroup, DeliveryType: 1,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	item := model.OrderItem{
		OrderID: order.ID, ProductID: 1, ActivityID: &actID, ActivityProductID: &apAID,
		ProductName: "1元拼团", Quantity: 1, UnitPrice: 1, Subtotal: 1,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	out, err = computeActivityRemaining(db, apB, 99, &accountID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.LimitReached || out.RemainingQty != 0 || out.LimitReason != "activity_user_max" {
		t.Fatalf("after group join: remain=%d reached=%v reason=%s", out.RemainingQty, out.LimitReached, out.LimitReason)
	}

	if err := db.Model(&order).Update("status", model.OrderStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	out, err = computeActivityRemaining(db, apB, 99, &accountID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.LimitReached || out.RemainingQty != 1 {
		t.Fatalf("after cancel: remain=%d reached=%v", out.RemainingQty, out.LimitReached)
	}
}
