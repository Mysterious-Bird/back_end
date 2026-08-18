package service

import (
	"errors"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupHomeCarouselTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.HomeCarousel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestCountDup_SameActivityProductAnyChannel(t *testing.T) {
	db := setupHomeCarouselTestDB(t)
	apID := uint64(7)
	actID := uint64(1)
	row := model.HomeCarousel{
		ProductID: 11, ActivityID: &actID, ActivityProductID: &apID,
		Channel: model.HomeCarouselChannelDeal, Status: model.HomeCarouselStatusOn, SortOrder: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := &HomeCarouselService{DB: db}
	n, err := svc.countDup(11, &apID, model.HomeCarouselChannelGroup)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("same AP different channel should count as dup, got %d", n)
	}
}

func TestCountDup_PlainProductAllowsOtherChannel(t *testing.T) {
	db := setupHomeCarouselTestDB(t)
	row := model.HomeCarousel{
		ProductID: 11, Channel: model.HomeCarouselChannelDeal,
		Status: model.HomeCarouselStatusOn, SortOrder: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := &HomeCarouselService{DB: db}
	n, err := svc.countDup(11, nil, model.HomeCarouselChannelGroup)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("plain product other channel should be unique, got %d", n)
	}
	n, err = svc.countDup(11, nil, model.HomeCarouselChannelDeal)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("plain product same channel should dup, got %d", n)
	}
}

func TestCreateCarousel_ActivityProductChannelFollowsKind(t *testing.T) {
	db := setupHomeCarouselTestDB(t)
	if err := db.AutoMigrate(&model.Product{}, &model.Activity{}, &model.ActivityProduct{}); err != nil {
		t.Fatal(err)
	}
	p := model.Product{MerchantID: 1, CategoryID: 1, Name: "票", CoverURL: "x", Price: 20, Status: model.ProductStatusOn}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	act := model.Activity{
		Name: "秒杀", Status: model.ActivityStatusOn,
		StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	price := 1.0
	target := uint32(3)
	ap := model.ActivityProduct{
		ActivityID: act.ID, ProductID: p.ID, ActivityPrice: 9.9, Status: 1,
		EnableGroupBuy: 1, GroupBuyPrice: &price, GroupBuyTargetCount: &target,
	}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatal(err)
	}
	svc := &HomeCarouselService{DB: db}
	aid, apid := act.ID, ap.ID
	view, err := svc.Create(HomeCarouselCreateInput{
		ProductID: p.ID, ActivityID: &aid, ActivityProductID: &apid,
		Channel: model.HomeCarouselChannelDeal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Channel != model.HomeCarouselChannelGroup {
		t.Fatalf("stored/displayed channel=%s want group", view.Channel)
	}
	_, err = svc.Create(HomeCarouselCreateInput{
		ProductID: p.ID, ActivityID: &aid, ActivityProductID: &apid,
		Channel: model.HomeCarouselChannelGroup,
	})
	if !errors.Is(err, ErrHomeCarouselDupProduct) {
		t.Fatalf("expected dup, got %v", err)
	}
}
