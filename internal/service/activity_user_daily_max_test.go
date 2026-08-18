package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestComputeActivityRemaining_UserDailyMaxAcrossProducts(t *testing.T) {
	db := setupActivityUserMaxQtyTestDB(t)
	loc := time.Local
	now := time.Date(2026, 8, 19, 13, 0, 0, 0, loc)
	act := model.Activity{
		Name: "日限活动", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
		Status: model.ActivityStatusOn, UserDailyMax: 1, UserDailyRefreshTime: "00:00:00",
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	apA := &model.ActivityProduct{ActivityID: act.ID, ProductID: 1}
	apB := &model.ActivityProduct{ActivityID: act.ID, ProductID: 2}

	out, err := computeActivityRemaining(db, apA, 99, nil, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.RemainingQty != 1 {
		t.Fatalf("guest remain=%d want 1", out.RemainingQty)
	}

	accountID := uint64(8)
	actID := act.ID
	apAID := uint64(21)
	order := model.Order{
		OrderNo: "D1", AccountID: accountID, MerchantID: 1,
		Status: model.OrderStatusPendingGroup, DeliveryType: 1,
		CreatedAt: now.Add(-time.Hour),
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	item := model.OrderItem{
		OrderID: order.ID, ProductID: 1, ActivityID: &actID, ActivityProductID: &apAID,
		ProductName: "直购", Quantity: 1, UnitPrice: 9.9, Subtotal: 9.9,
		CreatedAt: now.Add(-time.Hour),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	out, err = computeActivityRemaining(db, apB, 99, &accountID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.LimitReached || out.RemainingQty != 0 || out.LimitReason != "activity_user_daily" {
		t.Fatalf("after buy: remain=%d reached=%v reason=%s", out.RemainingQty, out.LimitReached, out.LimitReason)
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

func TestComputeActivityRemaining_UserDailyMaxUsesOwnRefresh(t *testing.T) {
	db := setupActivityUserMaxQtyTestDB(t)
	loc := time.Local
	now := time.Date(2026, 8, 19, 13, 0, 0, 0, loc)
	act := model.Activity{
		Name: "午间刷新", StartAt: now.Add(-24 * time.Hour), EndAt: now.Add(24 * time.Hour),
		Status: model.ActivityStatusOn, UserDailyMax: 1, UserDailyRefreshTime: "12:00:00",
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	ap := &model.ActivityProduct{ActivityID: act.ID, ProductID: 1, DailyRefreshTime: "00:00:00"}
	accountID := uint64(9)
	actID := act.ID
	apID := uint64(22)

	oldOrder := model.Order{
		OrderNo: "D-OLD", AccountID: accountID, MerchantID: 1,
		Status: model.OrderStatusCompleted, DeliveryType: 1,
		CreatedAt: time.Date(2026, 8, 19, 11, 0, 0, 0, loc),
	}
	if err := db.Create(&oldOrder).Error; err != nil {
		t.Fatal(err)
	}
	oldItem := model.OrderItem{
		OrderID: oldOrder.ID, ProductID: 1, ActivityID: &actID, ActivityProductID: &apID,
		ProductName: "旧窗", Quantity: 1, UnitPrice: 1, Subtotal: 1,
		CreatedAt: oldOrder.CreatedAt,
	}
	if err := db.Create(&oldItem).Error; err != nil {
		t.Fatal(err)
	}

	out, err := computeActivityRemaining(db, ap, 99, &accountID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.LimitReached || out.RemainingQty != 1 {
		t.Fatalf("order before 12:00 should not occupy new window: remain=%d reached=%v", out.RemainingQty, out.LimitReached)
	}

	newOrder := model.Order{
		OrderNo: "D-NEW", AccountID: accountID, MerchantID: 1,
		Status: model.OrderStatusCompleted, DeliveryType: 1,
		CreatedAt: time.Date(2026, 8, 19, 12, 30, 0, 0, loc),
	}
	if err := db.Create(&newOrder).Error; err != nil {
		t.Fatal(err)
	}
	newItem := model.OrderItem{
		OrderID: newOrder.ID, ProductID: 1, ActivityID: &actID, ActivityProductID: &apID,
		ProductName: "新窗", Quantity: 1, UnitPrice: 1, Subtotal: 1,
		CreatedAt: newOrder.CreatedAt,
	}
	if err := db.Create(&newItem).Error; err != nil {
		t.Fatal(err)
	}

	out, err = computeActivityRemaining(db, ap, 99, &accountID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.LimitReached || out.RemainingQty != 0 || out.LimitReason != "activity_user_daily" {
		t.Fatalf("order after 12:00 should occupy: remain=%d reached=%v reason=%s", out.RemainingQty, out.LimitReached, out.LimitReason)
	}
}
