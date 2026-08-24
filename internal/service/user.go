package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type UserService struct {
	DB *gorm.DB
}

type UserStats struct {
	OrderCount           int64                    `json:"order_count"`
	CartCount            int64                    `json:"cart_count"`
	CouponUnusedCount    int64                    `json:"coupon_unused_count"`
	AddressCount         int64                    `json:"address_count"`
	InventoryKindCount   int64                    `json:"inventory_kind_count"`
	InventoryTotalQty    int64                    `json:"inventory_total_qty"`
	BargainOngoingCount  int64                    `json:"bargain_ongoing_count"`
	OrderBadges          UserOrderBadges          `json:"order_badges"`
	DeliveryBadges       UserDeliveryBadges       `json:"delivery_badges"`
	InventoryUsageBadges UserInventoryUsageBadges `json:"inventory_usage_badges"`
}

// UserOrderBadges 个人中心订单角标，与 GET /user/orders?status_code= 筛选一致
type UserOrderBadges struct {
	PendingPay         int64 `json:"pending_pay"` // 待支付（购买单 + 外卖单）
	PendingGroup       int64 `json:"pending_group"`
	PendingMerchant    int64 `json:"pending_merchant"`
	Approved           int64 `json:"approved"`
	PendingUseMerchant int64 `json:"pending_use_merchant"`
	ReadyPickup        int64 `json:"ready_pickup"`
	PendingRider       int64 `json:"pending_rider"`
	Delivering         int64 `json:"delivering"`
	PendingConfirm     int64 `json:"pending_confirm"`
	Completed          int64 `json:"completed"`
	Cancelled          int64 `json:"cancelled"`
	GroupFailed        int64 `json:"group_failed"`
}

// UserDeliveryBadges 配送单角标，与 GET /user/deliveries?scope= 一致
type UserDeliveryBadges struct {
	Active         int64 `json:"active"`
	PendingConfirm int64 `json:"pending_confirm"`
	Appealing      int64 `json:"appealing"` // 用户申诉中（配送异常待管理员处理）
	Completed      int64 `json:"completed"`
	Total          int64 `json:"total"`
}

// UserInventoryUsageBadges 背包使用记录角标
type UserInventoryUsageBadges struct {
	PendingVerify int64 `json:"pending_verify"`
	PendingShip   int64 `json:"pending_ship"`
	CancelPending int64 `json:"cancel_pending"`
	Completed     int64 `json:"completed"`
	Total         int64 `json:"total"`
}

type ProfileDetail struct {
	Account   model.Account       `json:"account"`
	Profile   *model.UserProfile  `json:"profile,omitempty"`
	Addresses []model.UserAddress `json:"addresses"`
	Stats     UserStats           `json:"stats"`
}

type OverviewResponse struct {
	Account model.Account      `json:"account"`
	Profile *model.UserProfile `json:"profile,omitempty"`
	Stats   UserStats          `json:"stats"`
}

type CartItemView struct {
	model.CartItem
	UnitPrice    float64  `json:"unit_price"`
	GroupPrice   *float64 `json:"group_price,omitempty"`
	Subtotal     float64  `json:"subtotal"`
	CanGroupBuy  bool     `json:"can_group_buy"`
	CanUseCoupon bool     `json:"can_use_coupon"`
	GroupBuyID   *uint64  `json:"group_buy_id,omitempty"`
	ShopName     string   `json:"shop_name,omitempty"`
	HasOptions   bool     `json:"has_options"`
}

func (s *UserService) GetOverview(accountID uint64) (*OverviewResponse, error) {
	account, profile, err := s.loadAccountAndProfile(accountID)
	if err != nil {
		return nil, err
	}
	stats, err := s.buildStats(accountID)
	if err != nil {
		return nil, err
	}
	return &OverviewResponse{Account: *account, Profile: profile, Stats: *stats}, nil
}

func (s *UserService) GetProfile(accountID uint64) (*ProfileDetail, error) {
	account, profile, err := s.loadAccountAndProfile(accountID)
	if err != nil {
		return nil, err
	}
	stats, err := s.buildStats(accountID)
	if err != nil {
		return nil, err
	}
	var addresses []model.UserAddress
	if err := query.NotDeleted(s.DB).Where("account_id = ?", accountID).Order("is_default DESC, id DESC").Find(&addresses).Error; err != nil {
		return nil, err
	}
	return &ProfileDetail{
		Account:   *account,
		Profile:   profile,
		Addresses: addresses,
		Stats:     *stats,
	}, nil
}

// ListUsersForAdmin 管理端账号列表（默认含普通用户、商家、管理员；含已删除）。
func (s *UserService) ListUsersForAdmin(page, pageSize int, keyword string) ([]model.Account, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	q := s.DB.Model(&model.Account{})
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		if id, err := strconv.ParseUint(keyword, 10, 64); err == nil {
			// 短数字按 ID 精确查（便于找 7/8/9 等）；长数字兼搜手机号。
			if len(keyword) <= 6 {
				q = q.Where("id = ?", id)
			} else {
				like := "%" + keyword + "%"
				q = q.Where("id = ? OR phone LIKE ?", id, like)
			}
		} else {
			like := "%" + keyword + "%"
			q = q.Where("nickname LIKE ? OR phone LIKE ? OR CAST(id AS CHAR) LIKE ?", like, like, like)
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Account
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// AdminUserView 管理端用户展示。
type AdminUserView struct {
	model.Account
	StatusText string `json:"status_text"`
	GenderText string `json:"gender_text"`
	TypeText   string `json:"type_text"`
	IsDeleted  bool   `json:"is_deleted"`
}

func toAdminUserView(a model.Account) AdminUserView {
	gender := "未知"
	switch a.Gender {
	case 1:
		gender = "男"
	case 2:
		gender = "女"
	}
	return AdminUserView{
		Account:    a,
		StatusText: model.AccountStatusText(a.Status),
		GenderText: gender,
		TypeText:   model.AccountTypeText(a.Type),
		IsDeleted:  a.IsDeleted == model.Deleted,
	}
}

func ToAdminUserViews(list []model.Account) []AdminUserView {
	out := make([]AdminUserView, 0, len(list))
	for i := range list {
		out = append(out, toAdminUserView(list[i]))
	}
	return out
}

// SetUserStatusForAdmin 拉黑(0) / 解除拉黑(1)。仅普通用户。
func (s *UserService) SetUserStatusForAdmin(accountID uint64, status uint8) (*AdminUserView, error) {
	if status != model.AccountStatusActive && status != model.AccountStatusDisabled {
		return nil, fmt.Errorf("status 仅支持 0（拉黑）或 1（正常）")
	}
	var acc model.Account
	if err := query.NotDeleted(s.DB).Where("id = ? AND type = ?", accountID, model.AccountTypeUser).
		First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	if err := s.DB.Model(&acc).Update("status", status).Error; err != nil {
		return nil, err
	}
	acc.Status = status
	view := toAdminUserView(acc)
	return &view, nil
}

func (s *UserService) ListOrders(accountID uint64, page query.Page, status *uint8, statusCode string) (*query.PageResult, error) {
	_, pageSize, offset := page.Normalize()
	q := query.NotDeleted(s.DB.Model(&model.Order{})).Where("account_id = ?", accountID)
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	applyStatusCodeFilter(q, statusCode)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var orders []model.Order
	if err := q.Preload("Items", "is_deleted = ?", model.NotDeleted).Order("id DESC").Offset(offset).Limit(pageSize).Find(&orders).Error; err != nil {
		return nil, err
	}
	views := make([]OrderView, 0, len(orders))
	for _, o := range orders {
		views = append(views, OrderView{
			Order: o, StatusText: model.OrderStatusDisplayText(o.Status, o.MerchantReviewStage),
			StatusCode: model.OrderStatusCode(o.Status, o.MerchantReviewStage),
		})
	}
	pageNum, _, _ := page.Normalize()
	return &query.PageResult{List: views, Total: total, Page: pageNum, PageSize: pageSize}, nil
}

func (s *UserService) GetOrder(accountID, orderID uint64) (*OrderView, error) {
	var order model.Order
	if err := query.NotDeleted(s.DB).Preload("Items", "is_deleted = ?", model.NotDeleted).Where("id = ? AND account_id = ?", orderID, accountID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		return nil, err
	}
	return &OrderView{
		Order: order, StatusText: model.OrderStatusDisplayText(order.Status, order.MerchantReviewStage),
		StatusCode: model.OrderStatusCode(order.Status, order.MerchantReviewStage),
	}, nil
}

func (s *UserService) ListCart(accountID uint64) ([]CartItemView, error) {
	var items []model.CartItem
	if err := query.NotDeleted(s.DB).Preload("Product", "is_deleted = ?", model.NotDeleted).Where("account_id = ?", accountID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	merchantIDs := make([]uint64, 0)
	seenMid := map[uint64]struct{}{}
	for _, item := range items {
		mid := item.Product.MerchantID
		if mid == 0 {
			continue
		}
		if _, ok := seenMid[mid]; ok {
			continue
		}
		seenMid[mid] = struct{}{}
		merchantIDs = append(merchantIDs, mid)
	}
	shopNames := map[uint64]string{}
	if len(merchantIDs) > 0 {
		var merchants []model.MerchantProfile
		_ = query.NotDeleted(s.DB).Select("id", "shop_name").Where("id IN ?", merchantIDs).Find(&merchants)
		for _, m := range merchants {
			shopNames[m.ID] = m.ShopName
		}
	}
	views := make([]CartItemView, 0, len(items))
	for _, item := range items {
		var gb *model.GroupBuy
		if item.Product.EnableGroupBuy == 1 {
			var g model.GroupBuy
			if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", item.ProductID).First(&g).Error; err == nil {
				gb = &g
			}
		}
		sale := buildProductStoreView(item.Product, gb)

		hasOpts, _ := ProductNeedsOptions(s.DB, item.ProductID)
		view := CartItemView{
			CartItem:     item,
			UnitPrice:    item.Product.Price,
			CanGroupBuy:  sale.CanGroupBuy,
			CanUseCoupon: sale.CanUseCoupon,
			GroupBuyID:   sale.GroupBuyID,
			ShopName:     shopNames[item.Product.MerchantID],
			HasOptions:   hasOpts,
		}
		if item.PurchaseType == model.PurchaseTypeGroup {
			if item.Product.GroupBuyPrice != nil {
				view.GroupPrice = item.Product.GroupBuyPrice
				view.UnitPrice = *item.Product.GroupBuyPrice
			} else if item.GroupBuyID != nil {
				if gb != nil {
					view.GroupPrice = &gb.GroupPrice
					view.UnitPrice = gb.GroupPrice
				}
			} else if sale.SaleOptions.Group.Available {
				view.GroupPrice = &sale.SaleOptions.Group.Price
				view.UnitPrice = sale.SaleOptions.Group.Price
			}
		}
		view.Subtotal = view.UnitPrice * float64(item.Quantity)
		views = append(views, view)
	}
	return views, nil
}

func (s *UserService) ListCoupons(accountID uint64, status *uint8) (map[string]interface{}, error) {
	now := time.Now()
	_ = query.NotDeleted(s.DB.Model(&model.UserCoupon{})).
		Where("account_id = ? AND status = ? AND expired_at < ?", accountID, model.UserCouponStatusUnused, now).
		Update("status", model.UserCouponStatusExpired).Error

	q := query.NotDeleted(s.DB).Preload("Coupon", "is_deleted = ?", model.NotDeleted).Where("account_id = ?", accountID)
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var unusedCount int64
	query.NotDeleted(s.DB.Model(&model.UserCoupon{})).Where("account_id = ? AND status = ?", accountID, model.UserCouponStatusUnused).Count(&unusedCount)
	var list []model.UserCoupon
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"unused_count": unusedCount,
		"list":         list,
	}, nil
}

func (s *UserService) ListInventory(accountID uint64) (map[string]interface{}, error) {
	var items []model.UserInventory
	if err := query.NotDeleted(s.DB).Preload("Product", "is_deleted = ?", model.NotDeleted).Where("account_id = ? AND quantity > 0", accountID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	merchantIDs := make([]uint64, 0, len(items))
	seen := map[uint64]struct{}{}
	for _, it := range items {
		mid := it.Product.MerchantID
		if mid == 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		merchantIDs = append(merchantIDs, mid)
	}
	shopNames := map[uint64]string{}
	if len(merchantIDs) > 0 {
		var merchants []model.MerchantProfile
		_ = query.NotDeleted(s.DB).Select("id", "shop_name").Where("id IN ?", merchantIDs).Find(&merchants)
		for _, m := range merchants {
			shopNames[m.ID] = m.ShopName
		}
	}
	var totalQty int64
	list := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		totalQty += int64(it.Quantity)
		mid := it.Product.MerchantID
		hasOptions := false
		if it.Product.ItemType == model.ProductItemTypePackage {
			if needs, err := packageNeedsChildOptions(s.DB, it.ProductID, nil); err == nil {
				hasOptions = needs
			}
		} else if needs, err := ProductNeedsOptions(s.DB, it.ProductID); err == nil {
			hasOptions = needs
		}
		list = append(list, map[string]interface{}{
			"id": it.ID, "account_id": it.AccountID, "product_id": it.ProductID,
			"spec": it.Spec, "quantity": it.Quantity, "last_order_id": it.LastOrderID,
			"created_at": it.CreatedAt, "updated_at": it.UpdatedAt,
			"product": it.Product, "merchant_id": mid, "shop_name": shopNames[mid],
			"has_options": hasOptions,
		})
	}
	return map[string]interface{}{
		"kind_count": len(items),
		"total_qty":  totalQty,
		"list":       list,
	}, nil
}

func (s *UserService) loadAccountAndProfile(accountID uint64) (*model.Account, *model.UserProfile, error) {
	var account model.Account
	if err := query.NotDeleted(s.DB).First(&account, accountID).Error; err != nil {
		return nil, nil, err
	}
	var profile model.UserProfile
	err := query.NotDeleted(s.DB).Where("account_id = ?", accountID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &account, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &account, &profile, nil
}

func (s *UserService) buildStats(accountID uint64) (*UserStats, error) {
	stats := &UserStats{}
	if err := query.NotDeleted(s.DB.Model(&model.Order{})).Where("account_id = ?", accountID).Count(&stats.OrderCount).Error; err != nil {
		return nil, err
	}
	if err := query.NotDeleted(s.DB.Model(&model.CartItem{})).Where("account_id = ?", accountID).Count(&stats.CartCount).Error; err != nil {
		return nil, err
	}
	if err := query.NotDeleted(s.DB.Model(&model.UserCoupon{})).Where("account_id = ? AND status = ?", accountID, model.UserCouponStatusUnused).Count(&stats.CouponUnusedCount).Error; err != nil {
		return nil, err
	}
	if err := query.NotDeleted(s.DB.Model(&model.UserAddress{})).Where("account_id = ?", accountID).Count(&stats.AddressCount).Error; err != nil {
		return nil, err
	}
	type invAgg struct {
		KindCount int64
		TotalQty  int64
	}
	var agg invAgg
	if err := query.NotDeleted(s.DB.Model(&model.UserInventory{})).
		Select("COUNT(*) AS kind_count, COALESCE(SUM(quantity), 0) AS total_qty").
		Where("account_id = ? AND quantity > 0", accountID).
		Scan(&agg).Error; err != nil {
		return nil, err
	}
	stats.InventoryKindCount = agg.KindCount
	stats.InventoryTotalQty = agg.TotalQty

	if err := query.NotDeleted(s.DB.Model(&model.BargainSession{})).
		Where("initiator_account_id = ? AND status = ? AND expire_at > ?",
			accountID, model.BargainStatusOngoing, time.Now()).
		Count(&stats.BargainOngoingCount).Error; err != nil {
		return nil, err
	}

	orderBadges, err := buildUserOrderBadges(s.DB, accountID)
	if err != nil {
		return nil, err
	}
	stats.OrderBadges = *orderBadges

	deliveryBadges, err := buildUserDeliveryBadges(s.DB, accountID)
	if err != nil {
		return nil, err
	}
	stats.DeliveryBadges = *deliveryBadges

	usageBadges, err := buildUserInventoryUsageBadges(s.DB, accountID)
	if err != nil {
		return nil, err
	}
	stats.InventoryUsageBadges = *usageBadges

	return stats, nil
}

func buildUserOrderBadges(db *gorm.DB, accountID uint64) (*UserOrderBadges, error) {
	badges := &UserOrderBadges{}
	codes := []struct {
		code  string
		field *int64
	}{
		{"pending_pay", &badges.PendingPay},
		{"pending_group", &badges.PendingGroup},
		{"pending_merchant", &badges.PendingMerchant},
		{"approved", &badges.Approved},
		{"pending_use_merchant", &badges.PendingUseMerchant},
		{"ready_pickup", &badges.ReadyPickup},
		{"pending_rider", &badges.PendingRider},
		{"delivering", &badges.Delivering},
		{"completed", &badges.Completed},
	}
	for _, item := range codes {
		count, err := countUserOrdersByStatusCode(db, accountID, item.code)
		if err != nil {
			return nil, err
		}
		*item.field = count
	}
	// 外卖待支付一并计入，供个人中心「全部」角标
	var takeoutPending int64
	if err := query.NotDeleted(db.Model(&model.TakeoutOrder{})).
		Where("account_id = ? AND status = ? AND pay_status = ?",
			accountID, model.TakeoutStatusPendingPay, model.PayStatusUnpaid).
		Count(&takeoutPending).Error; err != nil {
		return nil, err
	}
	badges.PendingPay += takeoutPending
	if err := query.NotDeleted(db.Model(&model.Order{})).
		Where("account_id = ? AND status = ?", accountID, model.OrderStatusPendingConfirm).
		Count(&badges.PendingConfirm).Error; err != nil {
		return nil, err
	}
	if err := query.NotDeleted(db.Model(&model.Order{})).
		Where("account_id = ? AND status = ?", accountID, model.OrderStatusCancelled).
		Count(&badges.Cancelled).Error; err != nil {
		return nil, err
	}
	if err := query.NotDeleted(db.Model(&model.Order{})).
		Where("account_id = ? AND status = ?", accountID, model.OrderStatusGroupFailed).
		Count(&badges.GroupFailed).Error; err != nil {
		return nil, err
	}
	return badges, nil
}

func countUserOrdersByStatusCode(db *gorm.DB, accountID uint64, statusCode string) (int64, error) {
	q := query.NotDeleted(db.Model(&model.Order{})).Where("account_id = ?", accountID)
	applyStatusCodeFilter(q, statusCode)
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func userDeliveryBaseQuery(db *gorm.DB, accountID uint64) *gorm.DB {
	// 与 DeliveryService.userDeliveryQuery 一致：购买单 / 背包跑腿 / 外卖单
	return query.NotDeleted(db.Model(&model.DeliveryOrder{})).Where(
		`order_id IN (SELECT id FROM `+"`order`"+` WHERE account_id = ? AND is_deleted = 0)
		OR inventory_usage_id IN (SELECT id FROM user_inventory_usage WHERE account_id = ? AND is_deleted = 0)
		OR id IN (SELECT delivery_order_id FROM user_inventory_usage WHERE account_id = ? AND delivery_order_id IS NOT NULL AND is_deleted = 0)
		OR takeout_order_id IN (SELECT id FROM takeout_order WHERE account_id = ? AND is_deleted = 0)`,
		accountID, accountID, accountID, accountID,
	)
}

func buildUserDeliveryBadges(db *gorm.DB, accountID uint64) (*UserDeliveryBadges, error) {
	badges := &UserDeliveryBadges{}
	base := userDeliveryBaseQuery(db, accountID)
	if err := base.Count(&badges.Total).Error; err != nil {
		return nil, err
	}
	if err := userDeliveryBaseQuery(db, accountID).Where("status IN ?", []int{
		int(model.DeliveryPendingAdminReview),
		int(model.DeliveryPendingAccept),
		int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering),
	}).Count(&badges.Active).Error; err != nil {
		return nil, err
	}
	// 外卖配餐中尚无配送单，计入「配送中」角标，避免用户下单后角标为 0
	var preparingTakeout int64
	if err := query.NotDeleted(db.Model(&model.TakeoutOrder{})).
		Where("account_id = ? AND status = ? AND pay_status = ?",
			accountID, model.TakeoutStatusPreparing, model.PayStatusPaid).
		Count(&preparingTakeout).Error; err != nil {
		return nil, err
	}
	badges.Active += preparingTakeout
	if err := userDeliveryBaseQuery(db, accountID).
		Where("status = ? AND user_confirmed = ?", model.DeliveryDelivered, 0).
		Count(&badges.PendingConfirm).Error; err != nil {
		return nil, err
	}
	if err := userDeliveryBaseQuery(db, accountID).
		Where("status = ?", model.DeliveryException).
		Count(&badges.Appealing).Error; err != nil {
		return nil, err
	}
	if err := userDeliveryBaseQuery(db, accountID).
		Where("status = ?", model.DeliveryConfirmed).
		Count(&badges.Completed).Error; err != nil {
		return nil, err
	}
	return badges, nil
}

func buildUserInventoryUsageBadges(db *gorm.DB, accountID uint64) (*UserInventoryUsageBadges, error) {
	badges := &UserInventoryUsageBadges{}
	base := query.NotDeleted(db.Model(&model.UserInventoryUsage{})).Where("account_id = ?", accountID)
	if err := base.Count(&badges.Total).Error; err != nil {
		return nil, err
	}
	counts := []struct {
		status uint8
		field  *int64
	}{
		{model.InventoryUsagePendingVerify, &badges.PendingVerify},
		{model.InventoryUsagePendingShip, &badges.PendingShip},
		{model.InventoryUsageCancelPending, &badges.CancelPending},
		{model.InventoryUsageCompleted, &badges.Completed},
	}
	for _, item := range counts {
		if err := query.NotDeleted(db.Model(&model.UserInventoryUsage{})).
			Where("account_id = ? AND status = ?", accountID, item.status).
			Count(item.field).Error; err != nil {
			return nil, err
		}
	}
	return badges, nil
}
