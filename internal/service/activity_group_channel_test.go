package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestBuildActivityProductStoreViewGroupOnlyDisablesDeal(t *testing.T) {
	price := 9.9
	target := uint32(3)
	ap := &model.ActivityProduct{
		EnableGroupBuy:      1,
		ActivityPrice:       9.9,
		GroupBuyPrice:       &price,
		GroupBuyTargetCount: &target,
		EnableCoupon:        1,
		ActivityStock:       10,
	}
	act := &model.Activity{EnableCoupon: 1}
	p := &model.Product{ID: 1, Name: "movie", Price: 20, DealStock: 10, EnableDeal: 1}

	view := buildActivityProductStoreView(act, ap, p)
	if view.SaleOptions.Deal.Available {
		t.Fatal("拼团活动商品不应再提供直购通道")
	}
	if !view.CanGroupBuy {
		t.Fatal("expected can_group_buy")
	}
	if !view.SaleOptions.Group.Available {
		t.Fatal("expected group channel available")
	}
}

func TestBuildActivityProductStoreViewDirectKeepsDeal(t *testing.T) {
	ap := &model.ActivityProduct{
		EnableGroupBuy: 0,
		ActivityPrice:  9.9,
		EnableCoupon:   1,
		ActivityStock:  10,
	}
	act := &model.Activity{EnableCoupon: 1}
	p := &model.Product{ID: 1, Name: "movie", Price: 20, DealStock: 10, EnableDeal: 1}

	view := buildActivityProductStoreView(act, ap, p)
	if !view.SaleOptions.Deal.Available {
		t.Fatal("直购活动商品应保留直购通道")
	}
	if view.CanGroupBuy {
		t.Fatal("direct activity product must not be group")
	}
}

func TestCarouselChannelFollowsActivityProductKind(t *testing.T) {
	price := 1.0
	target := uint32(3)
	groupAP := model.ActivityProduct{
		EnableGroupBuy:      1,
		GroupBuyPrice:       &price,
		GroupBuyTargetCount: &target,
	}
	directAP := model.ActivityProduct{EnableGroupBuy: 0, ActivityPrice: 9.9}

	if got := carouselChannelForActivityProduct(groupAP); got != model.HomeCarouselChannelGroup {
		t.Fatalf("group AP channel=%s want group", got)
	}
	if got := carouselChannelForActivityProduct(directAP); got != model.HomeCarouselChannelDeal {
		t.Fatalf("direct AP channel=%s want deal", got)
	}
}
