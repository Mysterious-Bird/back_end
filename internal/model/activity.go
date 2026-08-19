package model

import "time"

const (
	ActivityStatusOff   uint8 = 0
	ActivityStatusOn    uint8 = 1
	ActivityStatusDraft uint8 = 2
)

type Activity struct {
	ID                   uint64    `gorm:"primaryKey" json:"id"`
	MerchantID           uint64    `gorm:"not null" json:"merchant_id"`
	Name                 string    `gorm:"size:128;not null" json:"name"`
	Description          *string   `gorm:"type:text" json:"description,omitempty"`
	CoverURL             *string   `gorm:"column:cover_url;size:512" json:"cover_url,omitempty"`
	BannerImages         []string  `gorm:"serializer:json" json:"banner_images,omitempty"`
	StartAt              time.Time `gorm:"not null" json:"start_at"`
	EndAt                time.Time `gorm:"not null" json:"end_at"`
	Status               uint8     `gorm:"not null;default:2" json:"status"`
	EnableCoupon         uint8     `gorm:"not null;default:1" json:"enable_coupon"`
	UserMaxQty           uint32    `gorm:"not null;default:0" json:"user_max_qty"`                             // 活动内每人最多购买件数（跨商品累计），0=不限
	UserDailyMax         uint32    `gorm:"not null;default:0" json:"user_daily_max"`                           // 活动内每人每天最多购买件数，0=不限
	UserDailyRefreshTime string    `gorm:"type:time;not null;default:00:00:00" json:"user_daily_refresh_time"` // 活动每人每天限购刷新时刻
	SortOrder            int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	SoftDelete
	Products []ActivityProduct `gorm:"foreignKey:ActivityID" json:"products,omitempty"`
}

func (Activity) TableName() string { return "activity" }

type ActivityProduct struct {
	ID                         uint64    `gorm:"primaryKey" json:"id"`
	ActivityID                 uint64    `gorm:"not null" json:"activity_id"`
	ProductID                  uint64    `gorm:"not null" json:"product_id"`
	ActivityPrice              float64   `gorm:"type:decimal(10,2);not null" json:"activity_price"`
	ActivityStock              uint32    `gorm:"not null;default:0" json:"activity_stock"`
	SoldCount                  uint32    `gorm:"not null;default:0" json:"sold_count"`
	PerUserMaxQty              uint32    `gorm:"not null;default:0" json:"per_user_max_qty"`
	PerUserMaxOrders           uint32    `gorm:"not null;default:0" json:"per_user_max_orders"` // legacy 全程限购；校验时若 ActivityMax==0 且 PerUserMaxOrders>0 则视 PerUserMaxOrders 为 ActivityMax；写入新数据优先写 ActivityMax
	DailyMax                   uint32    `gorm:"not null;default:0" json:"daily_max"`
	WeeklyMax                  uint32    `gorm:"not null;default:0" json:"weekly_max"`
	MonthlyMax                 uint32    `gorm:"not null;default:0" json:"monthly_max"`
	ActivityMax                uint32    `gorm:"not null;default:0" json:"activity_max"`
	RegisterHours              uint32    `gorm:"not null;default:0" json:"register_hours"`
	RegisterMax                uint32    `gorm:"not null;default:0" json:"register_max"`
	PlatformDailyMax           uint32    `gorm:"not null;default:0" json:"platform_daily_max"`
	DailyRefreshTime           string    `gorm:"type:time;not null;default:00:00:00" json:"daily_refresh_time"`
	WeeklyRefreshWeekday       uint8     `gorm:"not null;default:1" json:"weekly_refresh_weekday"`
	WeeklyRefreshTime          string    `gorm:"type:time;not null;default:00:00:00" json:"weekly_refresh_time"`
	MonthlyRefreshDay          uint8     `gorm:"not null;default:1" json:"monthly_refresh_day"`
	MonthlyRefreshTime         string    `gorm:"type:time;not null;default:00:00:00" json:"monthly_refresh_time"`
	PlatformDailySold          uint32    `gorm:"not null;default:0" json:"platform_daily_sold"`
	PlatformDailyBucket        string    `gorm:"size:32;not null;default:''" json:"platform_daily_bucket"`
	EnableGroupBuy             uint8     `gorm:"not null;default:0" json:"enable_group_buy"`
	EnableBargain              uint8     `gorm:"not null;default:0" json:"enable_bargain"` // 1=砍价商品（与拼团互斥）
	BargainFloorPrice          *float64  `gorm:"type:decimal(10,2)" json:"bargain_floor_price,omitempty"`
	BargainDurationHours       uint32    `gorm:"not null;default:24" json:"bargain_duration_hours"`
	BargainNewUserHours        uint32    `gorm:"not null;default:48" json:"bargain_new_user_hours"`
	BargainHelpDailyMax        uint32    `gorm:"not null;default:20" json:"bargain_help_daily_max"`
	BargainSelfCutMax          float64   `gorm:"type:decimal(10,2);not null;default:1" json:"bargain_self_cut_max"`
	BargainNewCutMode          uint8     `gorm:"not null;default:1" json:"bargain_new_cut_mode"` // 1随机 2固定
	BargainNewMin              float64   `gorm:"type:decimal(10,2);not null;default:1" json:"bargain_new_min"`
	BargainNewMax              float64   `gorm:"type:decimal(10,2);not null;default:5" json:"bargain_new_max"`
	BargainOldCutMode          uint8     `gorm:"not null;default:1" json:"bargain_old_cut_mode"` // 1随机 2固定
	BargainOldMin              float64   `gorm:"type:decimal(10,2);not null;default:0.1" json:"bargain_old_min"`
	BargainOldMax              float64   `gorm:"type:decimal(10,2);not null;default:1" json:"bargain_old_max"`
	GroupBuyPrice              *float64  `gorm:"type:decimal(10,2)" json:"group_buy_price,omitempty"`
	GroupBuyTargetCount        *uint32   `json:"group_buy_target_count,omitempty"`
	GroupBuyAllowRepeat        uint8     `gorm:"not null;default:0" json:"group_buy_allow_repeat"`
	GroupBuyMaxJoinsPerUser    uint32    `gorm:"not null;default:1" json:"group_buy_max_joins_per_user"`
	GroupBuyMaxConcurrentTeams uint32    `gorm:"column:group_buy_max_concurrent_teams;not null;default:0" json:"group_buy_max_concurrent_teams"`
	ExpireDays                 *uint32   `gorm:"column:expire_days" json:"expire_days,omitempty"`
	EnableCoupon               uint8     `gorm:"not null;default:1" json:"enable_coupon"`
	SortOrder                  int       `gorm:"not null;default:0" json:"sort_order"`
	Status                     uint8     `gorm:"not null;default:1" json:"status"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
	SoftDelete
	Product *Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (ActivityProduct) TableName() string { return "activity_product" }

func (a *Activity) IsActiveNow(now time.Time) bool {
	if a.Status != ActivityStatusOn {
		return false
	}
	if now.Before(a.StartAt) || now.After(a.EndAt) {
		return false
	}
	return true
}
