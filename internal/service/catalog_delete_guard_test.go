package service

import (
	"errors"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupCatalogDeleteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Product{},
		&model.Activity{},
		&model.ActivityProduct{},
		&model.UserInventory{},
		&model.UserInventoryUsage{},
		&model.Order{},
		&model.OrderItem{},
		&model.DeliveryOrder{},
		&model.TakeoutOrder{},
		&model.TakeoutOrderItem{},
		&model.BargainSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedDeleteProduct(db *gorm.DB, name string) *model.Product {
	p := model.Product{
		MerchantID: 1, CategoryID: 1, Name: name, CoverURL: "x",
		Price: 9.9, Status: model.ProductStatusOn, EnableDeal: 1, ItemType: model.ProductItemTypePhysical,
	}
	if err := db.Create(&p).Error; err != nil {
		panic(err)
	}
	return &p
}

func TestProductDelete_BlockedByUnusedBag(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "电影票")
	inv := model.UserInventory{AccountID: 8, ProductID: p.ID, Quantity: 2}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}

	err := (&ProductService{DB: db}).Delete(p.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
	var still model.Product
	if err := db.First(&still, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.IsDeleted != 0 {
		t.Fatal("product should remain")
	}
}

func TestProductDelete_AllowedWhenIdle(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "闲置")
	if err := (&ProductService{DB: db}).Delete(p.ID, nil); err != nil {
		t.Fatal(err)
	}
}

func TestProductDelete_BlockedByPendingVerify(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "待核销")
	inv := model.UserInventory{AccountID: 8, ProductID: p.ID, Quantity: 0}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	usage := model.UserInventoryUsage{
		AccountID: 8, InventoryID: inv.ID, ProductID: p.ID, MerchantID: 1,
		Quantity: 1, Status: model.InventoryUsagePendingVerify,
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	err := (&ProductService{DB: db}).Delete(p.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
}

func TestProductDelete_BlockedByPendingPayOrder(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "待付款")
	order := model.Order{
		OrderNo: "P1", AccountID: 8, MerchantID: 1,
		Status: model.OrderStatusPendingPay, PayStatus: model.PayStatusUnpaid, DeliveryType: 1,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	item := model.OrderItem{
		OrderID: order.ID, ProductID: p.ID, ProductName: p.Name, Quantity: 1, UnitPrice: 9.9, Subtotal: 9.9,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	err := (&ProductService{DB: db}).Delete(p.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
}

func TestProductDelete_AllowedAfterCompletedAndEmptyBag(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "已完成")
	order := model.Order{
		OrderNo: "C1", AccountID: 8, MerchantID: 1,
		Status: model.OrderStatusCompleted, PayStatus: model.PayStatusPaid, DeliveryType: 1,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	item := model.OrderItem{
		OrderID: order.ID, ProductID: p.ID, ProductName: p.Name, Quantity: 1, UnitPrice: 9.9, Subtotal: 9.9,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ProductService{DB: db}).Delete(p.ID, nil); err != nil {
		t.Fatal(err)
	}
}

func TestProductDelete_BlockedByTakeoutPreparing(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "外卖")
	to := model.TakeoutOrder{
		OrderNo: "T1", AccountID: 8, MerchantID: 1, Status: model.TakeoutStatusPreparing, PayStatus: model.PayStatusPaid,
	}
	if err := db.Create(&to).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TakeoutOrderItem{
		TakeoutOrderID: to.ID, ProductID: p.ID, ProductName: p.Name, Quantity: 1, UnitPrice: 9.9, Subtotal: 9.9,
	}).Error; err != nil {
		t.Fatal(err)
	}
	err := (&ProductService{DB: db}).Delete(p.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
}

func TestProductDelete_BlockedByUnconfirmedDelivery(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "配送中")
	inv := model.UserInventory{AccountID: 8, ProductID: p.ID, Quantity: 0}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	usage := model.UserInventoryUsage{
		AccountID: 8, InventoryID: inv.ID, ProductID: p.ID, MerchantID: 1,
		Quantity: 1, Status: model.InventoryUsagePendingShip,
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	d := model.DeliveryOrder{
		InventoryUsageID: &usage.ID, Status: model.DeliveryDelivered, UserConfirmed: 0,
	}
	if err := db.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	err := (&ProductService{DB: db}).Delete(p.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
}

func TestActivityProductRemove_BlockedByUnusedBagFromActivityOrder(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "活动票")
	now := time.Now()
	act := model.Activity{
		Name: "秒杀", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), Status: model.ActivityStatusOn,
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	ap := model.ActivityProduct{ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 1, Status: 1}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatal(err)
	}
	actID, apID := act.ID, ap.ID
	order := model.Order{
		OrderNo: "A1", AccountID: 8, MerchantID: 1, ActivityID: &actID,
		Status: model.OrderStatusCompleted, PayStatus: model.PayStatusPaid, DeliveryType: 1,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.OrderItem{
		OrderID: order.ID, ProductID: p.ID, ActivityID: &actID, ActivityProductID: &apID,
		ProductName: p.Name, Quantity: 1, UnitPrice: 1, Subtotal: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserInventory{AccountID: 8, ProductID: p.ID, Quantity: 1}).Error; err != nil {
		t.Fatal(err)
	}

	err := (&ActivityService{DB: db}).RemoveProductInActivity(act.ID, ap.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
}

func TestActivityProductRemove_AllowsWhenBagIsFromOtherChannel(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "同品")
	now := time.Now()
	act := model.Activity{
		Name: "秒杀", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), Status: model.ActivityStatusOn,
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	ap := model.ActivityProduct{ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 1, Status: 1}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserInventory{AccountID: 8, ProductID: p.ID, Quantity: 1}).Error; err != nil {
		t.Fatal(err)
	}

	if err := (&ActivityService{DB: db}).RemoveProductInActivity(act.ID, ap.ID, nil); err != nil {
		t.Fatal(err)
	}
}

func TestActivityDelete_BlockedByPendingActivityOrder(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	p := seedDeleteProduct(db, "团")
	now := time.Now()
	act := model.Activity{
		Name: "秒杀", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), Status: model.ActivityStatusOn,
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	ap := model.ActivityProduct{ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 1, Status: 1}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatal(err)
	}
	actID, apID := act.ID, ap.ID
	order := model.Order{
		OrderNo: "G1", AccountID: 8, MerchantID: 1, ActivityID: &actID,
		Status: model.OrderStatusPendingGroup, PayStatus: model.PayStatusPaid, DeliveryType: 1,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.OrderItem{
		OrderID: order.ID, ProductID: p.ID, ActivityID: &actID, ActivityProductID: &apID,
		ProductName: p.Name, Quantity: 1, UnitPrice: 1, Subtotal: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	err := (&ActivityService{DB: db}).Delete(act.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
}

func seedBargainActivity(t *testing.T, db *gorm.DB) (*model.Activity, *model.ActivityProduct, *model.Product) {
	t.Helper()
	p := seedDeleteProduct(db, "砍价票")
	now := time.Now()
	act := model.Activity{
		Name: "砍价场", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), Status: model.ActivityStatusOn,
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	ap := model.ActivityProduct{ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 29.9, Status: 1, EnableBargain: 1}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatal(err)
	}
	return &act, &ap, p
}

func TestActivityProductRemove_BlockedByActiveBargain(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	act, ap, p := seedBargainActivity(t, db)
	sess := model.BargainSession{
		ActivityID: act.ID, ActivityProductID: ap.ID, ProductID: p.ID, MerchantID: 1,
		InitiatorAccountID: 8, OriginPrice: 29.9, FloorPrice: 9.9, CurrentPrice: 20,
		Status: model.BargainStatusOngoing, ExpireAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	err := (&ActivityService{DB: db}).RemoveProductInActivity(act.ID, ap.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
	if msg := CatalogInUseMessage(err); msg != "有用户正在砍价中，无法删除" {
		t.Fatalf("message=%q", msg)
	}
}

func TestActivityProductRemove_AllowsWhenBargainExpired(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	act, ap, p := seedBargainActivity(t, db)
	sess := model.BargainSession{
		ActivityID: act.ID, ActivityProductID: ap.ID, ProductID: p.ID, MerchantID: 1,
		InitiatorAccountID: 8, OriginPrice: 29.9, FloorPrice: 9.9, CurrentPrice: 20,
		Status: model.BargainStatusOngoing, ExpireAt: time.Now().Add(-time.Minute),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ActivityService{DB: db}).RemoveProductInActivity(act.ID, ap.ID, nil); err != nil {
		t.Fatal(err)
	}
}

func TestActivityDelete_BlockedByActiveBargain(t *testing.T) {
	db := setupCatalogDeleteTestDB(t)
	act, ap, p := seedBargainActivity(t, db)
	sess := model.BargainSession{
		ActivityID: act.ID, ActivityProductID: ap.ID, ProductID: p.ID, MerchantID: 1,
		InitiatorAccountID: 8, OriginPrice: 29.9, FloorPrice: 9.9, CurrentPrice: 20,
		Status: model.BargainStatusOngoing, ExpireAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	err := (&ActivityService{DB: db}).Delete(act.ID, nil)
	if !errors.Is(err, ErrCatalogInUse) {
		t.Fatalf("expected ErrCatalogInUse, got %v", err)
	}
	if msg := CatalogInUseMessage(err); msg != "有用户正在砍价中，无法删除" {
		t.Fatalf("message=%q", msg)
	}
}
