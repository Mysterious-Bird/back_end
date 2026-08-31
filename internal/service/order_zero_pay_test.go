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
		&model.ProductOptionGroup{},
		&model.ProductOptionItem{},
		&model.Activity{},
		&model.ActivityProduct{},
		&model.BargainSession{},
		&model.Order{},
		&model.OrderItem{},
		&model.UserInventory{},
		&model.UserInventoryLog{},
		&model.UserInventoryUsage{},
		&model.VerificationCode{},
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
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewApproved {
		// auto_approve 应入履约且审核通过
		t.Fatalf("status=%d review=%d want fulfill/approved", order.Status, order.MerchantReviewStage)
	}

	// 回归：0 元单自动审核必须真正入包并生成待核销核销码，
	// 不能出现「审核通过但 order_item 之后才插入导致入包被跳过」。
	// 入包后 AutoPickup 立即把背包转为待核销 usage，背包数量归 0，核销码落在 usage 上。
	var creditLog model.UserInventoryLog
	if err := db.Where("account_id = ? AND order_id = ? AND event_type = ? AND delta_qty > 0",
		100, order.ID, model.InventoryEventOrderCredit).First(&creditLog).Error; err != nil {
		t.Fatalf("order credit log not written: %v", err)
	}
	var usage model.UserInventoryUsage
	if err := db.Where("account_id = ? AND source_order_id = ?", 100, order.ID).First(&usage).Error; err != nil {
		t.Fatalf("pending verify usage not created: %v", err)
	}
	if usage.Status != model.InventoryUsagePendingVerify {
		t.Fatalf("usage status want pending_verify, got %d", usage.Status)
	}
	var vc model.VerificationCode
	if err := db.Where("inventory_usage_id = ?", usage.ID).First(&vc).Error; err != nil {
		t.Fatalf("verification code not created: %v", err)
	}
	if vc.Code == "" {
		t.Fatalf("verification code empty")
	}

	var sessAfter model.BargainSession
	if err := db.First(&sessAfter, sess.ID).Error; err != nil {
		t.Fatalf("session reload: %v", err)
	}
	if sessAfter.Status != model.BargainStatusOrdered {
		t.Fatalf("bargain status want ordered, got %d", sessAfter.Status)
	}
}

func TestRepairApprovedWithoutCredit(t *testing.T) {
	db := setupZeroPayOrderDB(t)
	phone := "13900000003"
	if err := db.Create(&model.Account{ID: 102, Phone: &phone}).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	seedOpenMerchant(db, 1, "店A")
	p := seedOnShelfProduct(db, 1, "砍价品")

	// 构造历史 bug 状态：订单已通过待履约，明细已存在，但从未入包（无 order_credit 流水）
	order := model.Order{
		OrderNo: "BROKEN001", AccountID: 102, MerchantID: 1,
		Status: model.OrderStatusPendingFulfill, MerchantReviewStage: model.MerchantReviewApproved,
		DeliveryType: model.DeliveryTypePickup, PayAmount: 0, PayStatus: model.PayStatusPaid,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	if err := db.Create(&model.OrderItem{
		OrderID: order.ID, ProductID: p.ID, ProductName: p.Name,
		UnitPrice: 0, Quantity: 1, Subtotal: 0,
	}).Error; err != nil {
		t.Fatalf("item: %v", err)
	}

	svc := &OrderService{
		DB:           db,
		InventorySvc: &InventoryService{DB: db},
	}
	if err := svc.RepairApprovedWithoutCredit(order.ID); err != nil {
		t.Fatalf("repair: %v", err)
	}

	var creditLog model.UserInventoryLog
	if err := db.Where("order_id = ? AND event_type = ? AND delta_qty > 0",
		order.ID, model.InventoryEventOrderCredit).First(&creditLog).Error; err != nil {
		t.Fatalf("order credit log not written after repair: %v", err)
	}
	var usage model.UserInventoryUsage
	if err := db.Where("account_id = ? AND source_order_id = ?", 102, order.ID).First(&usage).Error; err != nil {
		t.Fatalf("pending verify usage not created after repair: %v", err)
	}
	if usage.Status != model.InventoryUsagePendingVerify {
		t.Fatalf("usage status want pending_verify, got %d", usage.Status)
	}
	var vc model.VerificationCode
	if err := db.Where("inventory_usage_id = ?", usage.ID).First(&vc).Error; err != nil {
		t.Fatalf("verification code not created after repair: %v", err)
	}

	// 幂等：再次修复不应重复入账
	if err := svc.RepairApprovedWithoutCredit(order.ID); err != nil {
		t.Fatalf("repair idempotent rerun: %v", err)
	}
	var credits int64
	if err := db.Model(&model.UserInventoryLog{}).
		Where("order_id = ? AND event_type = ?", order.ID, model.InventoryEventOrderCredit).
		Count(&credits).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if credits != 1 {
		t.Fatalf("credit log count want 1 after rerun, got %d", credits)
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
