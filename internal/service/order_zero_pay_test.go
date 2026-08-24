package service

import (
	"fmt"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// deferredSettleProvider 模拟微信渠道：不下单即结，禁止正价 SettlePaidInTx。
type deferredSettleProvider struct {
	inner payment.MockProvider
}

func (p *deferredSettleProvider) Name() string          { return "wechat-like" }
func (p *deferredSettleProvider) ImmediateSettle() bool { return false }
func (p *deferredSettleProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", payment.ErrNotSupported)
}
func (p *deferredSettleProvider) SettleSubjectPaidInTx(tx *gorm.DB, sub payment.PaySubject, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", payment.ErrNotSupported)
}
func (p *deferredSettleProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	return p.inner.RefundInTx(tx, orderID)
}
func (p *deferredSettleProvider) RefundAmountInTx(tx *gorm.DB, orderID uint64, amount float64, reason string) error {
	return p.inner.RefundAmountInTx(tx, orderID, amount, reason)
}
func (p *deferredSettleProvider) RefundSubjectAmountInTx(tx *gorm.DB, sub payment.PaySubject, amount float64, reason string) error {
	return p.inner.RefundSubjectAmountInTx(tx, sub, amount, reason)
}
func (p *deferredSettleProvider) CreatePrepay(orderID uint64, accountID uint64) (*payment.PrepayResult, error) {
	return &payment.PrepayResult{Provider: p.Name(), AlreadyPaid: false, NeedPay: true}, nil
}
func (p *deferredSettleProvider) CreatePrepayForSubject(sub payment.PaySubject) (*payment.PrepayResult, error) {
	return &payment.PrepayResult{Provider: p.Name(), AlreadyPaid: false, NeedPay: true}, nil
}
func (p *deferredSettleProvider) HandleNotify(headers map[string]string, body []byte) (*payment.NotifyResult, error) {
	return nil, payment.ErrNotConfigured
}

func setupZeroPayOrderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Account{},
		&model.MerchantProfile{},
		&model.ProductCategory{},
		&model.Product{},
		&model.Activity{},
		&model.ActivityProduct{},
		&model.BargainSession{},
		&model.Order{},
		&model.OrderItem{},
		&model.UserInventory{},
		&model.UserInventoryLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestOrderCreate_ZeroBargainPriceSettlesWithoutWallet(t *testing.T) {
	db := setupZeroPayOrderDB(t)
	phone := "13900000001"
	if err := db.Create(&model.Account{ID: 100, Phone: &phone}).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	seedOpenMerchant(db, 1, "店A")
	_ = db.Model(&model.MerchantProfile{}).Where("id = 1").Update("auto_approve", 1)
	p := seedOnShelfProduct(db, 1, "砍价品")
	now := time.Now()
	act := model.Activity{
		MerchantID: 1, Name: "砍价活动", Status: model.ActivityStatusOn,
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatalf("activity: %v", err)
	}
	floor := 0.0
	ap := model.ActivityProduct{
		ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 10, ActivityStock: 20,
		Status: 1, EnableBargain: 1, BargainFloorPrice: &floor,
	}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatalf("ap: %v", err)
	}
	sess := model.BargainSession{
		ActivityProductID:  ap.ID,
		InitiatorAccountID: 100,
		FloorPrice:         0,
		CurrentPrice:       0,
		Status:             model.BargainStatusOngoing,
		ExpireAt:           now.Add(time.Hour),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("session: %v", err)
	}

	pay := &deferredSettleProvider{inner: payment.MockProvider{DB: db}}
	orderSvc := &OrderService{
		DB:          db,
		ActivitySvc: &ActivityService{DB: db},
		Payment:     pay,
		InventorySvc: &InventoryService{DB: db},
	}

	view, err := orderSvc.Create(100, CreateOrderInput{
		ActivityProductID: &ap.ID,
		MerchantID:        1,
		Quantity:          1,
		DeliveryType:      model.DeliveryTypePickup,
		BargainSessionID:  &sess.ID,
	})
	if err != nil {
		t.Fatalf("create zero bargain order: %v", err)
	}
	if view.PayAmount > 0.009 {
		t.Fatalf("pay_amount want 0, got %v", view.PayAmount)
	}
	var order model.Order
	if err := db.First(&order, view.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if order.PayStatus != model.PayStatusPaid {
		t.Fatalf("pay_status want paid, got %d", order.PayStatus)
	}
	if order.Status != model.OrderStatusPendingFulfill && order.MerchantReviewStage != model.MerchantReviewApproved {
		// auto_approve 应入履约/已审
		t.Fatalf("status=%d review=%d want fulfill/approved", order.Status, order.MerchantReviewStage)
	}

	var sessAfter model.BargainSession
	if err := db.First(&sessAfter, sess.ID).Error; err != nil {
		t.Fatalf("session reload: %v", err)
	}
	if sessAfter.Status != model.BargainStatusOrdered {
		t.Fatalf("bargain status want ordered, got %d", sessAfter.Status)
	}
}

func TestSettlePaymentInTx_ZeroPayMarksPaid(t *testing.T) {
	db := setupZeroPayOrderDB(t)
	phone := "13900000002"
	if err := db.Create(&model.Account{ID: 101, Phone: &phone}).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	order := model.Order{
		OrderNo: "ZPAY001", AccountID: 101, MerchantID: 1,
		Status: model.OrderStatusPendingPay, PayStatus: model.PayStatusUnpaid,
		TotalAmount: 0, PayAmount: 0,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	svc := &OrderService{
		DB:      db,
		Payment: &deferredSettleProvider{inner: payment.MockProvider{DB: db}},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return svc.settlePaymentInTx(tx, order.ID, 0, time.Now())
	})
	if err != nil {
		t.Fatalf("settle zero: %v", err)
	}
	var after model.Order
	if err := db.First(&after, order.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.PayStatus != model.PayStatusPaid {
		t.Fatalf("pay_status want paid, got %d", after.PayStatus)
	}
	if after.Status != model.OrderStatusPendingFulfill {
		t.Fatalf("status want pending_fulfill, got %d", after.Status)
	}
}
