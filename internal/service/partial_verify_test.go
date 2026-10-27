package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"
)

// 部分核销：多次核销同一码，最后一次全部核销后码才作废。
func TestVerifyPartialQuantity(t *testing.T) {
	db := setupVerificationApplicableTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	p := seedOnShelfProduct(db, 1, "电影票")

	inv := model.UserInventory{AccountID: 10, ProductID: p.ID, Quantity: 3}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	usage := model.UserInventoryUsage{
		AccountID: 10, InventoryID: inv.ID, ProductID: p.ID,
		MerchantID: 1, UsageMerchantID: 1, Quantity: 3,
		DeliveryType: model.DeliveryTypePickup, Status: model.InventoryUsagePendingVerify,
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	code := "VERIFY-PARTIAL-001"
	vc := model.VerificationCode{
		InventoryUsageID: &usage.ID, AccountID: 10, Code: code,
		Status: model.VerificationCodeUnused,
	}
	if err := db.Create(&vc).Error; err != nil {
		t.Fatal(err)
	}
	svc := newVerificationSvc(db)

	preview, err := svc.LookupByCode(1, code)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if preview.RemainingQuantity != 3 || preview.VerifiedQuantity != 0 {
		t.Fatalf("preview want remaining=3 verified=0, got %+v", preview)
	}

	// 超过剩余数量被拒
	if _, err := svc.Verify(1, 200, code, 5, nil, nil); !errors.Is(err, ErrVerifyQuantityInvalid) {
		t.Fatalf("expect quantity invalid, got %v", err)
	}

	// 第一次部分核销 1 件：usage 保持待核销、码仍可用
	rec, err := svc.Verify(1, 200, code, 1, nil, nil)
	if err != nil {
		t.Fatalf("partial verify: %v", err)
	}
	if rec.Quantity != 1 {
		t.Fatalf("record quantity want 1, got %d", rec.Quantity)
	}
	var u1 model.UserInventoryUsage
	if err := db.First(&u1, usage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if u1.VerifiedQuantity != 1 || u1.Status != model.InventoryUsagePendingVerify {
		t.Fatalf("after partial: verified=%d status=%d", u1.VerifiedQuantity, u1.Status)
	}
	var c1 model.VerificationCode
	if err := db.First(&c1, vc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if c1.Status != model.VerificationCodeUnused {
		t.Fatalf("code should stay unused after partial, got %d", c1.Status)
	}

	// 第二次 quantity=0 表示核销剩余全部
	rec2, err := svc.Verify(1, 200, code, 0, nil, nil)
	if err != nil {
		t.Fatalf("final verify: %v", err)
	}
	if rec2.Quantity != 2 {
		t.Fatalf("final record quantity want 2, got %d", rec2.Quantity)
	}
	var u2 model.UserInventoryUsage
	if err := db.First(&u2, usage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if u2.VerifiedQuantity != 3 || u2.Status != model.InventoryUsageCompleted {
		t.Fatalf("after final: verified=%d status=%d", u2.VerifiedQuantity, u2.Status)
	}
	var c2 model.VerificationCode
	if err := db.First(&c2, vc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if c2.Status != model.VerificationCodeUsed {
		t.Fatalf("code should be used, got %d", c2.Status)
	}

	// 已全部核销后不可再核销
	if _, err := svc.Verify(1, 200, code, 1, nil, nil); !errors.Is(err, ErrVerifyCodeUsed) {
		t.Fatalf("expect code used, got %v", err)
	}
}

// 套餐商品不允许部分核销。
func TestVerifyPartialPackageRejected(t *testing.T) {
	db := setupVerificationApplicableTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	p := seedOnShelfProduct(db, 1, "套餐")
	p.ItemType = model.ProductItemTypePackage
	if err := db.Save(p).Error; err != nil {
		t.Fatal(err)
	}

	inv := model.UserInventory{AccountID: 10, ProductID: p.ID, Quantity: 2}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	usage := model.UserInventoryUsage{
		AccountID: 10, InventoryID: inv.ID, ProductID: p.ID,
		MerchantID: 1, UsageMerchantID: 1, Quantity: 2,
		DeliveryType: model.DeliveryTypePickup, Status: model.InventoryUsagePendingVerify,
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	code := "VERIFY-PKG-001"
	vc := model.VerificationCode{
		InventoryUsageID: &usage.ID, AccountID: 10, Code: code,
		Status: model.VerificationCodeUnused,
	}
	if err := db.Create(&vc).Error; err != nil {
		t.Fatal(err)
	}
	svc := newVerificationSvc(db)

	if _, err := svc.Verify(1, 200, code, 1, nil, nil); !errors.Is(err, ErrVerifyPartialPackage) {
		t.Fatalf("expect partial package rejected, got %v", err)
	}
}
