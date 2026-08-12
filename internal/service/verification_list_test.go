package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestListFilteredVerificationRecordsReturnsCompleted(t *testing.T) {
	db := setupDashboardSalesTestDB(t)
	seedSharedProductSalesFixture(t, db)
	svc := &VerificationService{DB: db}

	mid := uint64(2)
	list, total, err := svc.ListFiltered(VerificationListFilter{
		MerchantID: &mid,
		Page:       1,
		PageSize:   20,
	})
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if total != 1 {
		t.Fatalf("total want 1, got %d", total)
	}
	if len(list) != 1 {
		t.Fatalf("list len want 1, got %d", len(list))
	}
	if list[0].MerchantID != 2 {
		t.Fatalf("merchant_id want 2, got %d", list[0].MerchantID)
	}
	if list[0].Code == "" {
		t.Fatal("expected verification code on view")
	}
}

func TestListFilteredVerificationExcludesNonCompletedUsage(t *testing.T) {
	db := setupDashboardSalesTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	p := seedOnShelfProduct(db, 1, "商品")
	inv := model.UserInventory{AccountID: 1, ProductID: p.ID, Quantity: 1}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	usage := model.UserInventoryUsage{
		AccountID: 1, InventoryID: inv.ID, ProductID: p.ID, MerchantID: 1,
		UsageMerchantID: 1, Quantity: 1, DeliveryType: model.DeliveryTypePickup,
		Status: model.InventoryUsagePendingVerify,
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	vc := model.VerificationCode{
		InventoryUsageID: &usage.ID, AccountID: 1, Code: "PENDING-CODE",
		Status: model.VerificationCodeUsed,
	}
	if err := db.Create(&vc).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VerificationRecord{
		VerificationCodeID: vc.ID, MerchantID: 1, OperatorID: 1, VerifiedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &VerificationService{DB: db}
	mid := uint64(1)
	_, total, err := svc.ListFiltered(VerificationListFilter{MerchantID: &mid, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if total != 0 {
		t.Fatalf("pending usage should not appear, total=%d", total)
	}
}
