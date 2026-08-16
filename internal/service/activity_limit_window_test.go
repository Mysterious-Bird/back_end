package service

import (
	"slices"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestCalendarWindowAt_Day_WithRefresh(t *testing.T) {
	loc := time.Local
	// Wed 2026-07-30 13:00, refresh 12:00 → [today 12:00, tomorrow 12:00)
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	start, end := calendarWindowAt(now, "day", "12:00:00")

	wantStart := time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 31, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("day window after refresh: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Same day but before refresh → previous day window
	now = time.Date(2026, 7, 30, 11, 0, 0, 0, loc)
	start, end = calendarWindowAt(now, "day", "12:00:00")
	wantStart = time.Date(2026, 7, 29, 12, 0, 0, 0, loc)
	wantEnd = time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("day window before refresh: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowAt_Week_MondayRefresh(t *testing.T) {
	loc := time.Local
	// Tue 2026-07-21 13:00, refresh 12:00 → week [Mon 07-20 12:00, Mon 07-27 12:00)
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, loc)
	start, end := calendarWindowAt(now, "week", "12:00:00")

	wantStart := time.Date(2026, 7, 20, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 27, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Tue): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Monday before refresh → previous week
	mon := time.Date(2026, 7, 20, 11, 0, 0, 0, loc)
	start, end = calendarWindowAt(mon, "week", "12:00:00")
	wantStart = time.Date(2026, 7, 13, 12, 0, 0, 0, loc)
	wantEnd = time.Date(2026, 7, 20, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Mon before refresh): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowAt_Month_FirstDayRefresh(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, loc)
	start, end := calendarWindowAt(now, "month", "10:00:00")

	wantStart := time.Date(2026, 7, 1, 10, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 8, 1, 10, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("month window: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Dec 1 before refresh → November window
	dec := time.Date(2026, 12, 1, 9, 0, 0, 0, loc)
	start, end = calendarWindowAt(dec, "month", "10:00:00")
	wantStart = time.Date(2026, 11, 1, 10, 0, 0, 0, loc)
	wantEnd = time.Date(2026, 12, 1, 10, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("month window (before refresh on 1st): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowAt_InvalidRefreshFallsBackMidnight(t *testing.T) {
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, time.Local)
	start, end := calendarWindowAt(now, "day", "invalid")
	wantStart := time.Date(2026, 7, 21, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2026, 7, 22, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("invalid refresh fallback: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestSumBoughtQtyExcludesGroupFailed(t *testing.T) {
	need := []int{
		int(model.OrderStatusCancelled),
		int(model.OrderStatusGroupFailed),
		int(model.OrderStatusRefunded),
		int(model.OrderStatusClosed),
	}
	for _, st := range need {
		if !slices.Contains(orderStatusesExcludedFromBoughtQty, st) {
			t.Fatalf("excluded statuses missing %d, got %v", st, orderStatusesExcludedFromBoughtQty)
		}
	}
	if len(orderStatusesExcludedFromBoughtQty) != len(need) {
		t.Fatalf("expected %d excluded statuses, got %v", len(need), orderStatusesExcludedFromBoughtQty)
	}
}

func TestOrderStatusesExcludedAreIntSlice(t *testing.T) {
	// Regression: []uint8 is bound by GORM as a single []byte → MySQL 1064 near '?'.
	var v any = orderStatusesExcludedFromBoughtQty
	if _, ok := v.([]int); !ok {
		t.Fatalf("orderStatusesExcludedFromBoughtQty must be []int for GORM IN clause, got %T", v)
	}
}

func TestClampDayOfMonth(t *testing.T) {
	cases := []struct {
		name  string
		year  int
		month time.Month
		day   int
		want  int
	}{
		{"normal", 2026, 7, 15, 15},
		{"jan-31", 2026, 1, 31, 31},
		{"feb-31-leap", 2024, 2, 31, 29},  // 2024 is leap
		{"feb-31-nonleap", 2026, 2, 31, 28},
		{"apr-31", 2026, 4, 31, 30},
		{"day-0-clamped-to-1", 2026, 5, 0, 1},
		{"day-negative-clamped-to-1", 2026, 5, -3, 1},
		{"day-32-clamped-to-month", 2026, 6, 32, 30},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := clampDayOfMonth(c.year, c.month, c.day)
			if got != c.want {
				t.Fatalf("clampDayOfMonth(%d,%d,%d)=%d want %d", c.year, c.month, c.day, got, c.want)
			}
		})
	}
}

func TestCalendarWindowFor_Week_WednesdayNoon(t *testing.T) {
	loc := time.Local
	cfg := limitRefreshConfig{WeeklyWeekday: 3, WeeklyTime: "12:00:00"} // Wed
	// Thu 2026-07-23 13:00 → [Wed 07-22 12:00, Wed 07-29 12:00)
	now := time.Date(2026, 7, 23, 13, 0, 0, 0, loc)
	start, end := calendarWindowFor(now, "week", cfg)
	wantStart := time.Date(2026, 7, 22, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 29, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("got [%v,%v) want [%v,%v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowFor_Week_WednesdayBeforeRefresh(t *testing.T) {
	loc := time.Local
	cfg := limitRefreshConfig{WeeklyWeekday: 3, WeeklyTime: "12:00:00"} // Wed
	// Wed 2026-07-22 11:00 (before refresh) → previous week [Wed 07-15 12:00, Wed 07-22 12:00)
	now := time.Date(2026, 7, 22, 11, 0, 0, 0, loc)
	start, end := calendarWindowFor(now, "week", cfg)
	wantStart := time.Date(2026, 7, 15, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 22, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("got [%v,%v) want [%v,%v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowFor_Month_Day31_FebruaryClamp(t *testing.T) {
	loc := time.Local
	cfg := limitRefreshConfig{MonthlyDay: 31, MonthlyTime: "10:00:00"}
	now := time.Date(2026, 2, 15, 12, 0, 0, 0, loc)
	start, end := calendarWindowFor(now, "month", cfg)
	wantStart := time.Date(2026, 1, 31, 10, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 2, 28, 10, 0, 0, 0, loc) // 2026 not leap
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("got [%v,%v) want [%v,%v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowFor_Month_Day31_AfterAnchor(t *testing.T) {
	loc := time.Local
	cfg := limitRefreshConfig{MonthlyDay: 31, MonthlyTime: "10:00:00"}
	// Mar 31 11:00 (after Mar 31 10:00 anchor, clamped to Mar 31) → [Mar 31 10:00, Apr 30 10:00)
	now := time.Date(2026, 3, 31, 11, 0, 0, 0, loc)
	start, end := calendarWindowFor(now, "month", cfg)
	wantStart := time.Date(2026, 3, 31, 10, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 4, 30, 10, 0, 0, 0, loc) // Apr has 30 days
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("got [%v,%v) want [%v,%v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowFor_Day_UsesDailyTime(t *testing.T) {
	loc := time.Local
	cfg := limitRefreshConfig{DailyTime: "12:00:00"}
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	start, end := calendarWindowFor(now, "day", cfg)
	wantStart := time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 31, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("got [%v,%v) want [%v,%v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowFor_UnknownUnitReturnsZero(t *testing.T) {
	loc := time.Local
	cfg := limitRefreshConfig{DailyTime: "12:00:00"}
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	start, end := calendarWindowFor(now, "year", cfg)
	if !start.IsZero() || !end.IsZero() {
		t.Fatalf("unknown unit: got [%v,%v) want zero", start, end)
	}
}

func TestLimitRefreshFromProduct_Nil(t *testing.T) {
	cfg := limitRefreshFromProduct(nil)
	if cfg.DailyTime != "00:00:00" || cfg.WeeklyWeekday != 1 || cfg.WeeklyTime != "00:00:00" ||
		cfg.MonthlyDay != 1 || cfg.MonthlyTime != "00:00:00" {
		t.Fatalf("nil product cfg should be all defaults, got %+v", cfg)
	}
}

func TestLimitRefreshFromProduct_PassesThrough(t *testing.T) {
	ap := &model.ActivityProduct{
		DailyRefreshTime:     "09:00:00",
		WeeklyRefreshWeekday: 3,
		WeeklyRefreshTime:    "12:30:00",
		MonthlyRefreshDay:    15,
		MonthlyRefreshTime:   "08:00:00",
	}
	cfg := limitRefreshFromProduct(ap)
	if cfg.DailyTime != "09:00:00" || cfg.WeeklyWeekday != 3 || cfg.WeeklyTime != "12:30:00" ||
		cfg.MonthlyDay != 15 || cfg.MonthlyTime != "08:00:00" {
		t.Fatalf("cfg mismatch: got %+v", cfg)
	}
}

func TestLimitRefreshFromProduct_ClampsInvalidWeekdayAndDay(t *testing.T) {
	ap := &model.ActivityProduct{
		WeeklyRefreshWeekday: 9,
		MonthlyRefreshDay:    40,
	}
	cfg := limitRefreshFromProduct(ap)
	if cfg.WeeklyWeekday != 1 {
		t.Fatalf("invalid weekday should clamp to 1, got %d", cfg.WeeklyWeekday)
	}
	if cfg.MonthlyDay != 1 {
		t.Fatalf("invalid monthly day should clamp to 1, got %d", cfg.MonthlyDay)
	}
}
