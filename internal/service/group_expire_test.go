package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestComputeGroupExpireAt(t *testing.T) {
	loc := time.Local

	t.Run("no limits", func(t *testing.T) {
		now := time.Date(2026, 7, 30, 10, 0, 0, 0, loc)
		got := computeGroupExpireAt(now, nil)
		want := now.Add(24 * time.Hour)
		if !got.Equal(want) {
			t.Fatalf("nil ap: got %v, want %v", got, want)
		}

		ap := &model.ActivityProduct{}
		got = computeGroupExpireAt(now, ap)
		if !got.Equal(want) {
			t.Fatalf("zero limits: got %v, want %v", got, want)
		}
	})

	t.Run("daily refresh in 2h", func(t *testing.T) {
		// Wed 10:00, refresh 12:00 → next refresh in 2h
		now := time.Date(2026, 7, 30, 10, 0, 0, 0, loc)
		ap := &model.ActivityProduct{DailyMax: 3, DailyRefreshTime: "12:00:00"}
		got := computeGroupExpireAt(now, ap)
		want := time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v (not now+24h)", got, want)
		}
		if got.Equal(now.Add(24*time.Hour)) {
			t.Fatal("expire should be refresh point, not +24h")
		}
	})

	t.Run("refresh beyond 24h cap", func(t *testing.T) {
		// Wed 13:00, weekly limit → next refresh Mon ~5d away; cap at now+24h
		now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
		ap := &model.ActivityProduct{WeeklyMax: 5, DailyRefreshTime: "12:00:00"}
		got := computeGroupExpireAt(now, ap)
		want := now.Add(24 * time.Hour)
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func TestNextLimitRefreshAt(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, loc)

	at, ok := nextLimitRefreshAt(now, nil)
	if ok || !at.IsZero() {
		t.Fatalf("nil ap: got (%v, %v), want (zero, false)", at, ok)
	}

	ap := &model.ActivityProduct{DailyMax: 1, DailyRefreshTime: "12:00:00"}
	at, ok = nextLimitRefreshAt(now, ap)
	if !ok {
		t.Fatal("expected refresh with daily_max")
	}
	want := time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
	if !at.Equal(want) {
		t.Fatalf("got %v, want %v", at, want)
	}

	// platform daily participates with same day boundary
	ap = &model.ActivityProduct{PlatformDailyMax: 10, DailyRefreshTime: "12:00:00"}
	at, ok = nextLimitRefreshAt(now, ap)
	if !ok || !at.Equal(want) {
		t.Fatalf("platform daily: got (%v, %v), want (%v, true)", at, ok, want)
	}

	// earliest among multiple dimensions
	ap = &model.ActivityProduct{
		DailyMax: 1, WeeklyMax: 5, MonthlyMax: 10,
		DailyRefreshTime: "12:00:00",
	}
	at, ok = nextLimitRefreshAt(now, ap)
	if !ok || !at.Equal(want) {
		t.Fatalf("min of dims: got (%v, %v), want (%v, true)", at, ok, want)
	}
}

func TestNextLimitRefreshAt_WeeklyUsesWeekday(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, loc) // Tue
	ap := &model.ActivityProduct{WeeklyMax: 5, WeeklyRefreshWeekday: 3, WeeklyRefreshTime: "12:00:00"}
	got, ok := nextLimitRefreshAt(now, ap)
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 7, 22, 12, 0, 0, 0, loc) // Wed
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNextLimitRefreshAt_MonthlyUsesMonthlyDayAndTime(t *testing.T) {
	loc := time.Local
	// Jul 21 13:00, monthly anchor on day 15 at 10:00 → next refresh Aug 15 10:00
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, loc)
	ap := &model.ActivityProduct{MonthlyMax: 5, MonthlyRefreshDay: 15, MonthlyRefreshTime: "10:00:00"}
	got, ok := nextLimitRefreshAt(now, ap)
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 8, 15, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
