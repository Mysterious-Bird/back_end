package service

import (
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

// orderStatusesExcludedFromBoughtQty 不计入用户限购已购件数的终态订单。
// 必须用 []int：GORM 会把 []uint8 当成 []byte 绑成 '<binary>'，触发 MySQL 1064。
// 已全额退款/关闭的订单与取消一样释放限购额度（与活动 sold_count 回滚一致）。
var orderStatusesExcludedFromBoughtQty = []int{
	int(model.OrderStatusCancelled),
	int(model.OrderStatusGroupFailed),
	int(model.OrderStatusRefunded),
	int(model.OrderStatusClosed),
}

// refreshInstantOnDate returns the refresh clock on calendar date d (local).
func refreshInstantOnDate(d time.Time, refreshTime string) time.Time {
	rt, err := NormalizeDailyRefreshTime(refreshTime)
	if err != nil {
		rt = "00:00:00"
	}
	parsed, _ := time.Parse("15:04:05", rt)
	y, m, day := d.Date()
	loc := d.Location()
	return time.Date(y, m, day, parsed.Hour(), parsed.Minute(), parsed.Second(), 0, loc)
}

// parseClockParts parses "HH:MM:SS" into hour/min/sec; invalid → 00:00:00.
func parseClockParts(s string) (h, m, sec int) {
	rt, err := NormalizeDailyRefreshTime(s)
	if err != nil {
		rt = "00:00:00"
	}
	parsed, _ := time.Parse("15:04:05", rt)
	return parsed.Hour(), parsed.Minute(), parsed.Second()
}

// limitRefreshConfig carries per-product refresh clock config for day/week/month
// windows. Day uses DailyTime; week uses (WeeklyWeekday, WeeklyTime);
// month uses (MonthlyDay, MonthlyTime) with end-of-month clamping.
type limitRefreshConfig struct {
	DailyTime     string
	WeeklyWeekday uint8 // 1=Mon..7=Sun
	WeeklyTime    string
	MonthlyDay    uint8 // 1-31
	MonthlyTime   string
}

// limitRefreshFromProduct builds a limitRefreshConfig from an ActivityProduct,
// applying sane defaults/clamps for missing or out-of-range values.
func limitRefreshFromProduct(ap *model.ActivityProduct) limitRefreshConfig {
	if ap == nil {
		return limitRefreshConfig{
			DailyTime: "00:00:00", WeeklyWeekday: 1, WeeklyTime: "00:00:00",
			MonthlyDay: 1, MonthlyTime: "00:00:00",
		}
	}
	wd := ap.WeeklyRefreshWeekday
	if wd < 1 || wd > 7 {
		wd = 1
	}
	md := ap.MonthlyRefreshDay
	if md < 1 || md > 31 {
		md = 1
	}
	return limitRefreshConfig{
		DailyTime:     ap.DailyRefreshTime,
		WeeklyWeekday: wd,
		WeeklyTime:    ap.WeeklyRefreshTime,
		MonthlyDay:    md,
		MonthlyTime:   ap.MonthlyRefreshTime,
	}
}

// clampDayOfMonth returns day clamped to [1, lastDayOfMonth]. day<1 → 1;
// day>lastDay → lastDay. Handles leap years via Go's date normalization.
func clampDayOfMonth(year int, month time.Month, day int) int {
	if day < 1 {
		return 1
	}
	// Last day of month = day 1 of next month minus 1 day.
	firstNext := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	lastDay := firstNext.AddDate(0, 0, -1).Day()
	if day > lastDay {
		return lastDay
	}
	return day
}

// monthAnchor returns the clamped refresh anchor for (year, month) using the
// configured MonthlyDay + MonthlyTime in loc.
func monthAnchor(year int, month time.Month, cfg limitRefreshConfig, loc *time.Location) time.Time {
	day := clampDayOfMonth(year, month, int(cfg.MonthlyDay))
	h, mi, s := parseClockParts(cfg.MonthlyTime)
	return time.Date(year, month, day, h, mi, s, 0, loc)
}

// calendarWindowFor returns a half-open period [start, end) for unit
// "day" | "week" | "month" using the per-product refresh config cfg.
// Day uses cfg.DailyTime; week uses (cfg.WeeklyWeekday, cfg.WeeklyTime);
// month uses (cfg.MonthlyDay, cfg.MonthlyTime) with end-of-month clamping.
// Unknown unit returns zero times.
func calendarWindowFor(now time.Time, unit string, cfg limitRefreshConfig) (start, end time.Time) {
	loc := now.Location()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)

	switch unit {
	case "day":
		refreshToday := refreshInstantOnDate(midnight, cfg.DailyTime)
		start = refreshToday
		if now.Before(refreshToday) {
			start = refreshToday.AddDate(0, 0, -1)
		}
		return start, start.AddDate(0, 0, 1)
	case "week":
		// ISO weekday: Mon=1..Sun=7. Go Weekday: Sun=0..Sat=6.
		isoToday := ((int(now.Weekday()) + 6) % 7) + 1 // Mon=1..Sun=7
		offsetFromToday := int(cfg.WeeklyWeekday) - isoToday
		anchorDate := midnight.AddDate(0, 0, offsetFromToday)
		h, mi, s := parseClockParts(cfg.WeeklyTime)
		anchor := time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), h, mi, s, 0, loc)
		start = anchor
		if now.Before(anchor) {
			start = anchor.AddDate(0, 0, -7)
		}
		return start, start.AddDate(0, 0, 7)
	case "month":
		anchor := monthAnchor(y, m, cfg, loc)
		if now.Before(anchor) {
			// Previous month's clamped anchor → this month's clamped anchor.
			prevFirst := time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, -1, 0)
			start = monthAnchor(prevFirst.Year(), prevFirst.Month(), cfg, loc)
			end = anchor
		} else {
			// This month's clamped anchor → next month's clamped anchor.
			nextFirst := time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
			start = anchor
			end = monthAnchor(nextFirst.Year(), nextFirst.Month(), cfg, loc)
		}
		return start, end
	default:
		return time.Time{}, time.Time{}
	}
}

// calendarWindowAt returns a half-open period [start, end) for unit
// "day" | "week" | "month" aligned to refreshTime.
//
// Day uses refreshTime directly. Week/month delegate to calendarWindowFor
// with weekday=1 (Monday) / day=1, both at refreshTime — i.e. week starts
// Monday at refresh; month starts on the 1st at refresh. This preserves the
// legacy Monday/first-of-month semantics for callers that have not migrated
// to per-product refresh config. New week/month callers should use
// calendarWindowFor with a limitRefreshConfig from limitRefreshFromProduct.
// Invalid refreshTime falls back to 00:00:00. Unknown unit returns zero times.
func calendarWindowAt(now time.Time, unit string, refreshTime string) (start, end time.Time) {
	cfg := limitRefreshConfig{
		DailyTime:     refreshTime,
		WeeklyWeekday: 1,
		WeeklyTime:    refreshTime,
		MonthlyDay:    1,
		MonthlyTime:   refreshTime,
	}
	return calendarWindowFor(now, unit, cfg)
}

// calendarWindow is midnight-based; prefer calendarWindowAt with DailyRefreshTime.
func calendarWindow(now time.Time, unit string) (start, end time.Time) {
	return calendarWindowAt(now, unit, "00:00:00")
}

// registerDeadline is createdAt + hours (hours=0 → createdAt).
func registerDeadline(createdAt time.Time, hours uint32) time.Time {
	return createdAt.Add(time.Duration(hours) * time.Hour)
}

// inRegisterWindow is true iff now ∈ [createdAt, registerDeadline).
// hours=0 yields an empty window.
func inRegisterWindow(createdAt, now time.Time, hours uint32) bool {
	deadline := registerDeadline(createdAt, hours)
	return !now.Before(createdAt) && now.Before(deadline)
}

// sumBoughtQtyInWindow 统计账号对该活动商品的已购件数（排除取消与拼团失败）。
// start/end 均为非零时限制 o.created_at ∈ [start, end)；否则不限时间。
func sumBoughtQtyInWindow(db *gorm.DB, accountID, activityProductID uint64, start, end time.Time) (uint32, error) {
	q := db.Table("order_item oi").
		Select("COALESCE(SUM(oi.quantity), 0)").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.activity_product_id = ? AND oi.is_deleted = ?", accountID, activityProductID, model.NotDeleted).
		Where("o.status NOT IN ?", orderStatusesExcludedFromBoughtQty)
	if !start.IsZero() && !end.IsZero() {
		q = q.Where("o.created_at >= ? AND o.created_at < ?", start, end)
	}
	var bought uint32
	err := q.Scan(&bought).Error
	return bought, err
}

// sumBoughtQty 全程已购件数。
func sumBoughtQty(db *gorm.DB, accountID, activityProductID uint64) (uint32, error) {
	return sumBoughtQtyInWindow(db, accountID, activityProductID, time.Time{}, time.Time{})
}

// sumBoughtQtyByActivity 统计账号在整场活动下已购件数（跨活动商品，排除取消/拼团失败等）。
func sumBoughtQtyByActivity(db *gorm.DB, accountID, activityID uint64) (uint32, error) {
	return sumBoughtQtyByActivityInWindow(db, accountID, activityID, time.Time{}, time.Time{})
}

func sumBoughtQtyByActivityInWindow(db *gorm.DB, accountID, activityID uint64, start, end time.Time) (uint32, error) {
	if db == nil || accountID == 0 || activityID == 0 {
		return 0, nil
	}
	q := db.Table("order_item oi").
		Select("COALESCE(SUM(oi.quantity), 0)").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.activity_id = ? AND oi.is_deleted = ?", accountID, activityID, model.NotDeleted).
		Where("o.status NOT IN ?", orderStatusesExcludedFromBoughtQty)
	if !start.IsZero() && !end.IsZero() {
		q = q.Where("o.created_at >= ? AND o.created_at < ?", start, end)
	}
	var bought uint32
	err := q.Scan(&bought).Error
	return bought, err
}

func loadActivityUserMaxQty(db *gorm.DB, activityID uint64) (uint32, error) {
	cfg, err := loadActivityUserLimitConfig(db, activityID)
	return cfg.UserMaxQty, err
}

type activityUserLimitConfig struct {
	UserMaxQty           uint32 `gorm:"column:user_max_qty"`
	UserDailyMax         uint32 `gorm:"column:user_daily_max"`
	UserDailyRefreshTime string `gorm:"column:user_daily_refresh_time"`
}

func loadActivityUserLimitConfig(db *gorm.DB, activityID uint64) (activityUserLimitConfig, error) {
	var cfg activityUserLimitConfig
	if db == nil || activityID == 0 {
		return cfg, nil
	}
	err := db.Model(&model.Activity{}).
		Select("user_max_qty", "user_daily_max", "user_daily_refresh_time").
		Where("id = ? AND is_deleted = ?", activityID, model.NotDeleted).
		Scan(&cfg).Error
	return cfg, err
}

type activityRemainResult struct {
	RemainingQty uint32
	LimitReached bool
	LimitReason  string
}

func minU32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// computeActivityRemaining 本单最多可买件数 = 库存与各限购剩余的最小值。
// 日/周/月/全程/新用户窗与每人限购一律按「已购件数」累计（非订单笔数）。
// 例：每日限购 3 + 每人 10 → 当天最多共买 3 件；首单买满 3 后当天不可再买。
func computeActivityRemaining(
	db *gorm.DB,
	ap *model.ActivityProduct,
	stock uint32,
	accountID *uint64,
	accountCreatedAt time.Time,
	now time.Time,
) (activityRemainResult, error) {
	out := activityRemainResult{RemainingQty: stock}
	tighten := func(n uint32, reason string) {
		if n == 0 {
			out.RemainingQty = 0
			out.LimitReached = true
			if out.LimitReason == "" {
				out.LimitReason = reason
			}
			return
		}
		out.RemainingQty = minU32(out.RemainingQty, n)
	}

	if ap.PerUserMaxQty > 0 {
		tighten(ap.PerUserMaxQty, "per_user_qty")
	}

	actLimits, err := loadActivityUserLimitConfig(db, ap.ActivityID)
	if err != nil {
		return out, err
	}
	if actLimits.UserMaxQty > 0 {
		tighten(actLimits.UserMaxQty, "activity_user_max")
	}
	if actLimits.UserDailyMax > 0 {
		tighten(actLimits.UserDailyMax, "activity_user_daily")
	}
	if ap.DailyMax > 0 {
		tighten(ap.DailyMax, "daily")
	}
	if ap.WeeklyMax > 0 {
		tighten(ap.WeeklyMax, "weekly")
	}
	if ap.MonthlyMax > 0 {
		tighten(ap.MonthlyMax, "monthly")
	}
	activityMax := ap.ActivityMax
	if activityMax == 0 && ap.PerUserMaxOrders > 0 {
		activityMax = ap.PerUserMaxOrders
	}
	if activityMax > 0 {
		tighten(activityMax, "activity_max")
	}
	if ap.RegisterHours > 0 && ap.RegisterMax > 0 {
		tighten(ap.RegisterMax, "register_max")
	}
	if ap.PlatformDailyMax > 0 {
		sold := ap.PlatformDailySold
		if PlatformDailyBucketKey(ap.DailyRefreshTime, now) != ap.PlatformDailyBucket {
			sold = 0
		}
		var left uint32
		if sold < ap.PlatformDailyMax {
			left = ap.PlatformDailyMax - sold
		}
		tighten(left, "platform_daily")
	}

	if accountID == nil || *accountID == 0 {
		return out, nil
	}

	if ap.PerUserMaxQty > 0 {
		bought, err := sumBoughtQty(db, *accountID, ap.ID)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < ap.PerUserMaxQty {
			left = ap.PerUserMaxQty - bought
		}
		tighten(left, "per_user_qty")
	}

	type qtyLim struct {
		max    uint32
		unit   string
		reason string
	}
	lims := []qtyLim{
		{ap.DailyMax, "day", "daily"},
		{ap.WeeklyMax, "week", "weekly"},
		{ap.MonthlyMax, "month", "monthly"},
		{activityMax, "", "activity_max"},
	}
	cfg := limitRefreshFromProduct(ap)
	for _, lim := range lims {
		if lim.max == 0 {
			continue
		}
		var start, end time.Time
		if lim.unit != "" {
			start, end = calendarWindowFor(now, lim.unit, cfg)
		}
		bought, err := sumBoughtQtyInWindow(db, *accountID, ap.ID, start, end)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < lim.max {
			left = lim.max - bought
		}
		tighten(left, lim.reason)
	}

	if ap.RegisterHours > 0 && ap.RegisterMax > 0 {
		start := accountCreatedAt
		end := registerDeadline(accountCreatedAt, ap.RegisterHours)
		bought, err := sumBoughtQtyInWindow(db, *accountID, ap.ID, start, end)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < ap.RegisterMax {
			left = ap.RegisterMax - bought
		}
		tighten(left, "register_max")
	}

	if actLimits.UserMaxQty > 0 {
		bought, err := sumBoughtQtyByActivity(db, *accountID, ap.ActivityID)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < actLimits.UserMaxQty {
			left = actLimits.UserMaxQty - bought
		}
		tighten(left, "activity_user_max")
	}

	if actLimits.UserDailyMax > 0 {
		dayCfg := limitRefreshConfig{DailyTime: actLimits.UserDailyRefreshTime}
		start, end := calendarWindowFor(now, "day", dayCfg)
		bought, err := sumBoughtQtyByActivityInWindow(db, *accountID, ap.ActivityID, start, end)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < actLimits.UserDailyMax {
			left = actLimits.UserDailyMax - bought
		}
		tighten(left, "activity_user_daily")
	}

	return out, nil
}
