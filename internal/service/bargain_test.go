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
		BargainSelfCutMax: 1.0, BargainNewMin: 1, BargainNewMax: 5,
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
	if view.SelfCutDone != 1 {
		t.Fatal("expected self cut")
	}
	if view.CurrentPrice >= view.OriginPrice {
		t.Fatalf("price should drop after self cut: %v >= %v", view.CurrentPrice, view.OriginPrice)
	}
	cut := roundMoney(view.OriginPrice - view.CurrentPrice)
	if cut > ap.BargainSelfCutMax+1e-9 {
		t.Fatalf("self cut %v exceeds max %v", cut, ap.BargainSelfCutMax)
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
		BargainSelfCutMax: 1, BargainDurationHours: 24,
	}
	if err := validateBargainOnActivityProduct(in); err != nil {
		t.Fatal(err)
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
