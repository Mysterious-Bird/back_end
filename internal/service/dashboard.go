package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type DashboardService struct {
	DB *gorm.DB
}

// 有效营业额/有效订单口径：履约已完成、用户无法再走平台自助退款的确定收益。
// 含：自提核销完成、配送确认收货、外卖完成。不含：仅已支付、待审核、在背包可退、进行中履约。

// invalidOrderStatusInts 热销榜等「已支付未失败」漏斗用（勿用于看板有效营业额）。
// 勿用 []uint8：GORM 会当成 binary。
var invalidOrderStatusInts = []int{
	int(model.OrderStatusPendingPay),
	int(model.OrderStatusPendingGroup),
	int(model.OrderStatusPreparing),
	int(model.OrderStatusCancelled),
	int(model.OrderStatusGroupFailed),
	int(model.OrderStatusRefunding),
	int(model.OrderStatusRefunded),
	int(model.OrderStatusClosed),
}

type DailyStat struct {
	Date        string  `json:"date"`
	OrderCount  int64   `json:"order_count"`
	SalesAmount float64 `json:"sales_amount"`
}

type ProductSalesRank struct {
	ProductID   uint64 `json:"product_id"`
	ProductName string `json:"product_name"`
	MerchantID  uint64 `json:"merchant_id,omitempty"`
	CoverURL    string `json:"cover_url,omitempty"`
	SalesCount  uint32 `json:"sales_count"`
}

type AdminDashboard struct {
	OrderCount                    int64              `json:"order_count"` // 全部订单数（含未完成）
	ValidOrderCount               int64              `json:"valid_order_count"`
	CompletedOrderCount           int64              `json:"completed_order_count"`
	VerificationCount             int64              `json:"verification_count"`
	PendingRiderApps              int64              `json:"pending_rider_apps"`
	PendingDeliveryExceptions     int64              `json:"pending_delivery_exceptions"`
	PendingBagDeliveryReviews     int64              `json:"pending_bag_delivery_reviews"`
	MerchantCount                 int64              `json:"merchant_count"`
	ProductCount                  int64              `json:"product_count"`
	LowStockProductCount          int64              `json:"low_stock_product_count"`
	TotalSales                    float64            `json:"total_sales"`
	UserCount                     int64              `json:"user_count"`
	OrderTrend                    []DailyStat        `json:"order_trend"`
	TopProducts                   []ProductSalesRank `json:"top_products"`
}

type MerchantDashboard struct {
	ProductCount           int64              `json:"product_count"`
	PendingOrderReview     int64              `json:"pending_order_review"`
	PendingUseReview       int64              `json:"pending_use_review"`
	PendingPreparingCount  int64              `json:"pending_preparing_count"`
	TodayVerificationCount int64              `json:"today_verification_count"`
	LowStockCount          int64              `json:"low_stock_count"`
	OrderTrend             []DailyStat        `json:"order_trend"`
	TopProducts            []ProductSalesRank `json:"top_products"`
	Sales                  SalesReport        `json:"sales"`
}

// SalesReport 销售额报表。
// ValidOrderCount / TotalSalesAmount / Completed* = 已核销（已确定收益）。
// Pending* = 已付款尚未履约完成（待核销 / 配送中 / 外卖配餐配送中等）。
type SalesReport struct {
	MerchantID           *uint64              `json:"merchant_id,omitempty"`
	MerchantName         string               `json:"merchant_name,omitempty"`
	ValidOrderCount      int64                `json:"valid_order_count"`
	TotalSalesAmount     float64              `json:"total_sales_amount"`
	VerificationCount    int64                `json:"verification_count"`
	CompletedItemCount   int64                `json:"completed_item_count"`
	CompletedSalesAmount float64              `json:"completed_sales_amount"`
	CompletedItems       []SalesCompletedItem `json:"completed_items"`
	PendingOrderCount    int64                `json:"pending_order_count"`
	PendingSalesAmount   float64              `json:"pending_sales_amount"`
	PendingItems         []SalesPendingItem   `json:"pending_items"`
	StartDate            string               `json:"start_date,omitempty"`
	EndDate              string               `json:"end_date,omitempty"`
}

// SalesCompletedItem 已确定收益的履约明细（自提核销 / 配送确认收货 / 外卖完成）。
type SalesCompletedItem struct {
	UsageID      uint64  `json:"usage_id,omitempty"`
	TakeoutID    uint64  `json:"takeout_id,omitempty"`
	ProductID    uint64  `json:"product_id"`
	ProductName  string  `json:"product_name"`
	Quantity     uint32  `json:"quantity"`
	UnitPrice    float64 `json:"unit_price"`
	Amount       float64 `json:"amount"`
	FulfillType  string  `json:"fulfill_type"`
	FulfillText  string  `json:"fulfill_text"`
	CompletedAt  string  `json:"completed_at"`
	PackageText  string  `json:"package_text,omitempty"`
	IsPackage    bool    `json:"is_package"`
	UserNickname string  `json:"user_nickname,omitempty"`
	UserPhone    string  `json:"user_phone,omitempty"`
	PurchaseType    uint8   `json:"purchase_type,omitempty"`
	ActivityID      *uint64 `json:"activity_id,omitempty"`
	SaleChannel     string  `json:"sale_channel,omitempty"`
	SaleChannelText string  `json:"sale_channel_text,omitempty"`
}

// SalesPendingItem 已付款未履约完成的明细。
type SalesPendingItem struct {
	Source       string  `json:"source"` // usage | takeout | inventory
	ID           uint64  `json:"id"`
	ProductID    uint64  `json:"product_id"`
	ProductName  string  `json:"product_name"`
	Quantity     uint32  `json:"quantity"`
	UnitPrice    float64 `json:"unit_price"`
	Amount       float64 `json:"amount"`
	PurchasedAt  string  `json:"purchased_at"`
	StatusText   string  `json:"status_text"`
	UserNickname string  `json:"user_nickname,omitempty"`
	UserPhone    string  `json:"user_phone,omitempty"`
	PurchaseType    uint8   `json:"purchase_type,omitempty"`
	ActivityID      *uint64 `json:"activity_id,omitempty"`
	SaleChannel     string  `json:"sale_channel,omitempty"`
	SaleChannelText string  `json:"sale_channel_text,omitempty"`
}

// ResolveSaleChannel 根据订单项购买类型与活动归属解析销售渠道。
// 返回 (channel, text)：channel 取值 deal / group / activity_deal / activity_group。
// 未知 purchaseType 一律按 deal 处理（前端钻取会兜底为团购）。
func ResolveSaleChannel(purchaseType uint8, activityID *uint64) (string, string) {
	hasAct := activityID != nil && *activityID > 0
	switch {
	case purchaseType == model.PurchaseTypeGroup && hasAct:
		return "activity_group", "互动拼团"
	case purchaseType == model.PurchaseTypeGroup:
		return "group", "拼团"
	case hasAct:
		return "activity_deal", "活动直购"
	default:
		return "deal", "团购"
	}
}

// orderItemSaleMeta 聚合订单项成交单价、购买类型与活动归属。
type orderItemSaleMeta struct {
	UnitPrice    float64
	PurchaseType uint8
	ActivityID   *uint64
}

type SalesReportFilter struct {
	MerchantID *uint64
	StartDate  *time.Time // inclusive, day start
	EndDate    *time.Time // exclusive, day after end
}

func (s *DashboardService) freshDB() *gorm.DB {
	// NewDB 切断与 s.DB 的 Statement 共享，避免并发/链式复用导致 Where 条件串台
	return s.DB.Session(&gorm.Session{NewDB: true})
}

func (s *DashboardService) Admin() (*AdminDashboard, error) {
	d := &AdminDashboard{}
	s.freshDB().Model(&model.Order{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.OrderCount)
	s.freshDB().Model(&model.Order{}).Where("is_deleted = ? AND status = ?", model.NotDeleted, model.OrderStatusCompleted).Count(&d.CompletedOrderCount)
	s.effectiveVerificationQuery(nil).Count(&d.VerificationCount)
	s.freshDB().Model(&model.RiderApplication{}).Where("is_deleted = ? AND status = ?", model.NotDeleted, model.RiderApplicationPending).Count(&d.PendingRiderApps)
	s.freshDB().Model(&model.DeliveryOrder{}).
		Where("is_deleted = ? AND status = ?", model.NotDeleted, model.DeliveryException).
		Count(&d.PendingDeliveryExceptions)
	s.freshDB().Model(&model.DeliveryOrder{}).
		Where("is_deleted = ? AND status = ? AND inventory_usage_id IS NOT NULL AND takeout_order_id IS NULL",
			model.NotDeleted, model.DeliveryPendingAdminReview).
		Count(&d.PendingBagDeliveryReviews)
	s.freshDB().Model(&model.MerchantProfile{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.MerchantCount)
	s.freshDB().Model(&model.Product{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.ProductCount)
	s.freshDB().Model(&model.Product{}).Where("is_deleted = ? AND stock <= ?", model.NotDeleted, 10).Count(&d.LowStockProductCount)
	s.freshDB().Model(&model.Account{}).Where("is_deleted = ? AND type = ?", model.NotDeleted, model.AccountTypeUser).Count(&d.UserCount)

	allTimeSales, err := s.SalesReport(SalesReportFilter{MerchantID: nil})
	if err != nil {
		return nil, err
	}
	d.TotalSales = allTimeSales.TotalSalesAmount
	d.ValidOrderCount = allTimeSales.ValidOrderCount

	var err2 error
	d.OrderTrend, err2 = s.orderTrend(nil, 7)
	if err2 != nil {
		return nil, err2
	}
	d.TopProducts, err2 = s.topProducts(nil, 10)
	if err2 != nil {
		return nil, err2
	}
	return d, nil
}

func (s *DashboardService) Merchant(merchantID uint64) (*MerchantDashboard, error) {
	d := &MerchantDashboard{}
	s.freshDB().Model(&model.Product{}).Where("is_deleted = ? AND merchant_id = ?", model.NotDeleted, merchantID).Count(&d.ProductCount)
	s.freshDB().Model(&model.Order{}).Where(
		"is_deleted = ? AND merchant_id = ? AND status = ? AND merchant_review_stage = ?",
		model.NotDeleted, merchantID, model.OrderStatusPendingFulfill, model.MerchantReviewPending,
	).Count(&d.PendingOrderReview)
	s.freshDB().Model(&model.Order{}).Where(
		"is_deleted = ? AND merchant_id = ? AND status = ? AND merchant_review_stage = ?",
		model.NotDeleted, merchantID, model.OrderStatusPendingFulfill, model.MerchantReviewPendingUse,
	).Count(&d.PendingUseReview)

	// 备餐中：订单路径（Order.status=Preparing）+ 外卖配餐中（不含背包跑腿；订单轨配送备餐已由 Order.status 覆盖）
	s.freshDB().Model(&model.Order{}).Where(
		"is_deleted = ? AND merchant_id = ? AND status = ?",
		model.NotDeleted, merchantID, model.OrderStatusPreparing,
	).Count(&d.PendingPreparingCount)
	var takeoutPreparing int64
	s.freshDB().Model(&model.TakeoutOrder{}).Where(
		"is_deleted = ? AND merchant_id = ? AND status = ?",
		model.NotDeleted, merchantID, model.TakeoutStatusPreparing,
	).Count(&takeoutPreparing)
	d.PendingPreparingCount += takeoutPreparing

	start, end := todayRange()
	mid := merchantID
	s.effectiveVerificationQuery(&mid).
		Where("vr.verified_at >= ? AND vr.verified_at < ?", start, end).
		Count(&d.TodayVerificationCount)

	s.freshDB().Model(&model.Product{}).Where(
		"is_deleted = ? AND merchant_id = ? AND stock <= ?", model.NotDeleted, merchantID, 10,
	).Count(&d.LowStockCount)

	var err error
	d.OrderTrend, err = s.orderTrend(&merchantID, 7)
	if err != nil {
		return nil, err
	}
	d.TopProducts, err = s.topProducts(&merchantID, 10)
	if err != nil {
		return nil, err
	}
	sales, err := s.SalesReport(SalesReportFilter{MerchantID: &merchantID})
	if err != nil {
		return nil, err
	}
	d.Sales = *sales
	return d, nil
}

// SalesReport 统计有效（已确定收益）订单/营业额及核销次数。
func (s *DashboardService) SalesReport(filter SalesReportFilter) (*SalesReport, error) {
	report := &SalesReport{MerchantID: filter.MerchantID}
	if filter.StartDate != nil {
		report.StartDate = filter.StartDate.Format("2006-01-02")
	}
	if filter.EndDate != nil {
		report.EndDate = filter.EndDate.Add(-24 * time.Hour).Format("2006-01-02")
	}
	if filter.MerchantID != nil {
		var mp model.MerchantProfile
		if err := s.freshDB().Model(&model.MerchantProfile{}).
			Select("shop_name").
			Where("is_deleted = ? AND id = ?", model.NotDeleted, *filter.MerchantID).
			First(&mp).Error; err == nil {
			report.MerchantName = mp.ShopName
		}
	}

	items, count, amount, err := s.listCompletedSalesItems(filter)
	if err != nil {
		return nil, err
	}
	report.CompletedItems = items
	report.CompletedItemCount = count
	report.CompletedSalesAmount = roundMoney(amount)
	// 看板「有效订单 / 有效营业额」与已核销同口径
	report.ValidOrderCount = count
	report.TotalSalesAmount = report.CompletedSalesAmount

	pendingItems, pendingCount, pendingAmount, err := s.listPendingSalesItems(filter)
	if err != nil {
		return nil, err
	}
	report.PendingItems = pendingItems
	report.PendingOrderCount = pendingCount
	report.PendingSalesAmount = roundMoney(pendingAmount)

	vrQ := s.effectiveVerificationQuery(filter.MerchantID)
	vrQ = applyVerifiedTimeRange(vrQ, filter.StartDate, filter.EndDate)
	if err := vrQ.Count(&report.VerificationCount).Error; err != nil {
		return nil, err
	}
	return report, nil
}

// effectiveVerificationQuery 仅统计仍对应「已完成」使用记录的核销（回退后作废的不计入）。
func (s *DashboardService) effectiveVerificationQuery(merchantID *uint64) *gorm.DB {
	q := s.freshDB().Table("verification_record AS vr").
		Joins("JOIN verification_code vc ON vc.id = vr.verification_code_id AND vc.is_deleted = ?", model.NotDeleted).
		Joins("JOIN user_inventory_usage u ON u.id = vc.inventory_usage_id AND u.is_deleted = ? AND u.status = ?",
			model.NotDeleted, model.InventoryUsageCompleted).
		Where("vr.is_deleted = ?", model.NotDeleted)
	if merchantID != nil {
		q = q.Where("vr.merchant_id = ?", *merchantID)
	}
	return q
}

func applyCompletedTimeRange(q *gorm.DB, col string, start, end *time.Time) *gorm.DB {
	if start != nil {
		q = q.Where(col+" >= ?", *start)
	}
	if end != nil {
		q = q.Where(col+" < ?", *end)
	}
	return q
}

func applyVerifiedTimeRange(q *gorm.DB, start, end *time.Time) *gorm.DB {
	return applyCompletedTimeRange(q, "vr.verified_at", start, end)
}

// listCompletedSalesItems 汇总已确定收益：背包履约完成 + 外卖完成。
func (s *DashboardService) listCompletedSalesItems(filter SalesReportFilter) ([]SalesCompletedItem, int64, float64, error) {
	bagItems, bagCount, bagSum, err := s.listCompletedBagSalesItems(filter)
	if err != nil {
		return nil, 0, 0, err
	}
	takeoutItems, takeoutCount, takeoutSum, err := s.listCompletedTakeoutSalesItems(filter)
	if err != nil {
		return nil, 0, 0, err
	}
	totalCount := bagCount + takeoutCount
	totalSum := bagSum + takeoutSum

	merged := make([]SalesCompletedItem, 0, len(bagItems)+len(takeoutItems))
	merged = append(merged, bagItems...)
	merged = append(merged, takeoutItems...)
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].CompletedAt > merged[j].CompletedAt
	})
	if len(merged) > 500 {
		merged = merged[:500]
	}
	return merged, totalCount, totalSum, nil
}

func (s *DashboardService) listCompletedBagSalesItems(filter SalesReportFilter) ([]SalesCompletedItem, int64, float64, error) {
	q := query.NotDeleted(s.freshDB().Model(&model.UserInventoryUsage{})).
		Where("status = ?", model.InventoryUsageCompleted)
	if filter.MerchantID != nil {
		q = q.Where("usage_merchant_id = ?", *filter.MerchantID)
	}
	q = applyCompletedTimeRange(q, "updated_at", filter.StartDate, filter.EndDate)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}

	var usages []model.UserInventoryUsage
	if err := q.Preload("Product", "is_deleted = ?", model.NotDeleted).
		Order("updated_at DESC").
		Limit(500).
		Find(&usages).Error; err != nil {
		return nil, 0, 0, err
	}

	unitByKey := s.loadOrderItemSaleMeta(usages)

	items := make([]SalesCompletedItem, 0, len(usages))
	var sum float64
	for _, u := range usages {
		name := ""
		catalogPrice := 0.0
		isPkg := false
		if u.Product != nil {
			name = u.Product.Name
			catalogPrice = u.Product.Price
			isPkg = u.Product.ItemType == model.ProductItemTypePackage
		}
		unitPrice := catalogPrice
		var pt uint8
		var actID *uint64
		if u.SourceOrderID != nil {
			if m, ok := unitByKey[orderProductKey(*u.SourceOrderID, u.ProductID)]; ok {
				if m.UnitPrice > 0 {
					unitPrice = m.UnitPrice
				}
				pt = m.PurchaseType
				actID = m.ActivityID
			}
		}
		amount := roundMoney(unitPrice * float64(u.Quantity))
		sum += amount
		fulfillType := "pickup"
		fulfillText := "到店自提·已核销"
		if u.DeliveryType == model.DeliveryTypeDelivery {
			fulfillType = "delivery"
			fulfillText = "骑手配送·已确认收货"
		}
		pkgText := ""
		if isPkg {
			pkgText = u.PackageSelections.SummaryText()
		}
		nick, phone := s.lookupAccountBrief(u.AccountID)
		ch, chText := ResolveSaleChannel(pt, actID)
		items = append(items, SalesCompletedItem{
			UsageID: u.ID, ProductID: u.ProductID, ProductName: name,
			Quantity: u.Quantity, UnitPrice: unitPrice, Amount: amount,
			FulfillType: fulfillType, FulfillText: fulfillText,
			CompletedAt: u.UpdatedAt.Format("2006-01-02 15:04"),
			PackageText: pkgText, IsPackage: isPkg,
			UserNickname: nick, UserPhone: phone,
			PurchaseType: pt, ActivityID: actID,
			SaleChannel: ch, SaleChannelText: chText,
		})
	}
	return items, total, sum, nil
}

func (s *DashboardService) listCompletedTakeoutSalesItems(filter SalesReportFilter) ([]SalesCompletedItem, int64, float64, error) {
	q := query.NotDeleted(s.freshDB().Model(&model.TakeoutOrder{})).
		Where("status = ? AND pay_status = ?", model.TakeoutStatusCompleted, model.PayStatusPaid)
	if filter.MerchantID != nil {
		q = q.Where("usage_merchant_id = ?", *filter.MerchantID)
	}
	q = applyCompletedTimeRange(q, "updated_at", filter.StartDate, filter.EndDate)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}

	var orders []model.TakeoutOrder
	if err := q.Preload("Items", "is_deleted = ?", model.NotDeleted).
		Order("updated_at DESC").
		Limit(500).
		Find(&orders).Error; err != nil {
		return nil, 0, 0, err
	}

	items := make([]SalesCompletedItem, 0, len(orders))
	var sum float64
	for _, o := range orders {
		net := o.PayAmount - o.RefundedAmount
		if net < 0 {
			net = 0
		}
		net = roundMoney(net)
		sum += net
		name := "外卖订单"
		qty := uint32(0)
		unit := 0.0
		if len(o.Items) > 0 {
			name = o.Items[0].ProductName
			if len(o.Items) > 1 {
				name = name + " 等"
			}
			for _, it := range o.Items {
				qty += it.Quantity
			}
			if qty > 0 {
				unit = roundMoney(net / float64(qty))
			}
		}
		nick, phone := s.lookupAccountBrief(o.AccountID)
		items = append(items, SalesCompletedItem{
			TakeoutID: o.ID, ProductID: firstTakeoutProductID(o.Items), ProductName: name,
			Quantity: qty, UnitPrice: unit, Amount: net,
			FulfillType: "takeout", FulfillText: "外卖·已完成",
			CompletedAt: o.UpdatedAt.Format("2006-01-02 15:04"),
			UserNickname: nick, UserPhone: phone,
		})
	}
	return items, total, sum, nil
}

// listPendingSalesItems 已付款未履约完：待核销/配送中 usage + 进行中外卖 + 背包剩余库存。
func (s *DashboardService) listPendingSalesItems(filter SalesReportFilter) ([]SalesPendingItem, int64, float64, error) {
	bag, bagCount, bagSum, err := s.listPendingBagSalesItems(filter)
	if err != nil {
		return nil, 0, 0, err
	}
	takeout, takeoutCount, takeoutSum, err := s.listPendingTakeoutSalesItems(filter)
	if err != nil {
		return nil, 0, 0, err
	}
	inv, invCount, invSum, err := s.listPendingInventorySalesItems(filter)
	if err != nil {
		return nil, 0, 0, err
	}
	merged := make([]SalesPendingItem, 0, len(bag)+len(takeout)+len(inv))
	merged = append(merged, bag...)
	merged = append(merged, takeout...)
	merged = append(merged, inv...)
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].PurchasedAt > merged[j].PurchasedAt
	})
	if len(merged) > 500 {
		merged = merged[:500]
	}
	return merged, bagCount + takeoutCount + invCount, bagSum + takeoutSum + invSum, nil
}

func (s *DashboardService) listPendingBagSalesItems(filter SalesReportFilter) ([]SalesPendingItem, int64, float64, error) {
	q := query.NotDeleted(s.freshDB().Model(&model.UserInventoryUsage{})).
		Where("status IN ?", []int{
			int(model.InventoryUsagePendingVerify),
			int(model.InventoryUsagePendingShip),
			int(model.InventoryUsageCancelPending),
		})
	if filter.MerchantID != nil {
		q = q.Where("usage_merchant_id = ?", *filter.MerchantID)
	}
	q = applyCompletedTimeRange(q, "created_at", filter.StartDate, filter.EndDate)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var usages []model.UserInventoryUsage
	if err := q.Preload("Product", "is_deleted = ?", model.NotDeleted).
		Order("created_at DESC").
		Limit(500).
		Find(&usages).Error; err != nil {
		return nil, 0, 0, err
	}
	metaByKey := s.loadOrderItemSaleMeta(usages)
	items := make([]SalesPendingItem, 0, len(usages))
	var sum float64
	for _, u := range usages {
		name := ""
		catalogPrice := 0.0
		if u.Product != nil {
			name = u.Product.Name
			catalogPrice = u.Product.Price
		}
		unitPrice := catalogPrice
		var pt uint8
		var actID *uint64
		if u.SourceOrderID != nil {
			if m, ok := metaByKey[orderProductKey(*u.SourceOrderID, u.ProductID)]; ok {
				if m.UnitPrice > 0 {
					unitPrice = m.UnitPrice
				}
				pt = m.PurchaseType
				actID = m.ActivityID
			}
		}
		amount := roundMoney(unitPrice * float64(u.Quantity))
		sum += amount
		nick, phone := s.lookupAccountBrief(u.AccountID)
		ch, chText := ResolveSaleChannel(pt, actID)
		items = append(items, SalesPendingItem{
			Source: "usage", ID: u.ID, ProductID: u.ProductID, ProductName: name,
			Quantity: u.Quantity, UnitPrice: unitPrice, Amount: amount,
			PurchasedAt: u.CreatedAt.Format("2006-01-02 15:04"),
			StatusText:  model.InventoryUsageStatusText(u.Status),
			UserNickname: nick, UserPhone: phone,
			PurchaseType: pt, ActivityID: actID,
			SaleChannel: ch, SaleChannelText: chText,
		})
	}
	return items, total, sum, nil
}

func (s *DashboardService) listPendingTakeoutSalesItems(filter SalesReportFilter) ([]SalesPendingItem, int64, float64, error) {
	q := query.NotDeleted(s.freshDB().Model(&model.TakeoutOrder{})).
		Where("pay_status = ? AND status IN ?", model.PayStatusPaid, []int{
			int(model.TakeoutStatusPreparing),
			int(model.TakeoutStatusFulfilling),
		})
	if filter.MerchantID != nil {
		q = q.Where("usage_merchant_id = ?", *filter.MerchantID)
	}
	q = applyCompletedTimeRange(q, "created_at", filter.StartDate, filter.EndDate)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var orders []model.TakeoutOrder
	if err := q.Preload("Items", "is_deleted = ?", model.NotDeleted).
		Order("created_at DESC").
		Limit(500).
		Find(&orders).Error; err != nil {
		return nil, 0, 0, err
	}
	items := make([]SalesPendingItem, 0, len(orders))
	var sum float64
	for _, o := range orders {
		net := o.PayAmount - o.RefundedAmount
		if net < 0 {
			net = 0
		}
		net = roundMoney(net)
		sum += net
		name := "外卖订单"
		qty := uint32(0)
		unit := 0.0
		if len(o.Items) > 0 {
			name = o.Items[0].ProductName
			if len(o.Items) > 1 {
				name = name + " 等"
			}
			for _, it := range o.Items {
				qty += it.Quantity
			}
			if qty > 0 {
				unit = roundMoney(net / float64(qty))
			}
		}
		statusText := "外卖·配餐中"
		if o.Status == model.TakeoutStatusFulfilling {
			statusText = "外卖·配送中"
		}
		purchasedAt := o.CreatedAt.Format("2006-01-02 15:04")
		if o.PaidAt != nil {
			purchasedAt = o.PaidAt.Format("2006-01-02 15:04")
		}
		nick, phone := s.lookupAccountBrief(o.AccountID)
		items = append(items, SalesPendingItem{
			Source: "takeout", ID: o.ID, ProductID: firstTakeoutProductID(o.Items), ProductName: name,
			Quantity: qty, UnitPrice: unit, Amount: net,
			PurchasedAt: purchasedAt, StatusText: statusText,
			UserNickname: nick, UserPhone: phone,
		})
	}
	return items, total, sum, nil
}

func (s *DashboardService) listPendingInventorySalesItems(filter SalesReportFilter) ([]SalesPendingItem, int64, float64, error) {
	q := query.NotDeleted(s.freshDB().Model(&model.UserInventory{})).
		Where("quantity > 0")
	if filter.MerchantID != nil {
		q = q.Where(
			"product_id IN (SELECT id FROM product WHERE is_deleted = 0 AND merchant_id = ?)",
			*filter.MerchantID,
		)
	}
	q = applyCompletedTimeRange(q, "updated_at", filter.StartDate, filter.EndDate)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var rows []model.UserInventory
	if err := q.Preload("Product", "is_deleted = ?", model.NotDeleted).
		Order("updated_at DESC").
		Limit(500).
		Find(&rows).Error; err != nil {
		return nil, 0, 0, err
	}
	items := make([]SalesPendingItem, 0, len(rows))
	var sum float64
	for _, inv := range rows {
		name := ""
		unitPrice := 0.0
		if inv.Product.ID > 0 {
			name = inv.Product.Name
			unitPrice = inv.Product.Price
		}
		amount := roundMoney(unitPrice * float64(inv.Quantity))
		sum += amount
		nick, phone := s.lookupAccountBrief(inv.AccountID)
		items = append(items, SalesPendingItem{
			Source: "inventory", ID: inv.ID, ProductID: inv.ProductID, ProductName: name,
			Quantity: inv.Quantity, UnitPrice: unitPrice, Amount: amount,
			PurchasedAt: inv.UpdatedAt.Format("2006-01-02 15:04"),
			StatusText:  "已入背包·待使用",
			UserNickname: nick, UserPhone: phone,
			SaleChannel: "deal", SaleChannelText: "团购",
		})
	}
	return items, total, sum, nil
}

func (s *DashboardService) lookupAccountBrief(accountID uint64) (nickname, phone string) {
	if accountID == 0 {
		return "", ""
	}
	var acc model.Account
	if err := s.freshDB().Model(&model.Account{}).
		Select("nickname", "phone").
		Where("id = ? AND is_deleted = ?", accountID, model.NotDeleted).
		First(&acc).Error; err != nil {
		return "", ""
	}
	if acc.Nickname != nil {
		nickname = *acc.Nickname
	}
	if acc.Phone != nil {
		phone = *acc.Phone
	}
	return nickname, phone
}

func firstTakeoutProductID(items []model.TakeoutOrderItem) uint64 {
	if len(items) == 0 {
		return 0
	}
	return items[0].ProductID
}

func orderProductKey(orderID, productID uint64) string {
	return fmt.Sprintf("%d:%d", orderID, productID)
}

func (s *DashboardService) loadOrderItemUnitPrices(usages []model.UserInventoryUsage) map[string]float64 {
	meta := s.loadOrderItemSaleMeta(usages)
	out := make(map[string]float64, len(meta))
	for k, v := range meta {
		out[k] = v.UnitPrice
	}
	return out
}

// loadOrderItemSaleMeta 批量加载来源订单项的成交单价、购买类型与活动归属。
// 同一 (order_id, product_id) 仅保留首条；usages 缺失来源订单时返回空 map。
func (s *DashboardService) loadOrderItemSaleMeta(usages []model.UserInventoryUsage) map[string]orderItemSaleMeta {
	out := map[string]orderItemSaleMeta{}
	orderIDs := make([]uint64, 0)
	seen := map[uint64]struct{}{}
	for _, u := range usages {
		if u.SourceOrderID == nil || *u.SourceOrderID == 0 {
			continue
		}
		if _, ok := seen[*u.SourceOrderID]; ok {
			continue
		}
		seen[*u.SourceOrderID] = struct{}{}
		orderIDs = append(orderIDs, *u.SourceOrderID)
	}
	if len(orderIDs) == 0 {
		return out
	}
	var items []model.OrderItem
	if err := query.NotDeleted(s.freshDB().Model(&model.OrderItem{})).
		Select("order_id", "product_id", "unit_price", "subtotal", "quantity", "purchase_type", "activity_id").
		Where("order_id IN ?", orderIDs).
		Find(&items).Error; err != nil {
		return out
	}
	for _, it := range items {
		unit := it.UnitPrice
		if it.Quantity > 0 && it.Subtotal > 0 {
			unit = it.Subtotal / float64(it.Quantity)
		}
		key := orderProductKey(it.OrderID, it.ProductID)
		if _, exists := out[key]; !exists {
			out[key] = orderItemSaleMeta{
				UnitPrice:    roundMoney(unit),
				PurchaseType: it.PurchaseType,
				ActivityID:   it.ActivityID,
			}
		}
	}
	return out
}

func ParseSalesDateRange(startRaw, endRaw string) (start, end *time.Time, err error) {
	return parseSalesDateRange(startRaw, endRaw)
}

func parseSalesDateRange(startRaw, endRaw string) (start, end *time.Time, err error) {
	if startRaw == "" && endRaw == "" {
		return nil, nil, nil
	}
	loc := time.Local
	if startRaw != "" {
		t, parseErr := time.ParseInLocation("2006-01-02", startRaw, loc)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		start = &t
	}
	if endRaw != "" {
		t, parseErr := time.ParseInLocation("2006-01-02", endRaw, loc)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		endDay := t.Add(24 * time.Hour)
		end = &endDay
	} else if start != nil {
		endDay := start.Add(24 * time.Hour)
		end = &endDay
	}
	return start, end, nil
}

func todayRange() (time.Time, time.Time) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return start, start.Add(24 * time.Hour)
}

func (s *DashboardService) orderTrend(merchantID *uint64, days int) ([]DailyStat, error) {
	if days < 1 {
		days = 7
	}
	now := time.Now()
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))

	type row struct {
		Day         string
		OrderCount  int64
		SalesAmount float64
	}
	byDay := make(map[string]DailyStat, days)

	addRows := func(rows []row) {
		for _, r := range rows {
			day := normalizeTrendDay(r.Day)
			if day == "" {
				continue
			}
			cur := byDay[day]
			cur.Date = day
			cur.OrderCount += r.OrderCount
			cur.SalesAmount = roundMoney(cur.SalesAmount + r.SalesAmount)
			byDay[day] = cur
		}
	}

	// 背包履约完成：金额优先取来源订单成交单价
	bagDay := "DATE_FORMAT(u.updated_at, '%Y-%m-%d')"
	bagUnit := `COALESCE((
		SELECT CASE WHEN oi.quantity > 0 AND oi.subtotal > 0 THEN oi.subtotal / oi.quantity ELSE oi.unit_price END
		FROM order_item oi
		WHERE oi.order_id = u.source_order_id AND oi.product_id = u.product_id AND oi.is_deleted = 0
		LIMIT 1
	), p.price, 0)`
	bagQ := s.freshDB().Table("user_inventory_usage AS u").
		Joins("LEFT JOIN product p ON p.id = u.product_id AND p.is_deleted = 0").
		Where("u.is_deleted = ? AND u.status = ? AND u.updated_at >= ?",
			model.NotDeleted, model.InventoryUsageCompleted, start).
		Select(bagDay + " AS day, COUNT(*) AS order_count, COALESCE(SUM((" + bagUnit + ") * u.quantity), 0) AS sales_amount")
	if merchantID != nil {
		bagQ = bagQ.Where("u.usage_merchant_id = ?", *merchantID)
	}
	var bagRows []row
	if err := bagQ.Group(bagDay).Scan(&bagRows).Error; err != nil {
		return nil, err
	}
	addRows(bagRows)

	// 外卖完成
	toDay := "DATE_FORMAT(updated_at, '%Y-%m-%d')"
	toQ := query.NotDeleted(s.freshDB().Model(&model.TakeoutOrder{})).
		Where("status = ? AND pay_status = ? AND updated_at >= ?",
			model.TakeoutStatusCompleted, model.PayStatusPaid, start).
		Select(toDay + " AS day, COUNT(*) AS order_count, COALESCE(SUM(GREATEST(pay_amount - refunded_amount, 0)), 0) AS sales_amount")
	if merchantID != nil {
		toQ = toQ.Where("usage_merchant_id = ?", *merchantID)
	}
	var toRows []row
	if err := toQ.Group(toDay).Scan(&toRows).Error; err != nil {
		return nil, err
	}
	addRows(toRows)

	out := make([]DailyStat, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		if stat, ok := byDay[d]; ok {
			out = append(out, stat)
		} else {
			out = append(out, DailyStat{Date: d})
		}
	}
	return out, nil
}

// normalizeTrendDay 统一为 YYYY-MM-DD，兼容驱动偶发返回带时间的 day 字段。
func normalizeTrendDay(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t.Format("2006-01-02")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(time.Local).Format("2006-01-02")
	}
	return ""
}

func (s *DashboardService) topProducts(merchantID *uint64, limit int) ([]ProductSalesRank, error) {
	if limit < 1 {
		limit = 10
	}
	// 有效销量：已完成履约件数（背包核销/确认收货 + 外卖完成）
	type agg struct {
		ProductID  uint64
		SalesCount uint32
	}
	counts := map[uint64]uint32{}

	bagQ := s.freshDB().Table("user_inventory_usage AS u").
		Select("u.product_id, COALESCE(SUM(u.quantity), 0) AS sales_count").
		Where("u.is_deleted = ? AND u.status = ?", model.NotDeleted, model.InventoryUsageCompleted)
	if merchantID != nil {
		bagQ = bagQ.Where("u.usage_merchant_id = ?", *merchantID)
	}
	var bagRows []agg
	if err := bagQ.Group("u.product_id").Scan(&bagRows).Error; err != nil {
		return nil, err
	}
	for _, r := range bagRows {
		counts[r.ProductID] += r.SalesCount
	}

	toQ := s.freshDB().Table("takeout_order_item AS ti").
		Select("ti.product_id, COALESCE(SUM(ti.quantity), 0) AS sales_count").
		Joins("JOIN takeout_order t ON t.id = ti.takeout_order_id AND t.is_deleted = 0").
		Where("ti.is_deleted = 0 AND t.status = ? AND t.pay_status = ?",
			model.TakeoutStatusCompleted, model.PayStatusPaid)
	if merchantID != nil {
		toQ = toQ.Where("t.usage_merchant_id = ?", *merchantID)
	}
	var toRows []agg
	if err := toQ.Group("ti.product_id").Scan(&toRows).Error; err != nil {
		return nil, err
	}
	for _, r := range toRows {
		counts[r.ProductID] += r.SalesCount
	}
	if len(counts) == 0 {
		return []ProductSalesRank{}, nil
	}

	ids := make([]uint64, 0, len(counts))
	for id := range counts {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	var products []model.Product
	if err := query.NotDeleted(s.freshDB().Model(&model.Product{})).
		Select("id", "name", "merchant_id", "cover_url").
		Where("id IN ?", ids).
		Find(&products).Error; err != nil {
		return nil, err
	}
	byID := map[uint64]model.Product{}
	for _, p := range products {
		byID[p.ID] = p
	}
	out := make([]ProductSalesRank, 0, len(counts))
	for id, n := range counts {
		p, ok := byID[id]
		if !ok {
			continue
		}
		cover := ""
		if p.CoverURL != "" {
			cover = p.CoverURL
		}
		out = append(out, ProductSalesRank{
			ProductID: id, ProductName: p.Name, MerchantID: p.MerchantID,
			CoverURL: cover, SalesCount: n,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SalesCount == out[j].SalesCount {
			return out[i].ProductID < out[j].ProductID
		}
		return out[i].SalesCount > out[j].SalesCount
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type CategoryService struct {
	DB *gorm.DB
}

func (s *CategoryService) List() ([]model.ProductCategory, error) {
	return s.ListByMerchant(0, true)
}

// ListByMerchant 某店铺的商品分类；visibleOnly=true 时仅返回 status=1。
func (s *CategoryService) ListByMerchant(merchantID uint64, visibleOnly bool) ([]model.ProductCategory, error) {
	q := query.NotDeleted(s.DB).Where("merchant_id = ?", merchantID).Order("sort_order ASC, id ASC")
	if visibleOnly {
		q = q.Where("status = 1")
	}
	var list []model.ProductCategory
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *CategoryService) ListAll(status *uint8) ([]model.ProductCategory, error) {
	return s.ListAllScoped(nil, status)
}

func (s *CategoryService) ListAllScoped(merchantID *uint64, status *uint8) ([]model.ProductCategory, error) {
	q := query.NotDeleted(s.DB.Model(&model.ProductCategory{})).Order("sort_order ASC, id ASC")
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var list []model.ProductCategory
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *CategoryService) GetByID(id uint64) (*model.ProductCategory, error) {
	var cat model.ProductCategory
	if err := query.NotDeleted(s.DB).First(&cat, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	return &cat, nil
}

func (s *CategoryService) GetByIDForMerchant(id, merchantID uint64) (*model.ProductCategory, error) {
	cat, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if cat.MerchantID != merchantID {
		return nil, ErrCategoryForbidden
	}
	return cat, nil
}

type CreateCategoryInput struct {
	MerchantID uint64
	ParentID   uint64
	Name       string
	IconURL    *string
	SortOrder  int
	Status     uint8
}

type UpdateCategoryInput struct {
	Name      *string
	IconURL   *string
	SortOrder *int
	Status    *uint8
}

func (s *CategoryService) Create(input CreateCategoryInput) (*model.ProductCategory, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || input.MerchantID == 0 {
		return nil, ErrInvalidProductArg
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, ErrInvalidProductArg
	}
	if input.ParentID > 0 {
		parent, err := s.GetByIDForMerchant(input.ParentID, input.MerchantID)
		if err != nil {
			return nil, err
		}
		if parent.ParentID != 0 {
			return nil, ErrInvalidProductArg
		}
	}
	status := input.Status
	if status == 0 {
		status = 1
	}
	cat := model.ProductCategory{
		MerchantID: input.MerchantID,
		ParentID:   input.ParentID, Name: name, IconURL: input.IconURL,
		SortOrder: input.SortOrder, Status: status,
	}
	if err := s.DB.Create(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

func (s *CategoryService) Update(id uint64, input UpdateCategoryInput) (*model.ProductCategory, error) {
	return s.UpdateForMerchant(id, 0, input, false)
}

func (s *CategoryService) UpdateForMerchant(id, merchantID uint64, input UpdateCategoryInput, scoped bool) (*model.ProductCategory, error) {
	var cat *model.ProductCategory
	var err error
	if scoped {
		cat, err = s.GetByIDForMerchant(id, merchantID)
	} else {
		cat, err = s.GetByID(id)
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, ErrInvalidProductArg
		}
		updates["name"] = name
	}
	if input.IconURL != nil {
		updates["icon_url"] = *input.IconURL
	}
	if input.SortOrder != nil {
		updates["sort_order"] = *input.SortOrder
	}
	if input.Status != nil {
		updates["status"] = *input.Status
	}
	if len(updates) == 0 {
		return cat, nil
	}
	if err := s.DB.Model(cat).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *CategoryService) Delete(id uint64) error {
	return s.DeleteForMerchant(id, 0, false)
}

func (s *CategoryService) DeleteForMerchant(id, merchantID uint64, scoped bool) error {
	var cat *model.ProductCategory
	var err error
	if scoped {
		cat, err = s.GetByIDForMerchant(id, merchantID)
	} else {
		cat, err = s.GetByID(id)
	}
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, cat).Error
}

// FindOrCreateByName 按商家与名称查找一级分类，不存在则自动创建。
// merchantID=0 表示平台分类（套餐等）。
func (s *CategoryService) FindOrCreateByName(merchantID uint64, name string) (*model.ProductCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidProductArg
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, ErrInvalidProductArg
	}

	var cat model.ProductCategory
	err := query.NotDeleted(s.DB).
		Where("merchant_id = ? AND name = ?", merchantID, name).
		Order("parent_id ASC, id ASC").
		First(&cat).Error
	if err == nil {
		return &cat, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	cat = model.ProductCategory{
		MerchantID: merchantID,
		ParentID:   0,
		Name:       name,
		Status:     1,
	}
	if err := s.DB.Create(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

// EnsureBelongsToMerchant 校验分类属于指定商家。
func (s *CategoryService) EnsureBelongsToMerchant(categoryID, merchantID uint64) error {
	cat, err := s.GetByID(categoryID)
	if err != nil {
		return err
	}
	if cat.MerchantID != merchantID {
		return ErrCategoryForbidden
	}
	return nil
}
