package service

import (
	"errors"
	"strings"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestValidateGroupBuyOrderInput(t *testing.T) {
	teamID := uint64(99)

	tests := []struct {
		name         string
		quantity     uint32
		groupBuyTeam *uint64
		startNewTeam bool
		wantErr      string
	}{
		{"qty ok join team", 1, &teamID, false, ""},
		{"qty ok start new", 1, nil, true, ""},
		{"qty too many", 2, nil, true, "拼团每次只能购买 1 件"},
		{"neither team nor new", 1, nil, false, "请选择拼团或开新团"},
		{"both team and new", 1, &teamID, true, "不能同时指定参团与开新团"},
		{"zero team id treated as absent", 1, func() *uint64 { v := uint64(0); return &v }(), false, "请选择拼团或开新团"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGroupBuyOrderInput(tc.quantity, tc.groupBuyTeam, tc.startNewTeam)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrGroupBuyInvalid) {
				t.Fatalf("expected ErrGroupBuyInvalid, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected message containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAssertActivityGroupBuyOnly(t *testing.T) {
	enabled := &ActivityOrderContext{
		ActivityProduct: &model.ActivityProduct{EnableGroupBuy: 1},
	}
	disabled := &ActivityOrderContext{
		ActivityProduct: &model.ActivityProduct{EnableGroupBuy: 0},
	}

	tests := []struct {
		name         string
		purchaseType uint8
		actCtx       *ActivityOrderContext
		wantErr      bool
	}{
		{"group allowed", model.PurchaseTypeGroup, enabled, false},
		{"solo blocked when group AP", model.PurchaseTypeSolo, enabled, true},
		{"solo ok when not group", model.PurchaseTypeSolo, disabled, false},
		{"group blocked when deal AP", model.PurchaseTypeGroup, disabled, true},
		{"nil context ok", model.PurchaseTypeSolo, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertActivityGroupBuyOnly(tc.purchaseType, tc.actCtx)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, ErrGroupBuyInvalid) {
					t.Fatalf("expected ErrGroupBuyInvalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateTeamJoinLimit(t *testing.T) {
	tests := []struct {
		name        string
		existing    int64
		allowRepeat uint8
		maxJoins    uint32
		wantErr     bool
	}{
		{"off first join ok", 0, 0, 1, false},
		{"off second join reject", 1, 0, 1, true},
		{"on unlimited ok", 5, 1, 0, false},
		{"on under max ok", 1, 1, 3, false},
		{"on at max reject", 3, 1, 3, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTeamJoinLimit(tc.existing, tc.allowRepeat, tc.maxJoins)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGroupCompleteReady(t *testing.T) {
	tests := []struct {
		name     string
		current  uint32
		target   uint32
		distinct uint32
		allow    uint8
		want     bool
	}{
		{"off needs distinct", 3, 3, 1, 0, false},
		{"off distinct ok", 3, 3, 3, 0, true},
		{"on count enough same user", 3, 3, 1, 1, true},
		{"on count short", 2, 3, 2, 1, false},
		{"below count", 1, 3, 1, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := groupCompleteReady(tc.current, tc.target, tc.distinct, tc.allow)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestResolveGroupBuyRepeat(t *testing.T) {
	p := model.Product{GroupBuyAllowRepeat: 1}
	if a, m := resolveGroupBuyRepeat(p, nil); a != 1 || m != 0 {
		t.Fatalf("product: allow=%d max=%d", a, m)
	}
	act := &ActivityGroupBuyConfig{GroupBuyAllowRepeat: 0, GroupBuyMaxJoinsPerUser: 2}
	if a, m := resolveGroupBuyRepeat(p, act); a != 0 || m != 2 {
		t.Fatalf("act overrides: allow=%d max=%d", a, m)
	}
}
