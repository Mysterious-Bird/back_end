package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupVerificationApplicableTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.MerchantProfile{},
		&model.ProductCategory{},
		&model.Product{},
		&model.ProductApplicableMerchant{},
		&model.UserInventory{},
		&model.UserInventoryUsage{},
		&model.VerificationCode{},
		&model.VerificationRecord{},
		&model.ProductOptionGroup{},
		&model.ProductOptionItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type verificationApplicableFixture struct {
	product *model.Product
	usage   *model.UserInventoryUsage
	code    string
}

func seedVerificationApplicableFixture(t *testing.T, db *gorm.DB) verificationApplicableFixture {
	t.Helper()
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	seedOpenMerchant(db, 3, "店C")
	p := seedOnShelfProduct(db, 1, "跨店商品")
	productSvc := &ProductService{DB: db}
	if err := productSvc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	inv := model.UserInventory{AccountID: 100, ProductID: p.ID, Quantity: 1}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatalf("create inventory: %v", err)
	}
	usage := model.UserInventoryUsage{
		AccountID:       100,
		InventoryID:     inv.ID,
		ProductID:       p.ID,
		MerchantID:      1,
		UsageMerchantID: 1,
		Quantity:        1,
		DeliveryType:    model.DeliveryTypePickup,
		Status:          model.InventoryUsagePendingVerify,
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatalf("create usage: %v", err)
	}
	code := "VERIFY-SHARED-001"
	vc := model.VerificationCode{
		InventoryUsageID: &usage.ID,
		AccountID:        100,
		Code:             code,
		Status:           model.VerificationCodeUnused,
	}
	if err := db.Create(&vc).Error; err != nil {
		t.Fatalf("create verification code: %v", err)
	}
	return verificationApplicableFixture{product: p, usage: &usage, code: code}
}

func newVerificationSvc(db *gorm.DB) *VerificationService {
	return &VerificationService{
		DB:         db,
		ProductSvc: &ProductService{DB: db},
	}
}

func TestLookupByCodeApplicableMerchant(t *testing.T) {
	db := setupVerificationApplicableTestDB(t)
	fx := seedVerificationApplicableFixture(t, db)
	svc := newVerificationSvc(db)

	view, err := svc.LookupByCode(2, fx.code)
	if err != nil {
		t.Fatalf("lookup as applicable merchant 2: %v", err)
	}
	if view == nil || view.ProductID != fx.product.ID {
		t.Fatalf("unexpected preview: %+v", view)
	}

	_, err = svc.LookupByCode(3, fx.code)
	if err == nil {
		t.Fatal("expected error for non-applicable merchant 3")
	}
	if !errors.Is(err, ErrVerifyMerchantMismatch) {
		t.Fatalf("expected merchant mismatch, got %v", err)
	}
}

func TestVerifyApplicableMerchantSetsUsageMerchant(t *testing.T) {
	db := setupVerificationApplicableTestDB(t)
	fx := seedVerificationApplicableFixture(t, db)
	svc := newVerificationSvc(db)

	record, err := svc.Verify(2, 200, fx.code, 0, nil, nil)
	if err != nil {
		t.Fatalf("verify as applicable merchant 2: %v", err)
	}
	if record == nil || record.MerchantID != 2 {
		t.Fatalf("record merchant_id want 2, got %+v", record)
	}

	var usage model.UserInventoryUsage
	if err := db.First(&usage, fx.usage.ID).Error; err != nil {
		t.Fatalf("reload usage: %v", err)
	}
	if usage.Status != model.InventoryUsageCompleted {
		t.Fatalf("usage status want completed, got %d", usage.Status)
	}
	if usage.UsageMerchantID != 2 {
		t.Fatalf("usage_merchant_id want 2 (scanning store), got %d", usage.UsageMerchantID)
	}
}

func TestVerifyNonApplicableMerchantFails(t *testing.T) {
	db := setupVerificationApplicableTestDB(t)
	fx := seedVerificationApplicableFixture(t, db)
	svc := newVerificationSvc(db)

	_, err := svc.Verify(3, 300, fx.code, 0, nil, nil)
	if err == nil {
		t.Fatal("expected error for non-applicable merchant 3")
	}
	if !errors.Is(err, ErrVerifyMerchantMismatch) {
		t.Fatalf("expected merchant mismatch, got %v", err)
	}
}
