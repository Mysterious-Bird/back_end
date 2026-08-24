package service

import (
	"errors"
	"math"
	"math/rand"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupBargainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Account{},
		&model.Product{},
		&model.Activity{},
		&model.ActivityProduct{},
		&model.BargainSession{},
		&model.BargainHelp{},
		&model.BargainSettings{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&model.BargainSettings{
		ID: 1, HelpDailyMax: 20, HelpDailyRefreshTime: "00:00:00",
	}).Error; err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	return db
}

func seedBargainAP(t *testing.T, db *gorm.DB) (*model.Activity, *model.ActivityProduct, *model.Product) {
	t.Helper()
	p := model.Product{
		MerchantID: 1, CategoryID: 1, Name: "砍价品", CoverURL: "x",
		Price: 50, Status: model.ProductStatusOn, EnableDeal: 1,
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	act := model.Activity{
		MerchantID: 1, Name: "砍价活动", Status: model.ActivityStatusOn,
		StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(48 * time.Hour),
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	floor := 9.9
	ap := model.ActivityProduct{
		ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 29.9, ActivityStock: 100,
		Status: 1, EnableBargain: 1, BargainFloorPrice: &floor,
		BargainDurationHours: 24, BargainNewUserHours: 48, BargainHelpDailyMax: 20,
		EnableBargainSelfCut: 1, BargainSelfCutMode: model.BargainCutModeRandom,
		BargainSelfCutMin: 0.1, BargainSelfCutMax: 1.0,
		BargainNewMin: 1, BargainNewMax: 5,
		BargainOldMin: 0.1, BargainOldMax: 1,
	}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatal(err)
	}
	return &act, &ap, &p
}

func TestRollCutAmount_CapsAtRemain(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	got := rollCutAmount(0.5, 1, 5, r)
	if got <= 0 || got > 0.5+1e-9 {
		t.Fatalf("got %v want in (0,0.5]", got)
	}
}

func TestResolveCutAmount_FixedCapsAtRemain(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	got := resolveCutAmount(0.3, model.BargainCutModeFixed, 1.0, 1.0, r)
	if got != 0.3 {
		t.Fatalf("got %v want 0.3 (cap at remain / floor)", got)
	}
	got = resolveCutAmount(5, model.BargainCutModeFixed, 1.5, 1.5, r)
	if got != 1.5 {
		t.Fatalf("got %v want 1.5", got)
	}
}


func TestRollCutAmount_ZeroRemain(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	if got := rollCutAmount(0, 0.1, 1, r); got != 0 {
		t.Fatalf("got %v want 0", got)
	}
}

func TestBargainStart_SelfCutOnce(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, _ := seedBargainAP(t, db)
	acc := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(acc.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.SelfCutDone != 0 {
		t.Fatal("should not auto self cut on start")
	}
	if view.CurrentPrice != view.OriginPrice {
		t.Fatalf("price should stay origin before manual self cut: got %v want %v", view.CurrentPrice, view.OriginPrice)
	}
	after, err := svc.Help(view.ID, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SelfCutDone != 1 {
		t.Fatal("expected self cut after manual help")
	}
	if after.CurrentPrice >= after.OriginPrice {
		t.Fatalf("price should drop after self cut: %v >= %v", after.CurrentPrice, after.OriginPrice)
	}
	cut := roundMoney(after.OriginPrice - after.CurrentPrice)
	if cut > ap.BargainSelfCutMax+1e-9 {
		t.Fatalf("self cut %v exceeds max %v", cut, ap.BargainSelfCutMax)
	}
}

func TestBargainSelfCut_ConsumesDailyLimit(t *testing.T) {
	db := setupBargainTestDB(t)
	if err := db.Model(&model.BargainSettings{}).Where("id = 1").Update("help_daily_max", 1).Error; err != nil {
		t.Fatal(err)
	}
	_, ap, _ := seedBargainAP(t, db)
	acc := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(acc.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Help(view.ID, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelSession(acc.ID, view.ID); err != nil {
		t.Fatal(err)
	}
	view2, err := svc.StartSession(acc.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Help(view2.ID, acc.ID); !errors.Is(err, ErrBargainDailyLimit) {
		t.Fatalf("expected ErrBargainDailyLimit on second self cut, got %v", err)
	}
}

func TestBargainStart_SelfCutDisabledByDefault(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, _ := seedBargainAP(t, db)
	ap.EnableBargainSelfCut = 0
	if err := db.Save(ap).Error; err != nil {
		t.Fatal(err)
	}
	acc := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(acc.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.SelfCutDone != 0 {
		t.Fatal("self cut should be skipped when disabled")
	}
	if view.CurrentPrice != view.OriginPrice {
		t.Fatalf("price should stay origin: got %v want %v", view.CurrentPrice, view.OriginPrice)
	}
	if _, err := svc.Help(view.ID, acc.ID); !errors.Is(err, ErrBargainSelfCutDisabled) {
		t.Fatalf("expected ErrBargainSelfCutDisabled, got %v", err)
	}
}

func TestBargainStart_LimitExceeded(t *testing.T) {
	db := setupBargainTestDB(t)
	if err := db.AutoMigrate(&model.Order{}, &model.OrderItem{}); err != nil {
		t.Fatal(err)
	}
	_, ap, _ := seedBargainAP(t, db)
	ap.PerUserMaxQty = 1
	if err := db.Save(ap).Error; err != nil {
		t.Fatal(err)
	}
	acc := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	actID := ap.ActivityID
	apID := ap.ID
	order := model.Order{
		OrderNo: "B-LIMIT-1", AccountID: acc.ID, MerchantID: 1,
		Status: model.OrderStatusCompleted, DeliveryType: 1,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	item := model.OrderItem{
		OrderID: order.ID, ProductID: ap.ProductID, ActivityID: &actID, ActivityProductID: &apID,
		ProductName: "砍价品", Quantity: 1, UnitPrice: 29.9, Subtotal: 29.9,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	if _, err := svc.StartSession(acc.ID, ap.ID); !errors.Is(err, ErrActivityLimitExceeded) {
		t.Fatalf("expected ErrActivityLimitExceeded, got %v", err)
	}
}

func TestBargainHelp_DuplicateRejected(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, _ := seedBargainAP(t, db)
	init := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	helper := model.Account{CreatedAt: time.Now()} // new user
	if err := db.Create(&init).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&helper).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(init.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Help(view.ID, helper.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Help(view.ID, helper.ID); !errors.Is(err, ErrBargainAlreadyHelped) {
		t.Fatalf("expected ErrBargainAlreadyHelped, got %v", err)
	}
}

func TestBargainHelp_ExpiredRejected(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, _ := seedBargainAP(t, db)
	init := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	helper := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&init).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&helper).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(init.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BargainSession{}).Where("id = ?", view.ID).
		Updates(map[string]interface{}{"expire_at": time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Help(view.ID, helper.ID); !errors.Is(err, ErrBargainExpired) {
		t.Fatalf("expected ErrBargainExpired, got %v", err)
	}
}

func TestBargainCancel_FreesSlotForRestart(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, _ := seedBargainAP(t, db)
	ap.EnableBargainSelfCut = 0
	if err := db.Save(ap).Error; err != nil {
		t.Fatal(err)
	}
	acc := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(acc.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.CancelSession(acc.ID, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != model.BargainStatusCancelled {
		t.Fatalf("status=%d want cancelled", cancelled.Status)
	}
	if cancelled.CanCancel || cancelled.CanBuy {
		t.Fatal("cancelled session should not allow buy/cancel")
	}
	again, err := svc.StartSession(acc.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == view.ID {
		t.Fatal("should start a new session after cancel")
	}
	if again.Status != model.BargainStatusOngoing {
		t.Fatalf("status=%d", again.Status)
	}
}

func TestBargainCancel_ForbiddenForNonInitiator(t *testing.T) {
	db := setupBargainTestDB(t)
	_, ap, _ := seedBargainAP(t, db)
	ap.EnableBargainSelfCut = 0
	if err := db.Save(ap).Error; err != nil {
		t.Fatal(err)
	}
	init := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	other := model.Account{CreatedAt: time.Now().Add(-72 * time.Hour)}
	if err := db.Create(&init).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BargainService{DB: db}
	view, err := svc.StartSession(init.ID, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelSession(other.ID, view.ID); !errors.Is(err, ErrBargainForbidden) {
		t.Fatalf("expected ErrBargainForbidden, got %v", err)
	}
}

func TestAssertSessionBuyable(t *testing.T) {
	sess := &model.BargainSession{
		InitiatorAccountID: 9,
		Status:             model.BargainStatusOngoing,
		ExpireAt:           time.Now().Add(time.Hour),
		CurrentPrice:       12.3,
	}
	if err := assertSessionBuyable(sess, 9, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := assertSessionBuyable(sess, 8, time.Now()); !errors.Is(err, ErrBargainForbidden) {
		t.Fatalf("got %v", err)
	}
	sess.ExpireAt = time.Now().Add(-time.Minute)
	if err := assertSessionBuyable(sess, 9, time.Now()); !errors.Is(err, ErrBargainExpired) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBargainConfig(t *testing.T) {
	floor := 5.0
	in := ActivityProductInput{
		ProductID: 1, ActivityPrice: 10, EnableBargain: 1, BargainFloorPrice: &floor,
		BargainNewMin: 1, BargainNewMax: 2, BargainOldMin: 0.1, BargainOldMax: 1,
		BargainDurationHours: 24,
	}
	if err := validateBargainOnActivityProduct(in); err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	in.BargainFloorPrice = &zero
	if err := validateBargainOnActivityProduct(in); err != nil {
		t.Fatalf("floor=0 should pass: %v", err)
	}
	in.EnableGroupBuy = 1
	if err := validateBargainOnActivityProduct(in); err == nil {
		t.Fatal("group+bargain should fail")
	}
	in.EnableGroupBuy = 0
	bad := 10.0
	in.BargainFloorPrice = &bad
	if err := validateBargainOnActivityProduct(in); err == nil {
		t.Fatal("floor >= price should fail")
	}
	_ = math.Abs
}
