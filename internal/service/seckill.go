package service

import (
	"fmt"
	"sort"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
)

// SeckillProductView 首页秒杀卡片所需字段。
type SeckillProductView struct {
	ID             uint64    `json:"id"`
	ActivityID     uint64    `json:"activity_id"`
	MerchantID     uint64    `json:"merchant_id"`
	ProductID      uint64    `json:"product_id"`
	ProductName    string    `json:"product_name"`
	ProductCover   string    `json:"product_cover"`
	ActivityPrice  float64   `json:"activity_price"`
	OriginalPrice  float64   `json:"original_price"`
	ActivityStock  uint32    `json:"activity_stock"`
	SoldCount      uint32    `json:"sold_count"`
	AvailableStock uint32    `json:"available_stock"`
	StockProgress  float64   `json:"stock_progress"` // 0~1；activity_stock=0 时为 0
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	LimitLabels    []string  `json:"limit_labels"`
	LimitReached   bool      `json:"limit_reached"`
	LimitReason    string    `json:"limit_reason,omitempty"` // daily|weekly|monthly|activity_max|register_max
	DeadlineAt     time.Time `json:"deadline_at"`               // 该商品最短到期（活动结束与启用中的限购窗取最早）
	CanBuy         bool      `json:"can_buy"`
	ButtonState    string    `json:"button_state"` // buy|sold_out|limit_reached
}

// ListSeckillForUser 返回进行中活动下的上架活动商品。
// accountID 为 nil（未登录）时排除 register_hours>0 的商品，且不计个人限购。
// 已登录但不在新用户窗内的 register_hours>0 商品同样排除；达限仍返回并标记 limit_reached。
func (s *ActivityService) ListSeckillForUser(accountID *uint64) ([]SeckillProductView, error) {
	now := time.Now()

	var activities []model.Activity
	if err := query.NotDeleted(s.DB).
		Where("status = ? AND start_at <= ? AND end_at >= ?", model.ActivityStatusOn, now, now).
		Order("sort_order ASC, id DESC").
		Find(&activities).Error; err != nil {
		return nil, err
	}
	if len(activities) == 0 {
		return []SeckillProductView{}, nil
	}

	actByID := make(map[uint64]*model.Activity, len(activities))
	actIDs := make([]uint64, 0, len(activities))
	for i := range activities {
		actByID[activities[i].ID] = &activities[i]
		actIDs = append(actIDs, activities[i].ID)
	}

	var items []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ? AND status = ?", model.NotDeleted, model.ProductStatusOn).
		Where("activity_id IN ? AND status = 1", actIDs).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	var accountCreatedAt time.Time
	loggedIn := accountID != nil && *accountID > 0
	if loggedIn {
		var account model.Account
		if err := query.NotDeleted(s.DB).Select("id", "created_at").First(&account, *accountID).Error; err != nil {
			// 账号异常时降级为未登录列表，避免整页 500
			loggedIn = false
		} else {
			accountCreatedAt = account.CreatedAt
		}
	}

	out := make([]SeckillProductView, 0, len(items))
	for i := range items {
		ap := &items[i]
		act := actByID[ap.ActivityID]
		if act == nil || ap.Product == nil || ap.Product.ID == 0 {
			continue
		}

		if ap.RegisterHours > 0 {
			if !loggedIn || !inRegisterWindow(accountCreatedAt, now, ap.RegisterHours) {
				continue
			}
		}

		view := buildSeckillProductView(act, ap, ap.Product)
		view.DeadlineAt = seckillDeadlineAt(act, ap, accountCreatedAt, loggedIn, now)
		if loggedIn {
			reached, reason, err := s.seckillLimitStatus(*accountID, ap, accountCreatedAt, now)
			if err != nil {
				return nil, err
			}
			view.LimitReached = reached
			view.LimitReason = reason
		}
		view.CanBuy = !view.LimitReached && view.AvailableStock > 0
		view.ButtonState = seckillButtonState(view.LimitReached, view.AvailableStock)
		out = append(out, view)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DeadlineAt.Equal(out[j].DeadlineAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].DeadlineAt.Before(out[j].DeadlineAt)
	})
	return out, nil
}

func buildSeckillProductView(act *model.Activity, ap *model.ActivityProduct, p *model.Product) SeckillProductView {
	avail := availableActivityStock(ap, p)
	progress := 0.0
	if ap.ActivityStock > 0 {
		progress = float64(ap.SoldCount) / float64(ap.ActivityStock)
		if progress > 1 {
			progress = 1
		}
	}
	cover := ""
	if p.CoverURL != "" {
		cover = p.CoverURL
	}
	return SeckillProductView{
		ID:             ap.ID,
		ActivityID:     ap.ActivityID,
		MerchantID:     p.MerchantID,
		ProductID:      ap.ProductID,
		ProductName:    p.Name,
		ProductCover:   cover,
		ActivityPrice:  ap.ActivityPrice,
		OriginalPrice:  p.Price,
		ActivityStock:  ap.ActivityStock,
		SoldCount:      ap.SoldCount,
		AvailableStock: avail,
		StockProgress:  progress,
		StartAt:        act.StartAt,
		EndAt:          act.EndAt,
		LimitLabels:    buildSeckillLimitLabels(ap),
	}
}

func buildSeckillLimitLabels(ap *model.ActivityProduct) []string {
	labels := make([]string, 0, 6)
	if ap.RegisterHours > 0 {
		labels = append(labels, fmt.Sprintf("新用户%d小时内", ap.RegisterHours))
		if ap.RegisterMax > 0 {
			labels = append(labels, fmt.Sprintf("窗内限购%d件", ap.RegisterMax))
		}
	}
	if ap.PlatformDailyMax > 0 {
		labels = append(labels, fmt.Sprintf("今日限量%d件", ap.PlatformDailyMax))
	}
	if ap.DailyMax > 0 {
		labels = append(labels, fmt.Sprintf("每日限购%d件", ap.DailyMax))
	}
	if ap.WeeklyMax > 0 {
		labels = append(labels, fmt.Sprintf("每周限购%d件", ap.WeeklyMax))
	}
	if ap.MonthlyMax > 0 {
		labels = append(labels, fmt.Sprintf("每月限购%d件", ap.MonthlyMax))
	}
	activityMax := ap.ActivityMax
	if activityMax == 0 && ap.PerUserMaxOrders > 0 {
		activityMax = ap.PerUserMaxOrders
	}
	if activityMax > 0 {
		labels = append(labels, fmt.Sprintf("全程限购%d件", activityMax))
	}
	if ap.PerUserMaxQty > 0 {
		labels = append(labels, fmt.Sprintf("每人限购%d件", ap.PerUserMaxQty))
	}
	return labels
}

func seckillButtonState(limitReached bool, available uint32) string {
	if available == 0 {
		return "sold_out"
	}
	if limitReached {
		return "limit_reached"
	}
	return "buy"
}

// seckillDeadlineAt 取该商品当前最短到期：活动结束 ∪ 已启用日/周/月窗结束 ∪
// 平台日限窗结束（当 PlatformDailyMax>0）∪（登录且启用时）新用户窗截止。
// 各窗口均通过 calendarWindowFor + limitRefreshFromProduct 使用商品真实的
// 每维刷新时间，而非午夜或 DailyRefreshTime 一刀切。
func seckillDeadlineAt(act *model.Activity, ap *model.ActivityProduct, accountCreatedAt time.Time, loggedIn bool, now time.Time) time.Time {
	deadline := act.EndAt
	consider := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if deadline.IsZero() || t.Before(deadline) {
			deadline = t
		}
	}
	cfg := limitRefreshFromProduct(ap)
	if ap.DailyMax > 0 {
		_, end := calendarWindowFor(now, "day", cfg)
		consider(end)
	}
	if ap.WeeklyMax > 0 {
		_, end := calendarWindowFor(now, "week", cfg)
		consider(end)
	}
	if ap.MonthlyMax > 0 {
		_, end := calendarWindowFor(now, "month", cfg)
		consider(end)
	}
	if ap.PlatformDailyMax > 0 {
		_, end := calendarWindowFor(now, "day", cfg)
		consider(end)
	}
	if loggedIn && ap.RegisterHours > 0 {
		consider(registerDeadline(accountCreatedAt, ap.RegisterHours))
	}
	return deadline
}

// seckillLimitStatus 检查日/周/月/全程/新用户窗/每人件数限购（不含 register 窗外，调用方已过滤）。
// 各窗口按已购件数累计。返回首个触达的 reason。
func (s *ActivityService) seckillLimitStatus(accountID uint64, ap *model.ActivityProduct, accountCreatedAt, now time.Time) (bool, string, error) {
	stock := ^uint32(0)
	if ap.Product != nil {
		stock = availableActivityStock(ap, ap.Product)
	}
	aid := accountID
	remain, err := computeActivityRemaining(s.DB, ap, stock, &aid, accountCreatedAt, now)
	if err != nil {
		return false, "", err
	}
	return remain.LimitReached, remain.LimitReason, nil
}
