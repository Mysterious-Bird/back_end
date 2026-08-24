package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupHomeFeaturedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.HomeFeatured{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestHomeFeaturedCountDup_ActivityThenPlainDealChannel(t *testing.T) {
	db := setupHomeFeaturedTestDB(t)
	apID := uint64(7)
	actID := uint64(1)
	pid := uint64(11)
	row := model.HomeFeatured{
		Section: model.HomeFeaturedSectionDeal, ItemType: model.HomeFeaturedTypePinned,
		ProductID: &pid, ActivityID: &actID, ActivityProductID: &apID,
		Channel: model.HomeCarouselChannelGroup, Status: model.HomeFeaturedStatusOn, SortOrder: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := &HomeFeaturedService{DB: db}
	n, err := svc.countDup(model.HomeFeaturedSectionDeal, model.HomeFeaturedTypePinned, pid, 0, nil, model.HomeCarouselChannelDeal)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("activity row should not block plain deal channel dup check, got %d", n)
	}
}

func TestHomeFeaturedCountDup_SameActivityProductAnyChannel(t *testing.T) {
	db := setupHomeFeaturedTestDB(t)
	apID := uint64(7)
	actID := uint64(1)
	pid := uint64(11)
	row := model.HomeFeatured{
		Section: model.HomeFeaturedSectionDeal, ItemType: model.HomeFeaturedTypePinned,
		ProductID: &pid, ActivityID: &actID, ActivityProductID: &apID,
		Channel: model.HomeCarouselChannelGroup, Status: model.HomeFeaturedStatusOn, SortOrder: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := &HomeFeaturedService{DB: db}
	n, err := svc.countDup(model.HomeFeaturedSectionDeal, model.HomeFeaturedTypePinned, pid, 0, &apID, model.HomeCarouselChannelDeal)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("same AP different channel should count as dup, got %d", n)
	}
}

func TestHomeFeaturedCountDup_DifferentActivityProducts(t *testing.T) {
	db := setupHomeFeaturedTestDB(t)
	ap1 := uint64(7)
	ap2 := uint64(8)
	actID := uint64(1)
	pid := uint64(11)
	row := model.HomeFeatured{
		Section: model.HomeFeaturedSectionDeal, ItemType: model.HomeFeaturedTypePinned,
		ProductID: &pid, ActivityID: &actID, ActivityProductID: &ap1,
		Channel: model.HomeCarouselChannelGroup, Status: model.HomeFeaturedStatusOn, SortOrder: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := &HomeFeaturedService{DB: db}
	n, err := svc.countDup(model.HomeFeaturedSectionDeal, model.HomeFeaturedTypePinned, pid, 0, &ap2, model.HomeCarouselChannelDeal)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("different AP on same product should be allowed, got %d", n)
	}
}
