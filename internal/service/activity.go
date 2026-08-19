package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var (
	ErrActivityNotFound        = errors.New("activity not found")
	ErrActivityForbidden       = errors.New("activity forbidden")
	ErrActivityNotActive       = errors.New("activity not active")
	ErrActivityProductNotFound = errors.New("activity product not found")
	ErrActivityLimitExceeded   = errors.New("activity purchase limit exceeded")
	ErrActivityRegisterWindow  = errors.New("not in register purchase window")
)

type ActivityService struct {
	DB *gorm.DB
}

type ActivityInput struct {
	MerchantID           uint64
	Name                 string
	Description          *string
	CoverURL             *string
	BannerImages         []string
	StartAt              time.Time
	EndAt                time.Time
	Status               uint8
	EnableCoupon         uint8
	UserMaxQty           uint32
	UserDailyMax         uint32
	UserDailyRefreshTime string
	SortOrder            int
}

type ActivityProductInput struct {
	ProductID                  uint64
	ActivityPrice              float64
	ActivityStock              uint32
	PerUserMaxQty              uint32
	PerUserMaxOrders           uint32
	DailyMax                   uint32
	WeeklyMax                  uint32
	MonthlyMax                 uint32
	ActivityMax                uint32
	RegisterHours              uint32
	RegisterMax                uint32
	PlatformDailyMax           uint32
	DailyRefreshTime           string
	WeeklyRefreshWeekday       uint8
	WeeklyRefreshTime          string
	MonthlyRefreshDay          uint8
	MonthlyRefreshTime         string
	EnableGroupBuy             uint8
	EnableBargain              uint8
	BargainFloorPrice          *float64
	BargainDurationHours       uint32
	BargainNewUserHours        uint32
	BargainHelpDailyMax        uint32
	BargainSelfCutMax          float64
	BargainNewCutMode          uint8
	BargainNewMin              float64
	BargainNewMax              float64
	BargainOldCutMode          uint8
	BargainOldMin              float64
	BargainOldMax              float64
	GroupBuyPrice              *float64
	GroupBuyTargetCount        *uint32
	GroupBuyAllowRepeat        uint8
	GroupBuyMaxJoinsPerUser    uint32
	GroupBuyMaxConcurrentTeams uint32
	ExpireDays                 *uint32
	EnableCoupon               uint8
	SortOrder                  int
	Status                     uint8
}

// UpdateActivityProductPatch 活动商品部分更新。
type UpdateActivityProductPatch struct {
	ActivityPrice              *float64
	ActivityStock              *uint32
	PerUserMaxQty              *uint32
	PerUserMaxOrders           *uint32
	DailyMax                   *uint32
	WeeklyMax                  *uint32
	MonthlyMax                 *uint32
	ActivityMax                *uint32
	RegisterHours              *uint32
	RegisterMax                *uint32
	PlatformDailyMax           *uint32
	DailyRefreshTime           *string
	WeeklyRefreshWeekday       *uint8
	WeeklyRefreshTime          *string
	MonthlyRefreshDay          *uint8
	MonthlyRefreshTime         *string
	EnableGroupBuy             *uint8
	EnableBargain              *uint8
	BargainFloorPrice          *float64
	BargainDurationHours       *uint32
	BargainNewUserHours        *uint32
	BargainHelpDailyMax        *uint32
	BargainSelfCutMax          *float64
	BargainNewCutMode          *uint8
	BargainNewMin              *float64
	BargainNewMax              *float64
	BargainOldCutMode          *uint8
	BargainOldMin              *float64
	BargainOldMax              *float64
	GroupBuyPrice              *float64
	GroupBuyTargetCount        *uint32
	GroupBuyAllowRepeat        *uint8
	GroupBuyMaxJoinsPerUser    *uint32
	GroupBuyMaxConcurrentTeams *uint32
	ExpireDays                 *uint32
	EnableCoupon               *uint8
	SortOrder                  *int
	Status                     *uint8
}

type ActivityListFilter struct {
	MerchantID *uint64
	Status     *uint8
	ActiveOnly bool
}

type ActivityStoreView struct {
	model.Activity
	IsActive             bool  `json:"is_active"`
	EnableGroupBuy       uint8 `json:"enable_group_buy"`
	GroupBuyProductCount int64 `json:"group_buy_product_count"`
}

type ActivityDetailView struct {
	ActivityStoreView
	Products []ActivityProductItemView `json:"products,omitempty"`
}

type ActivityPublicDetailView struct {
	ActivityStoreView
	Products []ActivityProductStoreView `json:"products,omitempty"`
}

type ActivityProductItemView struct {
	model.ActivityProduct
	ProductName           string                    `json:"product_name,omitempty"`
	ProductCover          string                    `json:"product_cover,omitempty"`
	CanGroupBuy           bool                      `json:"can_group_buy"`
	CanUseCoupon          bool                      `json:"can_use_coupon"`
	ApplicableMerchantIDs []uint64                  `json:"applicable_merchant_ids"`
	ApplicableMerchants   []ApplicableMerchantBrief `json:"applicable_merchants"`
}

type ActivityProductStoreView struct {
	model.ActivityProduct
	MerchantID            uint64                    `json:"merchant_id"`
	ProductName           string                    `json:"product_name"`
	ProductCover          string                    `json:"product_cover"`
	OriginalPrice         float64                   `json:"original_price"`
	ItemType              uint8                     `json:"item_type"`
	AvailableStock        uint32                    `json:"available_stock"`
	CanGroupBuy           bool                      `json:"can_group_buy"`
	CanUseCoupon          bool                      `json:"can_use_coupon"`
	SaleOptions           ProductSaleOptions        `json:"sale_options"`
	LimitLabels           []string                  `json:"limit_labels"`
	LimitReached          bool                      `json:"limit_reached"`
	LimitReason           string                    `json:"limit_reason,omitempty"`
	RemainingQty          uint32                    `json:"remaining_qty"`
	PackageGroups         []PackageGroupView        `json:"package_groups,omitempty"`
	ApplicableMerchantIDs []uint64                  `json:"applicable_merchant_ids"`
	ApplicableMerchants   []ApplicableMerchantBrief `json:"applicable_merchants"`
}

type ActivityOrderContext struct {
	Activity        *model.Activity
	ActivityProduct *model.ActivityProduct
	Product         model.Product
	UnitPrice       float64
	EnableCoupon    bool
	GroupBuyConfig  *ActivityGroupBuyConfig
}

type ActivityGroupBuyConfig struct {
	EnableGroupBuy             uint8
	GroupBuyPrice              float64
	GroupBuyTargetCount        uint32
	GroupBuyAllowRepeat        uint8
	GroupBuyMaxJoinsPerUser    uint32
	GroupBuyMaxConcurrentTeams uint32
}

func (s *ActivityService) List(page, pageSize int, filter ActivityListFilter) ([]model.Activity, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	q := query.NotDeleted(s.DB.Model(&model.Activity{}))
	if filter.MerchantID != nil {
		q = q.Where("merchant_id = ?", *filter.MerchantID)
	}
	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.ActiveOnly {
		now := time.Now()
		q = q.Where("status = ? AND start_at <= ? AND end_at >= ?", model.ActivityStatusOn, now, now)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.Activity
	if err := q.Order("sort_order ASC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *ActivityService) GetByID(id uint64, merchantID *uint64) (*model.Activity, error) {
	var act model.Activity
	q := query.NotDeleted(s.DB).Where("id = ?", id)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if err := q.First(&act).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	return &act, nil
}

func (s *ActivityService) GetDetail(id uint64, merchantID *uint64) (*model.Activity, error) {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	var products []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", id).
		Order("sort_order ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	act.Products = products
	return act, nil
}

func (s *ActivityService) ListProducts(activityID uint64, merchantID *uint64) ([]model.ActivityProduct, error) {
	if _, err := s.GetByID(activityID, merchantID); err != nil {
		return nil, err
	}
	var products []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", activityID).
		Order("sort_order ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

func (s *ActivityService) Create(input ActivityInput) (*model.Activity, error) {
	rt, err := NormalizeDailyRefreshTime(input.UserDailyRefreshTime)
	if err != nil {
		return nil, fmt.Errorf("%w: user_daily_refresh_time 格式无效", ErrInvalidProductArg)
	}
	input.UserDailyRefreshTime = rt
	if err := validateActivityInput(input); err != nil {
		return nil, err
	}
	act := model.Activity{
		MerchantID: input.MerchantID, Name: strings.TrimSpace(input.Name),
		Description: input.Description, CoverURL: input.CoverURL,
		BannerImages: input.BannerImages, StartAt: input.StartAt, EndAt: input.EndAt,
		Status: input.Status, EnableCoupon: normalizeEnableCoupon(input.EnableCoupon),
		UserMaxQty: input.UserMaxQty, UserDailyMax: input.UserDailyMax,
		UserDailyRefreshTime: input.UserDailyRefreshTime, SortOrder: input.SortOrder,
	}
	if act.Status == 0 {
		act.Status = model.ActivityStatusDraft
	}
	if err := s.DB.Create(&act).Error; err != nil {
		return nil, err
	}
	return &act, nil
}

func (s *ActivityService) Update(id uint64, input ActivityInput, merchantID *uint64) (*model.Activity, error) {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	if err := validateActivityInput(input); err != nil {
		return nil, err
	}
	rt, err := NormalizeDailyRefreshTime(input.UserDailyRefreshTime)
	if err != nil {
		return nil, fmt.Errorf("%w: user_daily_refresh_time 格式无效", ErrInvalidProductArg)
	}
	input.UserDailyRefreshTime = rt
	updates := map[string]interface{}{
		"name": input.Name, "description": input.Description, "cover_url": input.CoverURL,
		"banner_images": toJSONColumn(input.BannerImages),
		"start_at":      input.StartAt, "end_at": input.EndAt, "status": input.Status,
		"enable_coupon": normalizeEnableCoupon(input.EnableCoupon),
		"user_max_qty":  input.UserMaxQty, "user_daily_max": input.UserDailyMax,
		"user_daily_refresh_time": input.UserDailyRefreshTime,
		"sort_order":              input.SortOrder,
	}
	if err := s.DB.Model(act).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, merchantID)
}

func (s *ActivityService) Delete(id uint64, merchantID *uint64) error {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return err
	}
	if err := assertActivityDeletable(s.DB, id); err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.SoftDelete(tx, &model.ActivityProduct{}, "activity_id = ?", id).Error; err != nil {
			return err
		}
		return query.SoftDelete(tx, act).Error
	})
}

func (s *ActivityService) AddProduct(activityID uint64, input ActivityProductInput, merchantID *uint64) (*model.ActivityProduct, error) {
	act, err := s.GetByID(activityID, merchantID)
	if err != nil {
		return nil, err
	}
	if err := validateActivityProductInput(input); err != nil {
		return nil, err
	}
	rt, err := NormalizeDailyRefreshTime(input.DailyRefreshTime)
	if err != nil {
		return nil, err
	}
	input.DailyRefreshTime = rt
	wrt, err := NormalizeDailyRefreshTime(input.WeeklyRefreshTime)
	if err != nil {
		return nil, fmt.Errorf("%w: weekly_refresh_time 格式无效", ErrInvalidProductArg)
	}
	input.WeeklyRefreshTime = wrt
	mrt, err := NormalizeDailyRefreshTime(input.MonthlyRefreshTime)
	if err != nil {
		return nil, fmt.Errorf("%w: monthly_refresh_time 格式无效", ErrInvalidProductArg)
	}
	input.MonthlyRefreshTime = mrt
	wd, err := normalizeWeeklyWeekday(input.WeeklyRefreshWeekday)
	if err != nil {
		return nil, err
	}
	input.WeeklyRefreshWeekday = wd
	md, err := normalizeMonthlyDay(input.MonthlyRefreshDay)
	if err != nil {
		return nil, err
	}
	input.MonthlyRefreshDay = md
	var product model.Product
	pq := query.NotDeleted(s.DB).Where("id = ?", input.ProductID)
	// 商家专场活动仍限制同店商品；平台活动（merchant_id=0）可跨店挂品
	if act.MerchantID != 0 {
		pq = pq.Where("merchant_id = ?", act.MerchantID)
	}
	if err := pq.First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}

	maxJoins := input.GroupBuyMaxJoinsPerUser
	status := input.Status
	if status == 0 {
		status = 1
	}

	// 同一活动允许同一 catalog product_id 多条活动商品
	// （例如 9.9 直购、1 元拼团、9.9 拼团并存）。每次添加都插入新行。
	input = normalizeActivityProductGroupBuyInput(input)
	input = normalizeBargainInput(input)
	if err := validateBargainOnActivityProduct(input); err != nil {
		return nil, err
	}
	ap := model.ActivityProduct{
		ActivityID: activityID, ProductID: input.ProductID,
		ActivityPrice: input.ActivityPrice, ActivityStock: input.ActivityStock,
		PerUserMaxQty: input.PerUserMaxQty, PerUserMaxOrders: input.PerUserMaxOrders,
		DailyMax: input.DailyMax, WeeklyMax: input.WeeklyMax, MonthlyMax: input.MonthlyMax,
		ActivityMax: input.ActivityMax, RegisterHours: input.RegisterHours, RegisterMax: input.RegisterMax,
		PlatformDailyMax: input.PlatformDailyMax, DailyRefreshTime: input.DailyRefreshTime,
		WeeklyRefreshWeekday: input.WeeklyRefreshWeekday, WeeklyRefreshTime: input.WeeklyRefreshTime,
		MonthlyRefreshDay: input.MonthlyRefreshDay, MonthlyRefreshTime: input.MonthlyRefreshTime,
		EnableGroupBuy: input.EnableGroupBuy, EnableBargain: input.EnableBargain,
		BargainFloorPrice: input.BargainFloorPrice, BargainDurationHours: input.BargainDurationHours,
		BargainNewUserHours: input.BargainNewUserHours, BargainHelpDailyMax: input.BargainHelpDailyMax,
		BargainSelfCutMax: input.BargainSelfCutMax, BargainNewCutMode: input.BargainNewCutMode,
		BargainNewMin: input.BargainNewMin, BargainNewMax: input.BargainNewMax,
		BargainOldCutMode: input.BargainOldCutMode, BargainOldMin: input.BargainOldMin, BargainOldMax: input.BargainOldMax,
		GroupBuyPrice:              input.GroupBuyPrice,
		GroupBuyTargetCount:        input.GroupBuyTargetCount,
		GroupBuyAllowRepeat:        input.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser:    maxJoins,
		GroupBuyMaxConcurrentTeams: input.GroupBuyMaxConcurrentTeams,
		ExpireDays:                 normalizeExpireDays(input.ExpireDays),
		EnableCoupon:               normalizeEnableCoupon(input.EnableCoupon),
		SortOrder:                  input.SortOrder, Status: status,
	}
	if err := s.DB.Create(&ap).Error; err != nil {
		return nil, fmt.Errorf("添加活动商品失败: %w", err)
	}
	return s.GetActivityProduct(ap.ID, merchantID)
}

func isMySQLDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func normalizeActivityProductGroupBuyInput(input ActivityProductInput) ActivityProductInput {
	if input.EnableGroupBuy != 1 {
		input.GroupBuyPrice = nil
		input.GroupBuyTargetCount = nil
		input.GroupBuyAllowRepeat = 0
		input.GroupBuyMaxJoinsPerUser = 0
		input.GroupBuyMaxConcurrentTeams = 0
	}
	return input
}

func activityProductUpdates(input ActivityProductInput, maxJoins uint32, status uint8) map[string]interface{} {
	input = normalizeActivityProductGroupBuyInput(input)
	input = normalizeBargainInput(input)
	if input.EnableGroupBuy != 1 {
		maxJoins = 0
	}
	return map[string]interface{}{
		"activity_price": input.ActivityPrice, "activity_stock": input.ActivityStock,
		"per_user_max_qty": input.PerUserMaxQty, "per_user_max_orders": input.PerUserMaxOrders,
		"daily_max": input.DailyMax, "weekly_max": input.WeeklyMax, "monthly_max": input.MonthlyMax,
		"activity_max": input.ActivityMax, "register_hours": input.RegisterHours, "register_max": input.RegisterMax,
		"platform_daily_max": input.PlatformDailyMax, "daily_refresh_time": input.DailyRefreshTime,
		"weekly_refresh_weekday": input.WeeklyRefreshWeekday, "weekly_refresh_time": input.WeeklyRefreshTime,
		"monthly_refresh_day": input.MonthlyRefreshDay, "monthly_refresh_time": input.MonthlyRefreshTime,
		"enable_group_buy": input.EnableGroupBuy, "group_buy_price": input.GroupBuyPrice,
		"group_buy_target_count":         input.GroupBuyTargetCount,
		"group_buy_allow_repeat":         input.GroupBuyAllowRepeat,
		"group_buy_max_joins_per_user":   maxJoins,
		"group_buy_max_concurrent_teams": input.GroupBuyMaxConcurrentTeams,
		"enable_bargain":                 input.EnableBargain,
		"bargain_floor_price":            input.BargainFloorPrice,
		"bargain_duration_hours":         input.BargainDurationHours,
		"bargain_new_user_hours":         input.BargainNewUserHours,
		"bargain_help_daily_max":         input.BargainHelpDailyMax,
		"bargain_self_cut_max":           input.BargainSelfCutMax,
		"bargain_new_cut_mode":           input.BargainNewCutMode,
		"bargain_new_min":                input.BargainNewMin,
		"bargain_new_max":                input.BargainNewMax,
		"bargain_old_cut_mode":           input.BargainOldCutMode,
		"bargain_old_min":                input.BargainOldMin,
		"bargain_old_max":                input.BargainOldMax,
		"expire_days":                    normalizeExpireDays(input.ExpireDays),
		"enable_coupon":                  normalizeEnableCoupon(input.EnableCoupon),
		"sort_order":                     input.SortOrder, "status": status,
	}
}

func (s *ActivityService) UpdateProduct(apID uint64, input ActivityProductInput, merchantID *uint64) (*model.ActivityProduct, error) {
	return s.UpdateProductInActivity(0, apID, activityProductInputToPatch(input), merchantID)
}

func (s *ActivityService) UpdateProductInActivity(activityID, apID uint64, patch UpdateActivityProductPatch, merchantID *uint64) (*model.ActivityProduct, error) {
	ap, err := s.GetActivityProduct(apID, merchantID)
	if err != nil {
		return nil, err
	}
	if activityID > 0 && ap.ActivityID != activityID {
		return nil, ErrActivityProductNotFound
	}
	if !patch.hasField() {
		return nil, ErrInvalidProductArg
	}
	merged := mergeActivityProductPatch(ap, patch)
	if err := validateActivityProductInput(merged); err != nil {
		return nil, err
	}
	rt, err := NormalizeDailyRefreshTime(merged.DailyRefreshTime)
	if err != nil {
		return nil, err
	}
	merged.DailyRefreshTime = rt
	wrt, err := NormalizeDailyRefreshTime(merged.WeeklyRefreshTime)
	if err != nil {
		return nil, fmt.Errorf("%w: weekly_refresh_time 格式无效", ErrInvalidProductArg)
	}
	merged.WeeklyRefreshTime = wrt
	mrt, err := NormalizeDailyRefreshTime(merged.MonthlyRefreshTime)
	if err != nil {
		return nil, fmt.Errorf("%w: monthly_refresh_time 格式无效", ErrInvalidProductArg)
	}
	merged.MonthlyRefreshTime = mrt
	wd, err := normalizeWeeklyWeekday(merged.WeeklyRefreshWeekday)
	if err != nil {
		return nil, err
	}
	merged.WeeklyRefreshWeekday = wd
	md, err := normalizeMonthlyDay(merged.MonthlyRefreshDay)
	if err != nil {
		return nil, err
	}
	merged.MonthlyRefreshDay = md
	maxJoins := merged.GroupBuyMaxJoinsPerUser
	status := merged.Status
	if status == 0 {
		status = 1
	}
	updates := activityProductUpdates(merged, maxJoins, status)
	if err := s.DB.Model(ap).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetActivityProduct(apID, merchantID)
}

func (p UpdateActivityProductPatch) hasField() bool {
	return p.ActivityPrice != nil || p.ActivityStock != nil || p.PerUserMaxQty != nil ||
		p.PerUserMaxOrders != nil || p.DailyMax != nil || p.WeeklyMax != nil ||
		p.MonthlyMax != nil || p.ActivityMax != nil || p.RegisterHours != nil ||
		p.RegisterMax != nil || p.PlatformDailyMax != nil || p.DailyRefreshTime != nil ||
		p.WeeklyRefreshWeekday != nil || p.WeeklyRefreshTime != nil ||
		p.MonthlyRefreshDay != nil || p.MonthlyRefreshTime != nil ||
		p.EnableGroupBuy != nil || p.GroupBuyPrice != nil ||
		p.GroupBuyTargetCount != nil || p.GroupBuyAllowRepeat != nil ||
		p.GroupBuyMaxJoinsPerUser != nil || p.GroupBuyMaxConcurrentTeams != nil || p.ExpireDays != nil ||
		p.EnableBargain != nil || p.BargainFloorPrice != nil || p.BargainDurationHours != nil ||
		p.BargainNewUserHours != nil || p.BargainHelpDailyMax != nil || p.BargainSelfCutMax != nil ||
		p.BargainNewMin != nil || p.BargainNewMax != nil || p.BargainOldMin != nil || p.BargainOldMax != nil ||
		p.BargainNewCutMode != nil || p.BargainOldCutMode != nil ||
		p.EnableCoupon != nil || p.SortOrder != nil || p.Status != nil
}

func activityProductInputToPatch(input ActivityProductInput) UpdateActivityProductPatch {
	patch := UpdateActivityProductPatch{
		ActivityPrice:              &input.ActivityPrice,
		ActivityStock:              &input.ActivityStock,
		PerUserMaxQty:              &input.PerUserMaxQty,
		PerUserMaxOrders:           &input.PerUserMaxOrders,
		DailyMax:                   &input.DailyMax,
		WeeklyMax:                  &input.WeeklyMax,
		MonthlyMax:                 &input.MonthlyMax,
		ActivityMax:                &input.ActivityMax,
		RegisterHours:              &input.RegisterHours,
		RegisterMax:                &input.RegisterMax,
		PlatformDailyMax:           &input.PlatformDailyMax,
		DailyRefreshTime:           &input.DailyRefreshTime,
		WeeklyRefreshWeekday:       &input.WeeklyRefreshWeekday,
		WeeklyRefreshTime:          &input.WeeklyRefreshTime,
		MonthlyRefreshDay:          &input.MonthlyRefreshDay,
		MonthlyRefreshTime:         &input.MonthlyRefreshTime,
		EnableGroupBuy:             &input.EnableGroupBuy,
		EnableBargain:              &input.EnableBargain,
		BargainFloorPrice:          input.BargainFloorPrice,
		BargainDurationHours:       &input.BargainDurationHours,
		BargainNewUserHours:        &input.BargainNewUserHours,
		BargainHelpDailyMax:        &input.BargainHelpDailyMax,
		BargainSelfCutMax:          &input.BargainSelfCutMax,
		BargainNewCutMode:          &input.BargainNewCutMode,
		BargainNewMin:              &input.BargainNewMin,
		BargainNewMax:              &input.BargainNewMax,
		BargainOldCutMode:          &input.BargainOldCutMode,
		BargainOldMin:              &input.BargainOldMin,
		BargainOldMax:              &input.BargainOldMax,
		GroupBuyPrice:              input.GroupBuyPrice,
		GroupBuyTargetCount:        input.GroupBuyTargetCount,
		GroupBuyAllowRepeat:        &input.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser:    &input.GroupBuyMaxJoinsPerUser,
		GroupBuyMaxConcurrentTeams: &input.GroupBuyMaxConcurrentTeams,
		ExpireDays:                 input.ExpireDays,
		EnableCoupon:               &input.EnableCoupon,
		SortOrder:                  &input.SortOrder,
		Status:                     &input.Status,
	}
	return patch
}

func mergeActivityProductPatch(ap *model.ActivityProduct, patch UpdateActivityProductPatch) ActivityProductInput {
	merged := ActivityProductInput{
		ProductID:                  ap.ProductID,
		ActivityPrice:              ap.ActivityPrice,
		ActivityStock:              ap.ActivityStock,
		PerUserMaxQty:              ap.PerUserMaxQty,
		PerUserMaxOrders:           ap.PerUserMaxOrders,
		DailyMax:                   ap.DailyMax,
		WeeklyMax:                  ap.WeeklyMax,
		MonthlyMax:                 ap.MonthlyMax,
		ActivityMax:                ap.ActivityMax,
		RegisterHours:              ap.RegisterHours,
		RegisterMax:                ap.RegisterMax,
		PlatformDailyMax:           ap.PlatformDailyMax,
		DailyRefreshTime:           ap.DailyRefreshTime,
		WeeklyRefreshWeekday:       ap.WeeklyRefreshWeekday,
		WeeklyRefreshTime:          ap.WeeklyRefreshTime,
		MonthlyRefreshDay:          ap.MonthlyRefreshDay,
		MonthlyRefreshTime:         ap.MonthlyRefreshTime,
		EnableGroupBuy:             ap.EnableGroupBuy,
		EnableBargain:              ap.EnableBargain,
		BargainFloorPrice:          ap.BargainFloorPrice,
		BargainDurationHours:       ap.BargainDurationHours,
		BargainNewUserHours:        ap.BargainNewUserHours,
		BargainHelpDailyMax:        ap.BargainHelpDailyMax,
		BargainSelfCutMax:          ap.BargainSelfCutMax,
		BargainNewCutMode:          ap.BargainNewCutMode,
		BargainNewMin:              ap.BargainNewMin,
		BargainNewMax:              ap.BargainNewMax,
		BargainOldCutMode:          ap.BargainOldCutMode,
		BargainOldMin:              ap.BargainOldMin,
		BargainOldMax:              ap.BargainOldMax,
		GroupBuyPrice:              ap.GroupBuyPrice,
		GroupBuyTargetCount:        ap.GroupBuyTargetCount,
		GroupBuyAllowRepeat:        ap.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser:    ap.GroupBuyMaxJoinsPerUser,
		GroupBuyMaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
		ExpireDays:                 ap.ExpireDays,
		EnableCoupon:               ap.EnableCoupon,
		SortOrder:                  ap.SortOrder,
		Status:                     ap.Status,
	}
	if patch.ActivityPrice != nil {
		merged.ActivityPrice = *patch.ActivityPrice
	}
	if patch.ActivityStock != nil {
		merged.ActivityStock = *patch.ActivityStock
	}
	if patch.PerUserMaxQty != nil {
		merged.PerUserMaxQty = *patch.PerUserMaxQty
	}
	if patch.PerUserMaxOrders != nil {
		merged.PerUserMaxOrders = *patch.PerUserMaxOrders
	}
	if patch.DailyMax != nil {
		merged.DailyMax = *patch.DailyMax
	}
	if patch.WeeklyMax != nil {
		merged.WeeklyMax = *patch.WeeklyMax
	}
	if patch.MonthlyMax != nil {
		merged.MonthlyMax = *patch.MonthlyMax
	}
	if patch.ActivityMax != nil {
		merged.ActivityMax = *patch.ActivityMax
	}
	if patch.RegisterHours != nil {
		merged.RegisterHours = *patch.RegisterHours
	}
	if patch.RegisterMax != nil {
		merged.RegisterMax = *patch.RegisterMax
	}
	if patch.PlatformDailyMax != nil {
		merged.PlatformDailyMax = *patch.PlatformDailyMax
	}
	if patch.DailyRefreshTime != nil {
		merged.DailyRefreshTime = *patch.DailyRefreshTime
	}
	if patch.WeeklyRefreshWeekday != nil {
		merged.WeeklyRefreshWeekday = *patch.WeeklyRefreshWeekday
	}
	if patch.WeeklyRefreshTime != nil {
		merged.WeeklyRefreshTime = *patch.WeeklyRefreshTime
	}
	if patch.MonthlyRefreshDay != nil {
		merged.MonthlyRefreshDay = *patch.MonthlyRefreshDay
	}
	if patch.MonthlyRefreshTime != nil {
		merged.MonthlyRefreshTime = *patch.MonthlyRefreshTime
	}
	if patch.EnableGroupBuy != nil {
		merged.EnableGroupBuy = *patch.EnableGroupBuy
	}
	if patch.EnableBargain != nil {
		merged.EnableBargain = *patch.EnableBargain
	}
	if patch.BargainFloorPrice != nil {
		merged.BargainFloorPrice = patch.BargainFloorPrice
	}
	if patch.BargainDurationHours != nil {
		merged.BargainDurationHours = *patch.BargainDurationHours
	}
	if patch.BargainNewUserHours != nil {
		merged.BargainNewUserHours = *patch.BargainNewUserHours
	}
	if patch.BargainHelpDailyMax != nil {
		merged.BargainHelpDailyMax = *patch.BargainHelpDailyMax
	}
	if patch.BargainSelfCutMax != nil {
		merged.BargainSelfCutMax = *patch.BargainSelfCutMax
	}
	if patch.BargainNewCutMode != nil {
		merged.BargainNewCutMode = *patch.BargainNewCutMode
	}
	if patch.BargainNewMin != nil {
		merged.BargainNewMin = *patch.BargainNewMin
	}
	if patch.BargainNewMax != nil {
		merged.BargainNewMax = *patch.BargainNewMax
	}
	if patch.BargainOldCutMode != nil {
		merged.BargainOldCutMode = *patch.BargainOldCutMode
	}
	if patch.BargainOldMin != nil {
		merged.BargainOldMin = *patch.BargainOldMin
	}
	if patch.BargainOldMax != nil {
		merged.BargainOldMax = *patch.BargainOldMax
	}
	if patch.GroupBuyPrice != nil {
		merged.GroupBuyPrice = patch.GroupBuyPrice
	}
	if patch.GroupBuyTargetCount != nil {
		merged.GroupBuyTargetCount = patch.GroupBuyTargetCount
	}
	if patch.GroupBuyAllowRepeat != nil {
		merged.GroupBuyAllowRepeat = *patch.GroupBuyAllowRepeat
	}
	if patch.GroupBuyMaxJoinsPerUser != nil {
		merged.GroupBuyMaxJoinsPerUser = *patch.GroupBuyMaxJoinsPerUser
	}
	if patch.GroupBuyMaxConcurrentTeams != nil {
		merged.GroupBuyMaxConcurrentTeams = *patch.GroupBuyMaxConcurrentTeams
	}
	if patch.ExpireDays != nil {
		merged.ExpireDays = normalizeExpireDays(patch.ExpireDays)
	}
	if patch.EnableCoupon != nil {
		merged.EnableCoupon = *patch.EnableCoupon
	}
	if patch.SortOrder != nil {
		merged.SortOrder = *patch.SortOrder
	}
	if patch.Status != nil {
		merged.Status = *patch.Status
	}
	return merged
}

func (s *ActivityService) GetProductInActivity(activityID, apID uint64, merchantID *uint64) (*model.ActivityProduct, error) {
	ap, err := s.GetActivityProduct(apID, merchantID)
	if err != nil {
		return nil, err
	}
	if ap.ActivityID != activityID {
		return nil, ErrActivityProductNotFound
	}
	return ap, nil
}

func (s *ActivityService) RemoveProductInActivity(activityID, apID uint64, merchantID *uint64) error {
	ap, err := s.GetProductInActivity(activityID, apID, merchantID)
	if err != nil {
		return err
	}
	if err := assertActivityProductDeletable(s.DB, ap); err != nil {
		return err
	}
	return query.SoftDelete(s.DB, ap).Error
}

func (s *ActivityService) RemoveProduct(apID uint64, merchantID *uint64) error {
	ap, err := s.GetActivityProduct(apID, merchantID)
	if err != nil {
		return err
	}
	if err := assertActivityProductDeletable(s.DB, ap); err != nil {
		return err
	}
	return query.SoftDelete(s.DB, ap).Error
}

func (s *ActivityService) GetActivityProduct(apID uint64, merchantID *uint64) (*model.ActivityProduct, error) {
	var ap model.ActivityProduct
	q := query.NotDeleted(s.DB).Preload("Product", "is_deleted = ?", model.NotDeleted).Where("id = ?", apID)
	if err := q.First(&ap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityProductNotFound
		}
		return nil, err
	}
	if merchantID != nil {
		if _, err := s.GetByID(ap.ActivityID, merchantID); err != nil {
			return nil, err
		}
	}
	return &ap, nil
}

func (s *ActivityService) ToStoreView(act *model.Activity, publicOnly bool) ActivityStoreView {
	return s.ToStoreViews([]model.Activity{*act}, publicOnly)[0]
}

func (s *ActivityService) ToStoreViews(activities []model.Activity, publicOnly bool) []ActivityStoreView {
	if len(activities) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(activities))
	for i := range activities {
		ids = append(ids, activities[i].ID)
	}
	counts := s.countGroupBuyProductsByActivity(ids, publicOnly)
	now := time.Now()
	views := make([]ActivityStoreView, 0, len(activities))
	for i := range activities {
		cnt := counts[activities[i].ID]
		views = append(views, ActivityStoreView{
			Activity:             activities[i],
			IsActive:             activities[i].IsActiveNow(now),
			EnableGroupBuy:       boolToUint8(cnt > 0),
			GroupBuyProductCount: cnt,
		})
	}
	return views
}

func (s *ActivityService) ToDetailView(act *model.Activity, products []model.ActivityProduct, publicOnly bool) ActivityDetailView {
	store := s.ToStoreView(act, publicOnly)
	items := make([]ActivityProductItemView, 0, len(products))
	for i := range products {
		if publicOnly {
			if products[i].Status != 1 {
				continue
			}
			if products[i].Product == nil || products[i].Product.Status != model.ProductStatusOn {
				continue
			}
		}
		items = append(items, toActivityProductItemView(act, &products[i]))
	}
	return ActivityDetailView{ActivityStoreView: store, Products: items}
}

func (s *ActivityService) GetStoreDetail(id uint64) (*ActivityPublicDetailView, error) {
	act, err := s.GetByID(id, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	products, err := s.ListStoreProducts(id, false)
	if err != nil {
		return nil, err
	}
	return &ActivityPublicDetailView{
		ActivityStoreView: s.ToStoreView(act, true),
		Products:          products,
	}, nil
}

func (s *ActivityService) GetDetailView(id uint64, merchantID *uint64) (*ActivityDetailView, error) {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	var products []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", id).
		Order("sort_order ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	view := s.ToDetailView(act, products, false)
	if err := s.enrichActivityProductItemViewsApplicable(view.Products); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *ActivityService) ListProductItemViews(activityID uint64, merchantID *uint64, publicOnly bool) ([]ActivityProductItemView, error) {
	act, err := s.GetByID(activityID, merchantID)
	if err != nil {
		return nil, err
	}
	if publicOnly && !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	q := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", activityID)
	if publicOnly {
		q = q.Where("status = 1")
	}
	var products []model.ActivityProduct
	if err := q.Order("sort_order ASC, id ASC").Find(&products).Error; err != nil {
		return nil, err
	}
	views := make([]ActivityProductItemView, 0, len(products))
	for i := range products {
		if publicOnly {
			if products[i].Product == nil || products[i].Product.Status != model.ProductStatusOn {
				continue
			}
		}
		views = append(views, toActivityProductItemView(act, &products[i]))
	}
	if err := s.enrichActivityProductItemViewsApplicable(views); err != nil {
		return nil, err
	}
	return views, nil
}

func toActivityProductItemView(act *model.Activity, ap *model.ActivityProduct) ActivityProductItemView {
	view := ActivityProductItemView{
		ActivityProduct: *ap,
		CanGroupBuy:     activityProductCanGroupBuy(ap),
		CanUseCoupon:    act.EnableCoupon == 1 && ap.EnableCoupon == 1,
	}
	if ap.Product != nil {
		view.ProductName = ap.Product.Name
		view.ProductCover = ap.Product.CoverURL
	}
	return view
}

func activityProductCanGroupBuy(ap *model.ActivityProduct) bool {
	return ap.EnableGroupBuy == 1 && ap.GroupBuyPrice != nil && *ap.GroupBuyPrice > 0 &&
		ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2
}

func (s *ActivityService) countGroupBuyProductsByActivity(activityIDs []uint64, publicOnly bool) map[uint64]int64 {
	out := make(map[uint64]int64, len(activityIDs))
	if len(activityIDs) == 0 {
		return out
	}
	// 表名前缀 is_deleted：JOIN product 后裸列名会 Error 1052 Column 'is_deleted' in where clause is ambiguous
	q := s.DB.Model(&model.ActivityProduct{}).
		Select("activity_product.activity_id AS activity_id, COUNT(*) AS cnt").
		Where("activity_product.is_deleted = ?", model.NotDeleted).
		Where("activity_product.activity_id IN ?", activityIDs).
		Where("activity_product.enable_group_buy = 1").
		Where("activity_product.group_buy_price IS NOT NULL AND activity_product.group_buy_price > 0 AND activity_product.group_buy_target_count >= 2")
	if publicOnly {
		q = q.Joins("JOIN product ON product.id = activity_product.product_id AND product.is_deleted = ? AND product.status = ?",
			model.NotDeleted, model.ProductStatusOn).
			Where("activity_product.status = 1")
	}
	type row struct {
		ActivityID uint64
		Cnt        int64
	}
	var rows []row
	if err := q.Group("activity_id").Scan(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ActivityID] = r.Cnt
	}
	return out
}

func boolToUint8(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func (s *ActivityService) GetProductItemView(activityID, apID uint64, merchantID *uint64) (*ActivityProductItemView, error) {
	act, err := s.GetByID(activityID, merchantID)
	if err != nil {
		return nil, err
	}
	ap, err := s.GetProductInActivity(activityID, apID, merchantID)
	if err != nil {
		return nil, err
	}
	view := toActivityProductItemView(act, ap)
	views := []ActivityProductItemView{view}
	if err := s.enrichActivityProductItemViewsApplicable(views); err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (s *ActivityService) ListStoreProducts(activityID uint64, groupBuyOnly bool) ([]ActivityProductStoreView, error) {
	act, err := s.GetByID(activityID, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}

	var items []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ? AND status = ?", model.NotDeleted, model.ProductStatusOn).
		Where("activity_id = ? AND status = 1", activityID).
		Order("sort_order ASC, id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	views := make([]ActivityProductStoreView, 0, len(items))
	for i := range items {
		if items[i].Product == nil || items[i].Product.ID == 0 {
			continue
		}
		view := buildActivityProductStoreView(act, &items[i], items[i].Product)
		if groupBuyOnly && !view.CanGroupBuy {
			continue
		}
		views = append(views, view)
	}
	if err := s.enrichActivityProductStoreViewsApplicable(views); err != nil {
		return nil, err
	}
	return views, nil
}

func (s *ActivityService) GetStoreProduct(activityID, activityProductID uint64) (*ActivityProductStoreView, error) {
	return s.GetStoreProductForUser(activityID, activityProductID, nil)
}

// GetStoreProductForUser 活动商品详情；accountID 非空时按个人已购计算 remaining_qty / limit_reached。
func (s *ActivityService) GetStoreProductForUser(activityID, activityProductID uint64, accountID *uint64) (*ActivityProductStoreView, error) {
	act, err := s.GetByID(activityID, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	ap, err := s.GetActivityProduct(activityProductID, nil)
	if err != nil {
		return nil, err
	}
	if ap.ActivityID != activityID {
		return nil, ErrActivityProductNotFound
	}
	if ap.Status != 1 || ap.Product == nil || ap.Product.Status != model.ProductStatusOn {
		return nil, ErrActivityProductNotFound
	}
	view := buildActivityProductStoreView(act, ap, ap.Product)
	views := []ActivityProductStoreView{view}
	if err := s.enrichActivityProductStoreViewsApplicable(views); err != nil {
		return nil, err
	}
	view = views[0]
	if ap.Product.ItemType == model.ProductItemTypePackage {
		groups, err := (&ProductService{DB: s.DB}).LoadPackageGroups(ap.Product.ID)
		if err != nil {
			return nil, err
		}
		view.PackageGroups = groups
	}
	if err := s.enrichActivityProductLimits(&view, ap, accountID); err != nil {
		return nil, err
	}
	return &view, nil
}

func buildActivityProductStoreView(act *model.Activity, ap *model.ActivityProduct, p *model.Product) ActivityProductStoreView {
	avail := availableActivityStock(ap, p)
	canCoupon := act.EnableCoupon == 1 && ap.EnableCoupon == 1
	canGroup := activityProductCanGroupBuy(ap)

	cover := ""
	if p.CoverURL != "" {
		cover = p.CoverURL
	}
	deal := PurchaseOption{
		Available:    !canGroup && avail > 0,
		Price:        ap.ActivityPrice,
		CanUseCoupon: canCoupon,
	}
	groupPrice := ap.ActivityPrice
	if ap.GroupBuyPrice != nil {
		groupPrice = *ap.GroupBuyPrice
	}
	group := GroupPurchaseOption{
		PurchaseOption: PurchaseOption{
			Available:    canGroup && avail > 0,
			Price:        groupPrice,
			CanUseCoupon: canCoupon,
		},
		TargetCount:        ap.GroupBuyTargetCount,
		AllowRepeatJoin:    ap.GroupBuyAllowRepeat,
		MaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
	}

	return ActivityProductStoreView{
		ActivityProduct: *ap,
		MerchantID:      p.MerchantID,
		ProductName:     p.Name,
		ProductCover:    cover,
		OriginalPrice:   p.Price,
		ItemType:        p.ItemType,
		AvailableStock:  avail,
		CanGroupBuy:     canGroup,
		CanUseCoupon:    canCoupon,
		SaleOptions: ProductSaleOptions{
			Deal:  deal,
			Group: group,
		},
		LimitLabels:  buildSeckillLimitLabels(ap, act.UserMaxQty, act.UserDailyMax),
		RemainingQty: avail,
	}
}

func (s *ActivityService) enrichActivityProductStoreViewsApplicable(views []ActivityProductStoreView) error {
	if len(views) == 0 {
		return nil
	}
	productIDs := make([]uint64, 0, len(views))
	for i := range views {
		productIDs = append(productIDs, views[i].ProductID)
	}
	briefsMap, idsMap, err := (&ProductService{DB: s.DB}).loadApplicableMerchantBriefs(productIDs)
	if err != nil {
		return err
	}
	for i := range views {
		pid := views[i].ProductID
		views[i].ApplicableMerchantIDs = idsMap[pid]
		views[i].ApplicableMerchants = briefsMap[pid]
	}
	return nil
}

func (s *ActivityService) enrichActivityProductItemViewsApplicable(views []ActivityProductItemView) error {
	if len(views) == 0 {
		return nil
	}
	productIDs := make([]uint64, 0, len(views))
	for i := range views {
		productIDs = append(productIDs, views[i].ProductID)
	}
	briefsMap, idsMap, err := (&ProductService{DB: s.DB}).loadApplicableMerchantBriefs(productIDs)
	if err != nil {
		return err
	}
	for i := range views {
		pid := views[i].ProductID
		views[i].ApplicableMerchantIDs = idsMap[pid]
		views[i].ApplicableMerchants = briefsMap[pid]
	}
	return nil
}

// enrichActivityProductLimits 写入限购标签与本单剩余可买件数（各限购剩余取最小）。
func (s *ActivityService) enrichActivityProductLimits(view *ActivityProductStoreView, ap *model.ActivityProduct, accountID *uint64) error {
	actLimits, err := loadActivityUserLimitConfig(s.DB, ap.ActivityID)
	if err != nil {
		return err
	}
	view.LimitLabels = buildSeckillLimitLabels(ap, actLimits.UserMaxQty, actLimits.UserDailyMax)
	view.LimitReached = false
	view.LimitReason = ""

	now := time.Now()
	var createdAt time.Time
	if accountID != nil && *accountID > 0 {
		var account model.Account
		if err := query.NotDeleted(s.DB).Select("id", "created_at").First(&account, *accountID).Error; err != nil {
			accountID = nil
		} else {
			createdAt = account.CreatedAt
		}
	}

	remain, err := computeActivityRemaining(s.DB, ap, view.AvailableStock, accountID, createdAt, now)
	if err != nil {
		return err
	}
	view.RemainingQty = remain.RemainingQty
	view.LimitReached = remain.LimitReached
	view.LimitReason = remain.LimitReason
	return nil
}

func productSellableUnits(p *model.Product) uint32 {
	if p == nil {
		return 0
	}
	best := p.Stock
	if p.DealStock > best {
		best = p.DealStock
	}
	if p.GroupStock > best {
		best = p.GroupStock
	}
	if p.TakeoutStock > best {
		best = p.TakeoutStock
	}
	return best
}

func availableActivityStock(ap *model.ActivityProduct, product *model.Product) uint32 {
	base := productSellableUnits(product)
	if ap.ActivityStock == 0 {
		return base
	}
	remain := uint32(0)
	if ap.ActivityStock > ap.SoldCount {
		remain = ap.ActivityStock - ap.SoldCount
	}
	// 商品通道库存均为 0 时，仅以活动库存为准（避免遗留 stock=0 误杀）
	if base == 0 {
		return remain
	}
	if base < remain {
		return base
	}
	return remain
}

func (s *ActivityService) ResolveForOrder(accountID uint64, activityProductID uint64, merchantID uint64, quantity uint32, purchaseType uint8) (*ActivityOrderContext, error) {
	var ap model.ActivityProduct
	if err := query.NotDeleted(s.DB).Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("id = ?", activityProductID).First(&ap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityProductNotFound
		}
		return nil, err
	}

	act, err := s.GetByID(ap.ActivityID, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	if ap.Status != 1 {
		return nil, ErrActivityProductNotFound
	}
	if ap.Product == nil || ap.Product.Status != model.ProductStatusOn {
		return nil, ErrProductNotFound
	}
	// 入口店面须为商品所属店或适用店；订单 merchant_id 记商品 owner（见 OrderService.Create）
	if merchantID != 0 && ap.Product.MerchantID != merchantID {
		if err := (&ProductService{DB: s.DB}).AssertMerchantApplicable(ap.Product.ID, merchantID); err != nil {
			return nil, ErrActivityForbidden
		}
	}
	// 商家专场活动须与商品所属商家一致；平台活动 merchant_id=0 不额外限制入口店
	if act.MerchantID != 0 && act.MerchantID != ap.Product.MerchantID {
		return nil, ErrActivityForbidden
	}

	product := *ap.Product
	avail := availableActivityStock(&ap, &product)
	if quantity == 0 {
		quantity = 1
	}
	if avail < quantity {
		return nil, ErrInsufficientStock
	}

	if err := s.checkUserLimits(s.DB, accountID, &ap, quantity); err != nil {
		return nil, err
	}

	unitPrice := ap.ActivityPrice
	enableCoupon := act.EnableCoupon == 1 && ap.EnableCoupon == 1
	var gbConfig *ActivityGroupBuyConfig

	if purchaseType == model.PurchaseTypeGroup {
		if ap.EnableGroupBuy != 1 || ap.GroupBuyPrice == nil {
			return nil, ErrGroupBuyInvalid
		}
		target := uint32(2)
		if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
			target = *ap.GroupBuyTargetCount
		}
		maxJoins := ap.GroupBuyMaxJoinsPerUser
		unitPrice = *ap.GroupBuyPrice
		gbConfig = &ActivityGroupBuyConfig{
			EnableGroupBuy:             1,
			GroupBuyPrice:              *ap.GroupBuyPrice,
			GroupBuyTargetCount:        target,
			GroupBuyAllowRepeat:        ap.GroupBuyAllowRepeat,
			GroupBuyMaxJoinsPerUser:    maxJoins,
			GroupBuyMaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
		}
	}

	return &ActivityOrderContext{
		Activity: act, ActivityProduct: &ap, Product: product,
		UnitPrice: unitPrice, EnableCoupon: enableCoupon, GroupBuyConfig: gbConfig,
	}, nil
}

// checkUserLimits enforces per-user qty / calendar / register windows against db.
// Pass the transaction handle inside OrderService.Create to close the TOCTOU window
// between ResolveForOrder's pre-check and order insert.
func (s *ActivityService) checkUserLimits(db *gorm.DB, accountID uint64, ap *model.ActivityProduct, quantity uint32) error {
	if db == nil {
		db = s.DB
	}
	now := time.Now()
	var account model.Account
	if err := query.NotDeleted(db).Select("id", "created_at").First(&account, accountID).Error; err != nil {
		return err
	}

	if ap.RegisterHours > 0 && !inRegisterWindow(account.CreatedAt, now, ap.RegisterHours) {
		return ErrActivityRegisterWindow
	}

	stock := ^uint32(0)
	if ap.Product != nil {
		stock = availableActivityStock(ap, ap.Product)
	}
	aid := accountID
	remain, err := computeActivityRemaining(db, ap, stock, &aid, account.CreatedAt, now)
	if err != nil {
		return err
	}
	if remain.LimitReached || quantity > remain.RemainingQty {
		return ErrActivityLimitExceeded
	}
	return nil
}

func (s *ActivityService) CreditSoldInTx(tx *gorm.DB, activityProductID uint64, quantity uint32) (platformBucket *string, err error) {
	if activityProductID == 0 {
		return nil, nil
	}
	ap, err := lockActivityProduct(tx, activityProductID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if _, err := ensurePlatformDailyBucketLocked(tx, ap, now); err != nil {
		return nil, err
	}
	bucket, err := creditPlatformDailyLocked(tx, ap, quantity)
	if err != nil {
		return nil, err
	}
	if ap.ActivityStock > 0 {
		result := tx.Model(&model.ActivityProduct{}).
			Where("id = ? AND sold_count + ? <= activity_stock", activityProductID, quantity).
			Update("sold_count", gorm.Expr("sold_count + ?", quantity))
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, ErrInsufficientStock
		}
	}
	return bucket, nil
}

// ReholdAfterOrderRestoreInTx 关单后补入账恢复订单时：重占活动已售，并按有效订单对账平台日限。
// 不可再走 CreditSoldInTx：日限对账已含本单，再 credit 会重复占用。
func (s *ActivityService) ReholdAfterOrderRestoreInTx(tx *gorm.DB, orderID uint64) error {
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ? AND activity_product_id IS NOT NULL", orderID).Find(&items).Error; err != nil {
		return err
	}
	seen := make(map[uint64]struct{})
	now := time.Now()
	for _, it := range items {
		if it.ActivityProductID == nil || it.Quantity == 0 {
			continue
		}
		apID := *it.ActivityProductID
		ap, err := lockActivityProduct(tx, apID)
		if err != nil {
			return err
		}
		if _, done := seen[apID]; !done {
			seen[apID] = struct{}{}
			if _, err := ensurePlatformDailyBucketLocked(tx, ap, now); err != nil {
				return err
			}
		}
		if ap.ActivityStock > 0 {
			res := tx.Model(&model.ActivityProduct{}).
				Where("id = ? AND sold_count + ? <= activity_stock", apID, it.Quantity).
				Update("sold_count", gorm.Expr("sold_count + ?", it.Quantity))
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrInsufficientStock
			}
		}
	}
	return nil
}

func (s *ActivityService) RollbackSoldInTx(tx *gorm.DB, orderID uint64) error {
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ? AND activity_product_id IS NOT NULL", orderID).Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		if it.ActivityProductID == nil || it.Quantity == 0 {
			continue
		}
		ap, err := lockActivityProduct(tx, *it.ActivityProductID)
		if err != nil {
			return err
		}
		// 与 CreditSoldInTx 对称：仅 activity_stock>0 时才记/回滚 sold_count；
		// 不限库存活动只占用平台日限，硬扣 sold_count 会在 0 上 RowsAffected=0 导致取消 500。
		if ap.ActivityStock > 0 {
			if err := tx.Model(&model.ActivityProduct{}).
				Where("id = ?", *it.ActivityProductID).
				Update("sold_count", gorm.Expr("GREATEST(0, CAST(sold_count AS SIGNED) - ?)", it.Quantity)).Error; err != nil {
				return err
			}
		}
		if it.PlatformDailyBucket != nil && *it.PlatformDailyBucket != "" {
			if _, err := ensurePlatformDailyBucketLocked(tx, ap, time.Now()); err != nil {
				return err
			}
			if err := rollbackPlatformDailyLocked(tx, ap, it.Quantity, *it.PlatformDailyBucket); err != nil {
				return err
			}
		}
	}
	return nil
}

// RollbackSoldQtyOnRefundInTx 背包退款时按件数回滚活动已售（支持部分退）。
func (s *ActivityService) RollbackSoldQtyOnRefundInTx(tx *gorm.DB, orderID, productID uint64, quantity uint32) error {
	if orderID == 0 || productID == 0 || quantity == 0 {
		return nil
	}
	var items []model.OrderItem
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND product_id = ? AND activity_product_id IS NOT NULL", orderID, productID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return err
	}
	need := quantity
	for i := range items {
		it := &items[i]
		if need == 0 || it.ActivityProductID == nil || it.Quantity == 0 {
			continue
		}
		take := it.Quantity
		if take > need {
			take = need
		}
		ap, err := lockActivityProduct(tx, *it.ActivityProductID)
		if err != nil {
			return err
		}
		// 与 CreditSoldInTx / RollbackSoldInTx 对称：不限量活动不记 sold_count
		if ap.ActivityStock > 0 {
			if err := tx.Model(&model.ActivityProduct{}).
				Where("id = ?", *it.ActivityProductID).
				Update("sold_count", gorm.Expr("GREATEST(0, CAST(sold_count AS SIGNED) - ?)", take)).Error; err != nil {
				return err
			}
		}
		need -= take
	}
	return nil
}

// RollbackPlatformDailyOnRefundInTx 背包退款时按件数释放平台日限（仅同桶）。
func (s *ActivityService) RollbackPlatformDailyOnRefundInTx(tx *gorm.DB, orderID, productID uint64, quantity uint32) error {
	if orderID == 0 || productID == 0 || quantity == 0 {
		return nil
	}
	var items []model.OrderItem
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND product_id = ? AND activity_product_id IS NOT NULL", orderID, productID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return err
	}
	need := quantity
	for i := range items {
		it := &items[i]
		if need == 0 || it.ActivityProductID == nil || it.PlatformDailyBucket == nil || *it.PlatformDailyBucket == "" {
			continue
		}
		take := it.Quantity
		if take > need {
			take = need
		}
		ap, err := lockActivityProduct(tx, *it.ActivityProductID)
		if err != nil {
			return err
		}
		if _, err := ensurePlatformDailyBucketLocked(tx, ap, time.Now()); err != nil {
			return err
		}
		if err := rollbackPlatformDailyLocked(tx, ap, take, *it.PlatformDailyBucket); err != nil {
			return err
		}
		need -= take
	}
	return nil
}

func validateActivityInput(input ActivityInput) error {
	// MerchantID=0 表示平台跨店活动；>0 为商家专场
	if strings.TrimSpace(input.Name) == "" {
		return ErrInvalidProductArg
	}
	if !input.EndAt.After(input.StartAt) {
		return ErrInvalidProductArg
	}
	if _, err := NormalizeDailyRefreshTime(input.UserDailyRefreshTime); err != nil {
		return fmt.Errorf("%w: user_daily_refresh_time 格式无效", ErrInvalidProductArg)
	}
	return nil
}

func validateActivityProductInput(input ActivityProductInput) error {
	if input.ProductID == 0 || input.ActivityPrice <= 0 {
		return ErrInvalidProductArg
	}
	if input.PlatformDailyMax > 1_000_000 {
		return fmt.Errorf("%w: platform_daily_max 过大", ErrInvalidProductArg)
	}
	if _, err := NormalizeDailyRefreshTime(input.DailyRefreshTime); err != nil {
		return err
	}
	if _, err := NormalizeDailyRefreshTime(input.WeeklyRefreshTime); err != nil {
		return fmt.Errorf("%w: weekly_refresh_time 格式无效", ErrInvalidProductArg)
	}
	if _, err := NormalizeDailyRefreshTime(input.MonthlyRefreshTime); err != nil {
		return fmt.Errorf("%w: monthly_refresh_time 格式无效", ErrInvalidProductArg)
	}
	if _, err := normalizeWeeklyWeekday(input.WeeklyRefreshWeekday); err != nil {
		return err
	}
	if _, err := normalizeMonthlyDay(input.MonthlyRefreshDay); err != nil {
		return err
	}
	if input.EnableGroupBuy == 1 {
		if input.GroupBuyPrice == nil || *input.GroupBuyPrice <= 0 {
			return ErrInvalidProductArg
		}
		if input.GroupBuyTargetCount == nil || *input.GroupBuyTargetCount < 2 {
			return ErrInvalidProductArg
		}
	}
	return validateBargainOnActivityProduct(normalizeBargainInput(input))
}

func normalizeWeeklyWeekday(v uint8) (uint8, error) {
	if v == 0 {
		return 1, nil
	}
	if v < 1 || v > 7 {
		return 0, fmt.Errorf("%w: weekly_refresh_weekday", ErrInvalidProductArg)
	}
	return v, nil
}

func normalizeMonthlyDay(v uint8) (uint8, error) {
	if v == 0 {
		return 1, nil
	}
	if v < 1 || v > 31 {
		return 0, fmt.Errorf("%w: monthly_refresh_day", ErrInvalidProductArg)
	}
	return v, nil
}
