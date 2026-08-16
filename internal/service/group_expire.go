package service

import (
	"time"

	"yujixinjiang/backend/internal/model"
)

const groupTeamMaxLifetime = 24 * time.Hour

// nextLimitRefreshAt returns the earliest strict-future limit refresh among
// enabled activity product dimensions (daily / weekly / monthly / platform daily).
// Each dimension uses its own per-dimension refresh clock via calendarWindowFor:
// day/platform use DailyTime; week uses (WeeklyWeekday, WeeklyTime);
// month uses (MonthlyDay, MonthlyTime). ok is false when ap is nil or no
// limit dimension is enabled.
func nextLimitRefreshAt(now time.Time, ap *model.ActivityProduct) (time.Time, bool) {
	if ap == nil {
		return time.Time{}, false
	}

	cfg := limitRefreshFromProduct(ap)
	var earliest time.Time
	found := false

	consider := func(unit string, max uint32) {
		if max == 0 {
			return
		}
		_, end := calendarWindowFor(now, unit, cfg)
		if end.IsZero() {
			return
		}
		if !found || end.Before(earliest) {
			earliest = end
			found = true
		}
	}

	consider("day", ap.DailyMax)
	consider("week", ap.WeeklyMax)
	consider("month", ap.MonthlyMax)
	consider("day", ap.PlatformDailyMax) // platform daily shares cfg.DailyTime

	return earliest, found
}

// computeGroupExpireAt sets new team expire_at to min(now+24h, nextLimitRefreshAt).
// Non-activity or no enabled limits → now+24h only.
func computeGroupExpireAt(now time.Time, ap *model.ActivityProduct) time.Time {
	cap := now.Add(groupTeamMaxLifetime)
	refresh, ok := nextLimitRefreshAt(now, ap)
	if !ok || refresh.After(cap) {
		return cap
	}
	return refresh
}
