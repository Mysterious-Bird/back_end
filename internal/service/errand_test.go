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
		&model.PaymentTransaction{},
		&model.PaymentRefund{},
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
	if view.Status != model.ErrandStatusPendingPay {
		t.Fatalf("status want pending pay, got %d", view.Status)
	}
	if view.DeliveryOrderID != nil {
		t.Fatal("delivery order should not exist before fee paid")
	}
	// 先付跑腿费：支付成功后才创建配送单并进入待接单池
	if _, err := svc.CreatePrepay(accountID, view.ID); err != nil {
		t.Fatalf("prepay fee: %v", err)
	}
	view, err = svc.Get(accountID, view.ID)
	if err != nil {
		t.Fatalf("get after pay: %v", err)
	}
	if view.Status != model.ErrandStatusPendingAccept || view.DeliveryOrderID == nil {
		t.Fatalf("after pay status=%d delivery=%v", view.Status, view.DeliveryOrderID)
	}
	deliveryID := view.DeliveryOrderID

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
	if got.Status != model.ErrandStatusPendingConfirm || got.CollectedAmount != 8.5 {
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
	if _, err := svc.CreatePrepay(101, view.ID); err != nil {
		t.Fatalf("prepay fee: %v", err)
	}
	view, err = svc.Get(101, view.ID)
	if err != nil {
		t.Fatalf("get after pay: %v", err)
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
	// 未支付时不存在配送单，也不应有骑手可见
	var cnt int64
	if err := db.Model(&model.DeliveryOrder{}).Where("errand_order_id = ?", view.ID).Count(&cnt).Error; err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("delivery order want 0, got %d", cnt)
	}
}

func TestErrandCancelAfterPayRefundsFee(t *testing.T) {
	db, svc, _, _ := newErrandFixture(t)
	view, err := svc.Create(107, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "钱八", ContactPhone: "13400000000",
		Detail: "收货路", GoodsDesc: "文件",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreatePrepay(107, view.ID); err != nil {
		t.Fatalf("prepay fee: %v", err)
	}
	if _, err := svc.Cancel(107, view.ID, "不要了"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var eo model.ErrandOrder
	if err := db.First(&eo, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if eo.Status != model.ErrandStatusCancelled {
		t.Fatalf("status want cancelled, got %d", eo.Status)
	}
	if eo.RefundedAmount != 1 || eo.PayStatus != model.PayStatusRefunded {
		t.Fatalf("refund want 1/refunded, got refunded=%v pay_status=%d", eo.RefundedAmount, eo.PayStatus)
	}
}

func TestErrandUnpaidStaleCancel(t *testing.T) {
	db, svc, _, _ := newErrandFixture(t)
	view, err := svc.Create(105, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "周九", ContactPhone: "13300000000",
		Detail: "收货路", GoodsDesc: "文件",
	})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := db.Model(&model.ErrandOrder{}).Where("id = ?", view.ID).
		Update("pay_expire_at", past).Error; err != nil {
		t.Fatal(err)
	}
	n, err := svc.CancelUnpaidStale(time.Now())
	if err != nil {
		t.Fatalf("stale cancel: %v", err)
	}
	if n != 1 {
		t.Fatalf("stale cancel count want 1, got %d", n)
	}
	var eo model.ErrandOrder
	_ = db.First(&eo, view.ID).Error
	if eo.Status != model.ErrandStatusCancelled {
		t.Fatalf("status want cancelled, got %d", eo.Status)
	}
}

func TestErrandGoodsPrepay(t *testing.T) {
	db, svc, dsvc, riderID := newErrandFixture(t)
	view, err := svc.Create(106, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "吴十", ContactPhone: "13200000000",
		Detail: "收货路", GoodsDesc: "水果", ItemCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.DeliveryFee != 2 {
		t.Fatalf("fee want 2, got %v", view.DeliveryFee)
	}
	if _, err := svc.CreatePrepay(106, view.ID); err != nil {
		t.Fatalf("prepay fee: %v", err)
	}
	view, err = svc.Get(106, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dsvc.Accept(riderID, *view.DeliveryOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RiderPickup(riderID, view.ID, 8.5, []string{"receipt.jpg"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, err := svc.CreatePrepay(106, view.ID); err != nil {
		t.Fatalf("prepay goods: %v", err)
	}
	var eo model.ErrandOrder
	if err := db.First(&eo, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if eo.GoodsPayStatus != model.PayStatusPaid || eo.Status != model.ErrandStatusPendingConfirm {
		t.Fatalf("after goods pay goods_pay_status=%d status=%d", eo.GoodsPayStatus, eo.Status)
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
	if _, err := svc.CreatePrepay(103, view.ID); err != nil {
		t.Fatalf("prepay fee: %v", err)
	}
	view, err = svc.Get(103, view.ID)
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

func TestErrandAdminResumeAfterRiderException(t *testing.T) {
	db, svc, dsvc, riderID := newErrandFixture(t)
	const accountID = 105

	newAcceptedOrder := func(goodsDesc string) uint64 {
		view, err := svc.Create(accountID, ErrandCreateInput{
			PickupAddress: "取货路", ContactName: "周八", ContactPhone: "13600000000",
			Detail: "收货路", GoodsDesc: goodsDesc,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := svc.CreatePrepay(accountID, view.ID); err != nil {
			t.Fatalf("prepay: %v", err)
		}
		view, err = svc.Get(accountID, view.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if _, err := dsvc.Accept(riderID, *view.DeliveryOrderID); err != nil {
			t.Fatalf("accept: %v", err)
		}
		return view.ID
	}

	// 已取货后骑手上报异常：恢复后应回到配送中，骑手可继续送达
	id := newAcceptedOrder("奶茶 1 杯")
	if _, err := svc.RiderPickup(riderID, id, 8.5, []string{"receipt.jpg"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, err := svc.ReportExceptionByRider(riderID, id, "车辆故障"); err != nil {
		t.Fatalf("report exception: %v", err)
	}
	if _, err := svc.AdminResume(id, "继续配送"); err != nil {
		t.Fatalf("admin resume: %v", err)
	}
	var eo model.ErrandOrder
	if err := db.First(&eo, id).Error; err != nil {
		t.Fatal(err)
	}
	if eo.Status != model.ErrandStatusDelivering {
		t.Fatalf("status after resume want delivering(3), got %d", eo.Status)
	}
	var d model.DeliveryOrder
	if err := db.First(&d, *eo.DeliveryOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliveryDelivering {
		t.Fatalf("delivery status after resume want delivering(3), got %d", d.Status)
	}
	if _, err := svc.RiderComplete(riderID, id, model.ErrandPayModeCash); err != nil {
		t.Fatalf("rider should continue after resume: %v", err)
	}

	// 未取货上报异常：恢复后回到已接单，骑手可重新确认取货
	id2 := newAcceptedOrder("咖啡 1 杯")
	if _, err := svc.ReportExceptionByRider(riderID, id2, "商家关门"); err != nil {
		t.Fatalf("report exception: %v", err)
	}
	if _, err := svc.AdminResume(id2, "继续配送"); err != nil {
		t.Fatalf("admin resume: %v", err)
	}
	var eo2 model.ErrandOrder
	if err := db.First(&eo2, id2).Error; err != nil {
		t.Fatal(err)
	}
	if eo2.Status != model.ErrandStatusAccepted {
		t.Fatalf("status after resume want accepted(2), got %d", eo2.Status)
	}
	var d2 model.DeliveryOrder
	if err := db.First(&d2, *eo2.DeliveryOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if d2.Status != model.DeliveryAccepted {
		t.Fatalf("delivery status after resume want accepted(1), got %d", d2.Status)
	}
}

func TestRiderActiveKeepsExceptionOrder(t *testing.T) {
	db, svc, dsvc, riderID := newErrandFixture(t)
	const accountID = 106

	view, err := svc.Create(accountID, ErrandCreateInput{
		PickupAddress: "取货路", ContactName: "吴九", ContactPhone: "13700000000",
		Detail: "收货路", GoodsDesc: "水果",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreatePrepay(accountID, view.ID); err != nil {
		t.Fatalf("prepay: %v", err)
	}
	view, err = svc.Get(accountID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	deliveryID := *view.DeliveryOrderID
	if _, err := dsvc.Accept(riderID, deliveryID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := svc.RiderPickup(riderID, view.ID, 8.5, []string{"receipt.jpg"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, err := svc.RiderComplete(riderID, view.ID, model.ErrandPayModeCash); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 用户在待确认收货阶段申诉：订单不应从骑手「配送中」消失
	if _, err := svc.Appeal(accountID, view.ID, "商品不符"); err != nil {
		t.Fatalf("user appeal: %v", err)
	}
	views, total, err := dsvc.ListForRider(riderID, "active", 1, 10)
	if err != nil {
		t.Fatalf("list rider active: %v", err)
	}
	if total != 1 || len(views) != 1 || views[0].Status != model.DeliveryException {
		t.Fatalf("exception order should stay in active, total=%d status=%v", total, len(views))
	}

	// 管理端恢复配送后回到待确认，骑手端不再异常
	if _, err := dsvc.AdminResolveResume(deliveryID, "协商一致"); err != nil {
		t.Fatalf("admin resume: %v", err)
	}
	var d model.DeliveryOrder
	if err := db.First(&d, deliveryID).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliveryDelivered {
		t.Fatalf("delivery status after resume want delivered(4), got %d", d.Status)
	}
	var eo model.ErrandOrder
	if err := db.First(&eo, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if eo.Status != model.ErrandStatusPendingConfirm {
		t.Fatalf("errand status after resume want pending confirm(4), got %d", eo.Status)
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
