package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRefundStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Order{}, &model.PaymentRefund{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestSumRefundedOrders_UsesRefundTimeNotOrderCreated(t *testing.T) {
	db := setupRefundStatsDB(t)
	svc := &DashboardService{DB: db}

	day1 := time.Date(2026, 8, 1, 12, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.Local)
	day2End := time.Date(2026, 8, 11, 0, 0, 0, 0, time.Local)
	day2Start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.Local)

	// 旧单：下单在 day1，退款流水在 day2
	old := model.Order{
		OrderNo: "R1", AccountID: 1, MerchantID: 1,
		PayStatus: model.PayStatusRefunded, PayAmount: 10, RefundedAmount: 10,
		CreatedAt: day1, UpdatedAt: day2,
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	pr := model.PaymentRefund{
		OrderNo: "R1", OutRefundNo: "out1", RefundID: "wx1",
		SubjectType: model.PaySubjectOrder, SubjectID: old.ID,
		RefundAmount: 10, Status: 1, CreatedAt: day2, UpdatedAt: day2,
	}
	if err := db.Create(&pr).Error; err != nil {
		t.Fatalf("payment_refund: %v", err)
	}

	// 按 day2 过滤应计入；若误用 created_at 则会漏掉
	cnt, amt, err := svc.sumRefundedOrders(SalesReportFilter{StartDate: &day2Start, EndDate: &day2End})
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if cnt != 1 || amt < 9.99 || amt > 10.01 {
		t.Fatalf("day2 window: cnt=%d amt=%v want 1 / 10", cnt, amt)
	}

	day1Start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	day1End := time.Date(2026, 8, 2, 0, 0, 0, 0, time.Local)
	cnt, amt, err = svc.sumRefundedOrders(SalesReportFilter{StartDate: &day1Start, EndDate: &day1End})
	if err != nil {
		t.Fatalf("sum day1: %v", err)
	}
	if cnt != 0 || amt > 0.009 {
		t.Fatalf("day1 window should exclude refund-on-day2 order: cnt=%d amt=%v", cnt, amt)
	}
}

func TestSumRefundedOrders_LocalZeroPayUsesUpdatedAt(t *testing.T) {
	db := setupRefundStatsDB(t)
	svc := &DashboardService{DB: db}

	day2 := time.Date(2026, 8, 10, 15, 0, 0, 0, time.Local)
	day2Start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.Local)
	day2End := time.Date(2026, 8, 11, 0, 0, 0, 0, time.Local)
	o := model.Order{
		OrderNo: "Z0", AccountID: 1, MerchantID: 1,
		PayStatus: model.PayStatusRefunded, PayAmount: 0, RefundedAmount: 0,
		CreatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.Local),
		UpdatedAt: day2,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatalf("order: %v", err)
	}
	cnt, amt, err := svc.sumRefundedOrders(SalesReportFilter{StartDate: &day2Start, EndDate: &day2End})
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("want local zero-pay refund counted, cnt=%d", cnt)
	}
	if amt > 0.009 {
		t.Fatalf("zero-pay amt want 0, got %v", amt)
	}
}
