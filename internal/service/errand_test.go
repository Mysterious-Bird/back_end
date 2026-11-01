package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupErrandTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Account{},
		&model.RiderApplication{},
		&model.ErrandOrder{},
		&model.ErrandSettings{},
		&model.DeliveryOrder{},
		&model.RiderEarning{},
		&model.FulfillmentEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedErrandRider(t *testing.T, db *gorm.DB) uint64 {
	t.Helper()
	nick := "骑手"
	acc := model.Account{Type: 3, Nickname: &nick}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	app := model.RiderApplication{AccountID: acc.ID, Status: model.RiderApplicationApproved, RealName: "骑手小王"}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}
	return acc.ID
}

func newErrandFixture(t *testing.T) (*gorm.DB, *ErrandService, *DeliveryService, uint64) {
	t.Helper()
	db := setupErrandTestDB(t)
	svc := &ErrandService{DB: db}
	dsvc := &DeliveryService{DB: db, ErrandSvc: svc}
	riderID := seedErrandRider(t, db)
	return db, svc, dsvc, riderID
}

func TestErrandFlow_Cash(t *testing.T) {
	db, svc, dsvc, riderID := newErrandFixture(t)
	const accountID = 100

	view, err := svc.Create(accountID, ErrandCreateInput{
		PickupShopName: "奶茶店",
		PickupAddress:  "取货路 1 号",
		ContactName:    "张三",
		ContactPhone:   "13800000000",
		Detail:         "收货路 2 号",
		GoodsDesc:      "冰美式 2 杯",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view.Status != model.ErrandStatusPendingAccept {
		t.Fatalf("status want pending accept, got %d", view.Status)
	}
	deliveryID := view.DeliveryOrderID
	if deliveryID == nil {
		t.Fatal("delivery order not created")
	}

	if _, err := dsvc.Accept(riderID, *deliveryID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	var eo model.ErrandOrder
	if err := db.First(&eo, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if eo.Status != model.ErrandStatusAccepted {
		t.Fatalf("status after accept want 2, got %d", eo.Status)
	}

	// 无券码必须录商品费 + 小票
	if _, err := svc.RiderPickup(riderID, view.ID, 0, nil); err == nil {
		t.Fatal("expect goods amount required")
	}
	got, err := svc.RiderPickup(riderID, view.ID, 8.5, []string{"receipt.jpg"})
	if err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if got.Status != model.ErrandStatusDelivering || got.PayAmount != 9.5 {
		t.Fatalf("after pickup status=%d pay=%v", got.Status, got.PayAmount)
	}

	// 线下现金送达
	got, err = svc.RiderComplete(riderID, view.ID, model.ErrandPayModeCash)
	if err != nil {
		t.Fatalf("complete cash: %v", err)
	}
	if got.Status != model.ErrandStatusPendingConfirm || got.CollectedAmount != 9.5 {
		t.Fatalf("after cash status=%d collected=%v", got.Status, got.CollectedAmount)
	}

	// 用户确认完成 + 收益入账
	got, err = svc.Confirm(accountID, view.ID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got.Status != model.ErrandStatusCompleted {
		t.Fatalf("status want completed, got %d", got.Status)
	}
	var earn model.RiderEarning
	if err := db.Where("errand_order_id = ?", view.ID).First(&earn).Error; err != nil {
		t.Fatalf("earning not created: %v", err)
	}
	if earn.Amount != 9.5 || earn.RiderID != riderID {
		t.Fatalf("earning amount=%v rider=%d", earn.Amount, earn.RiderID)
	}
}

func TestErrandFlow_VoucherNoGoodsAmount(t *testing.T) {
	db, svc, dsvc, riderID := newErrandFixture(t)
	view, err := svc.Create(101, ErrandCreateInput{
		PickupAddress: "取货路 1 号", ContactName: "李四", ContactPhone: "13900000000",
		Detail: "收货路 2 号", GoodsDesc: "团购套餐", VoucherCode: "88881234",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := dsvc.Accept(riderID, *view.DeliveryOrderID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// 有券码不允许再报商品费
	if _, err := svc.RiderPickup(riderID, view.ID, 5, nil); err == nil {
		t.Fatal("expect voucher order rejects goods amount")
	}
	got, err := svc.RiderPickup(riderID, view.ID, 0, nil)
	if err != nil {
		t.Fatalf("pickup voucher: %v", err)
	}
	if got.PayAmount != 1 {
		t.Fatalf("voucher pay amount want 1, got %v", got.PayAmount)
	}
	var eo model.ErrandOrder
	_ = db.First(&eo, view.ID).Error
	if eo.GoodsAmount != 0 {
		t.Fatalf("voucher goods amount want 0, got %v", eo.GoodsAmount)
	}
}

func TestErrandCancelBeforeAccept(t *testing.T) {
	db, svc, _, _ := newErrandFixture(t)
	view, err := svc.Create(102, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "王五", ContactPhone: "13700000000",
		Detail: "收货路", GoodsDesc: "文件",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Cancel(102, view.ID, "临时不需要")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got.Status != model.ErrandStatusCancelled {
		t.Fatalf("status want cancelled, got %d", got.Status)
	}
	var d model.DeliveryOrder
	if err := db.First(&d, *view.DeliveryOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliveryCancelled {
		t.Fatalf("delivery status want cancelled, got %d", d.Status)
	}
}

func TestErrandAutoConfirm(t *testing.T) {
	db, svc, dsvc, riderID := newErrandFixture(t)
	view, err := svc.Create(103, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "赵六", ContactPhone: "13600000000",
		Detail: "收货路", GoodsDesc: "药品", VoucherCode: "V123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dsvc.Accept(riderID, *view.DeliveryOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RiderPickup(riderID, view.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RiderComplete(riderID, view.ID, model.ErrandPayModeCash); err != nil {
		t.Fatal(err)
	}
	// 模拟 25 小时未确认
	old := time.Now().Add(-25 * time.Hour)
	if err := db.Model(&model.ErrandOrder{}).Where("id = ?", view.ID).
		Update("updated_at", old).Error; err != nil {
		t.Fatal(err)
	}
	n, err := svc.AutoConfirmStale(time.Now())
	if err != nil {
		t.Fatalf("auto confirm: %v", err)
	}
	if n != 1 {
		t.Fatalf("auto confirm count want 1, got %d", n)
	}
	var eo model.ErrandOrder
	_ = db.First(&eo, view.ID).Error
	if eo.Status != model.ErrandStatusCompleted {
		t.Fatalf("status want completed, got %d", eo.Status)
	}
}

func TestErrandFeeByItemCount(t *testing.T) {
	_, svc, _, _ := newErrandFixture(t)
	cases := []struct {
		count int
		want  float64
	}{
		{1, 1}, {2, 1}, {3, 2}, {4, 3}, {5, 4}, {10, 9},
	}
	for _, tc := range cases {
		view, err := svc.Create(104, ErrandCreateInput{
			PickupAddress: "取货路", ContactName: "孙七", ContactPhone: "13500000000",
			Detail: "收货路", GoodsDesc: "商品", ItemCount: tc.count,
		})
		if err != nil {
			t.Fatalf("create count=%d: %v", tc.count, err)
		}
		if view.DeliveryFee != tc.want || view.ItemCount != tc.count {
			t.Fatalf("count=%d fee=%v item=%d, want fee=%v",
				tc.count, view.DeliveryFee, view.ItemCount, tc.want)
		}
	}
	if _, err := svc.Create(104, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "孙七", ContactPhone: "13500000000",
		Detail: "收货路", GoodsDesc: "商品", ItemCount: 21,
	}); err == nil {
		t.Fatal("expect item count cap error")
	}
}
