package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestResolveSaleChannelEx_Bargain(t *testing.T) {
	ch, chText := ResolveSaleChannelEx(model.PurchaseTypeSolo, ptrU64(12), true)
	if ch != "bargain" || chText != "砍价单" {
		t.Fatalf("bargain got channel=%s text=%s want bargain/砍价单", ch, chText)
	}
	// 非砍价：仍走原逻辑
	ch, _ = ResolveSaleChannelEx(model.PurchaseTypeSolo, ptrU64(12), false)
	if ch != "activity_deal" {
		t.Fatalf("non-bargain activity got %s want activity_deal", ch)
	}
}

func TestLoadOrderItemSaleMeta_BargainMarked(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, p := seedBargainAP(t, db)
	order := model.Order{
		OrderNo: "BARGAIN001", AccountID: 100, MerchantID: 1,
		Status: model.OrderStatusPendingFulfill, PayStatus: model.PayStatusPaid,
		TotalAmount: 0, PayAmount: 0,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	// 0 元成交价
	if err := db.Create(&model.OrderItem{
		OrderID: order.ID, ProductID: p.ID, ProductName: p.Name,
		UnitPrice: 0, Quantity: 1, Subtotal: 0, PurchaseType: model.PurchaseTypeSolo,
	}).Error; err != nil {
		t.Fatalf("item: %v", err)
	}
	sess := model.BargainSession{
		ActivityProductID: ap.ID, InitiatorAccountID: 100,
		CurrentPrice: 0, FloorPrice: 0, Status: model.BargainStatusOrdered,
		OrderID: &order.ID, ExpireAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("session: %v", err)
	}
	usage := model.UserInventoryUsage{
		AccountID: 100, ProductID: p.ID, InventoryID: 1,
		SourceOrderID: &order.ID, Quantity: 1, Status: model.InventoryUsagePendingVerify,
		MerchantID: 1, UsageMerchantID: 1,
	}
	svc := &DashboardService{DB: db}
	meta := svc.loadOrderItemSaleMeta([]model.UserInventoryUsage{usage})
	key := orderProductKey(order.ID, p.ID)
	m, ok := meta[key]
	if !ok {
		t.Fatalf("meta missing for key %v", key)
	}
	if m.BargainSessionID == nil || *m.BargainSessionID != sess.ID {
		t.Fatalf("bargain_session_id want %d, got %v", sess.ID, m.BargainSessionID)
	}
	if m.UnitPrice != 0 {
		t.Fatalf("0元单 unit_price want 0, got %v", m.UnitPrice)
	}
}

func TestGetSessionByOrder(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, p := seedBargainAP(t, db)
	order := model.Order{
		OrderNo: "BARGAIN002", AccountID: 101, MerchantID: 1,
		Status: model.OrderStatusPendingFulfill, PayStatus: model.PayStatusPaid,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	sess := model.BargainSession{
		ActivityProductID: ap.ID, InitiatorAccountID: 101,
		ProductID: p.ID, MerchantID: 1,
		OriginPrice: 29.9, FloorPrice: 9.9, CurrentPrice: 12.0,
		Status: model.BargainStatusOrdered, OrderID: &order.ID,
		ExpireAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := db.Create(&model.BargainHelp{
		SessionID: sess.ID, HelperAccountID: 999, CutAmount: 3.5, IsNewUser: 1,
	}).Error; err != nil {
		t.Fatalf("help: %v", err)
	}

	svc := &BargainService{DB: db}
	view, err := svc.GetSessionByOrder(order.ID)
	if err != nil {
		t.Fatalf("GetSessionByOrder: %v", err)
	}
	if view == nil {
		t.Fatalf("expected bargain view, got nil")
	}
	if view.ID != sess.ID {
		t.Fatalf("session id want %d, got %d", sess.ID, view.ID)
	}
	if len(view.Helps) != 1 || view.Helps[0].CutAmount != 3.5 {
		t.Fatalf("helps want 1 cut=3.5, got %+v", view.Helps)
	}

	// 非砍价订单返回 nil
	plainOrder := model.Order{OrderNo: "PLAIN001", AccountID: 102, MerchantID: 1}
	if err := db.Create(&plainOrder).Error; err != nil {
		t.Fatalf("plain order: %v", err)
	}
	view, err = svc.GetSessionByOrder(plainOrder.ID)
	if err != nil || view != nil {
		t.Fatalf("plain order want nil,nil, got %v,%v", view, err)
	}
}

// TestGetSessionByOrder_ExpiredActivity 活动已结束后，订单详情仍应能回显砍价记录（历史数据）。
func TestGetSessionByOrder_ExpiredActivity(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, p := seedBargainAP(t, db)
	// 让活动过期（EndAt 早于当前），复现"已结束活动"场景
	if err := db.Model(&model.Activity{}).
		Where("id = ?", ap.ActivityID).
		Updates(map[string]interface{}{"end_at": time.Now().Add(-2 * time.Hour)}).Error; err != nil {
		t.Fatalf("expire activity: %v", err)
	}
	order := model.Order{
		OrderNo: "BARGAIN_EXP", AccountID: 103, MerchantID: 1,
		Status: model.OrderStatusPendingFulfill, PayStatus: model.PayStatusPaid,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	sess := model.BargainSession{
		ActivityProductID: ap.ID, InitiatorAccountID: 103,
		ProductID: p.ID, MerchantID: 1,
		OriginPrice: 29.9, FloorPrice: 9.9, CurrentPrice: 12.0,
		Status: model.BargainStatusOrdered, OrderID: &order.ID,
		ExpireAt: time.Now().Add(-time.Hour),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := db.Create(&model.BargainHelp{
		SessionID: sess.ID, HelperAccountID: 1000, CutAmount: 2.0, IsNewUser: 0,
	}).Error; err != nil {
		t.Fatalf("help: %v", err)
	}

	svc := &BargainService{DB: db}
	view, err := svc.GetSessionByOrder(order.ID)
	if err != nil {
		t.Fatalf("GetSessionByOrder with expired activity: %v", err)
	}
	if view == nil {
		t.Fatalf("expected bargain view for expired activity, got nil")
	}
	if view.ID != sess.ID || len(view.Helps) != 1 {
		t.Fatalf("want session %d helps=1, got id=%d helps=%d", sess.ID, view.ID, len(view.Helps))
	}
}
