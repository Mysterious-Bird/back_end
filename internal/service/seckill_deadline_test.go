package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestSeckillDeadlineAt_IncludesPlatformDaily(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)}
	ap := &model.ActivityProduct{PlatformDailyMax: 100, DailyRefreshTime: "18:00:00"}
	got := seckillDeadlineAt(act, ap, time.Time{}, false, now)
	want := time.Date(2026, 7, 30, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSeckillDeadlineAt_DailyUsesRefreshNotMidnight(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)}
	ap := &model.ActivityProduct{DailyMax: 3, DailyRefreshTime: "15:00:00"}
	got := seckillDeadlineAt(act, ap, time.Time{}, false, now)
	want := time.Date(2026, 7, 30, 15, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSeckillDeadlineAt_WeeklyUsesWeekdayAndTime(t *testing.T) {
	loc := time.Local
	// Thu 2026-07-23 13:00, weekly Wed 12:00 → next end Wed 2026-07-29 12:00
	now := time.Date(2026, 7, 23, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)}
	ap := &model.ActivityProduct{WeeklyMax: 5, WeeklyRefreshWeekday: 3, WeeklyRefreshTime: "12:00:00"}
	got := seckillDeadlineAt(act, ap, time.Time{}, false, now)
	want := time.Date(2026, 7, 29, 12, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSeckillDeadlineAt_MonthlyUsesDayAndTime(t *testing.T) {
	loc := time.Local
	// Jul 21 13:00, monthly day 15 10:00 → next end Aug 15 10:00 (activity ends after that)
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 9, 10, 0, 0, 0, 0, loc)}
	ap := &model.ActivityProduct{MonthlyMax: 5, MonthlyRefreshDay: 15, MonthlyRefreshTime: "10:00:00"}
	got := seckillDeadlineAt(act, ap, time.Time{}, false, now)
	want := time.Date(2026, 8, 15, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSeckillDeadlineAt_RegisterDeadlineWhenLoggedIn(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)}
	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, loc)
	ap := &model.ActivityProduct{RegisterHours: 2, RegisterMax: 1}
	got := seckillDeadlineAt(act, ap, createdAt, true, now)
	want := createdAt.Add(2 * time.Hour) // 2026-07-30 12:00
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSeckillDeadlineAt_RegisterIgnoredWhenNotLoggedIn(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)}
	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, loc)
	ap := &model.ActivityProduct{RegisterHours: 2, RegisterMax: 1}
	got := seckillDeadlineAt(act, ap, createdAt, false, now)
	if !got.Equal(act.EndAt) {
		t.Fatalf("not logged in: deadline should be act.EndAt, got %v want %v", got, act.EndAt)
	}
}

func TestSeckillDeadlineAt_PicksEarliestAcrossDimensions(t *testing.T) {
	loc := time.Local
	// Thu 2026-07-30 13:00. Daily 15:00 → 15:00 today. Platform 18:00. Weekly Mon 09:00 → Aug 03 09:00.
	// Monthly day 1 00:00 → Aug 01 00:00. Activity end Aug 10. Earliest = today 15:00.
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	act := &model.Activity{EndAt: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)}
	ap := &model.ActivityProduct{
		DailyMax: 3, WeeklyMax: 5, MonthlyMax: 10, PlatformDailyMax: 100,
		DailyRefreshTime:     "15:00:00",
		WeeklyRefreshWeekday: 1,
		WeeklyRefreshTime:    "09:00:00",
		MonthlyRefreshDay:    1,
		MonthlyRefreshTime:   "00:00:00",
	}
	got := seckillDeadlineAt(act, ap, time.Time{}, false, now)
	want := time.Date(2026, 7, 30, 15, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
