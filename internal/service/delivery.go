package service

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDeliveryNotFound           = errors.New("delivery order not found")
	ErrDeliveryTaken              = errors.New("delivery order already taken")
	ErrDeliveryForbidden          = errors.New("delivery order forbidden")
	ErrDeliveryStatusInvalid      = errors.New("delivery status invalid")
	ErrBagErrandStartNotAllowed   = errors.New("bag errand start not allowed before verify")
)

// genPickupCode ?? 4 ???????????????????????
func genPickupCode(tx *gorm.DB, merchantID uint64) string {
	for i := 0; i < 8; i++ {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			continue
		}
		n := uint16(b[0])<<8 | uint16(b[1])
		code := n % 10000
		if code == 0 {
			continue
		}
		candidate := fmt.Sprintf("%04d", code)
		// 碰撞域按履约门店：外卖单用 usage_merchant_id，订单/背包用 merchant_id（调用方传入对应 ID）
		var existing int64
		tx.Model(&model.DeliveryOrder{}).
			Where("pickup_code = ? AND is_deleted = ? AND "+
				"(EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM takeout_order t WHERE t.id = delivery_order.takeout_order_id AND t.is_deleted = 0 AND t.usage_merchant_id = ?))",
				candidate, model.NotDeleted, merchantID, merchantID, merchantID).
			Count(&existing)
		if existing == 0 {
			return candidate
		}
	}
	return "1000"
}

type CompleteDeliveryInput struct {
	Remark *string
	Photos []string
}

type DeliveryUsageLine struct {
	UsageID              uint64                        `json:"usage_id,omitempty"`
	ProductID            uint64                        `json:"product_id"`
	ProductName          string                        `json:"product_name"`
	ProductImage         string                        `json:"product_image,omitempty"`
	UnitPrice            float64                       `json:"unit_price"`
	Quantity             uint32                        `json:"quantity"`
	IsPackage            bool                          `json:"is_package"`
	PackageSelectionText string                        `json:"package_selection_text,omitempty"`
	OptionSelectionText  string                        `json:"option_selection_text,omitempty"`
	OptionSelections     model.OptionSelectionSnapshot `json:"option_selections,omitempty"`
}

// DeliveryMerchantBrief 骑手/管理端展示用门店摘要。
type DeliveryMerchantBrief struct {
	ID           uint64   `json:"id"`
	ShopName     string   `json:"shop_name"`
	Address      string   `json:"address,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
}

type DeliveryView struct {
	model.DeliveryOrder
	Errand             *model.ErrandOrder     `json:"errand,omitempty"`
	StatusText         string                 `json:"status_text"`
	AddressSnapshot    *model.AddressSnapshot `json:"address_snapshot,omitempty"`
	UsageItems         []DeliveryUsageLine    `json:"usage_items,omitempty"`
	VerifyCode         *string                `json:"verify_code,omitempty"`
	DeliveryTimeRemark string                 `json:"delivery_time_remark,omitempty"`
	RiderName          string                 `json:"rider_name,omitempty"`
	RiderPhone         string                 `json:"rider_phone,omitempty"`
	// 列表卡片摘要（骑手端商品名/图/价）
	ProductName  string                 `json:"product_name,omitempty"`
	ProductImage string                 `json:"product_image,omitempty"`
	Quantity     uint32                 `json:"quantity,omitempty"`
	GoodsAmount  float64                `json:"goods_amount"`
	TotalAmount  float64                `json:"total_amount"`
	Merchant     *DeliveryMerchantBrief `json:"merchant,omitempty"`
}

type BagDeliveryReviewView struct {
	DeliveryView
	AccountID    uint64 `json:"account_id"`
	Nickname     string `json:"nickname,omitempty"`
	AccountPhone string `json:"account_phone,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	MerchantName string `json:"merchant_name,omitempty"`
}

type DeliveryService struct {
	DB                *gorm.DB
	InventorySvc      *InventoryService
	DeliveryFeePaySvc *DeliveryFeePayService
	ErrandSvc         *ErrandService
}

// IsBagErrand 背包跑腿：有 usage、非外卖主单。
func IsBagErrand(d *model.DeliveryOrder) bool {
	return d != nil && d.InventoryUsageID != nil && d.TakeoutOrderID == nil
}

// IsErrand 独立跑腿配送单。
func IsErrand(d *model.DeliveryOrder) bool {
	return d != nil && d.ErrandOrderID != nil
}

func (s *DeliveryService) ListForRider(riderID uint64, scope string, page, pageSize int) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{}))
	switch scope {
	case "pending":
		q = q.Where(
			"status = ? AND rider_id IS NULL AND (merchant_prepared = 1 OR (inventory_usage_id IS NOT NULL AND takeout_order_id IS NULL) OR errand_order_id IS NOT NULL)",
			model.DeliveryPendingAccept,
		)
	case "active":
		// 已送达但用户未确认收货前仍算配送中，用户确认后才进入配送记录
		q = q.Where("rider_id = ? AND status IN ?", riderID, []int{
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering),
			int(model.DeliveryDelivered),
		})
	case "history":
		q = q.Where("rider_id = ? AND status = ?", riderID, model.DeliveryConfirmed)
	default:
		q = q.Where(
			"status = ? AND rider_id IS NULL AND (merchant_prepared = 1 OR (inventory_usage_id IS NOT NULL AND takeout_order_id IS NULL) OR errand_order_id IS NOT NULL)",
			model.DeliveryPendingAccept,
		)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("Order.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toRiderDeliveryViews(s.DB, list), total, nil
}

// ListForUser scope: active/delivering=配送中；pending_confirm=待确认收货；
// appealing/exception=申诉中；history=已完成/已取消（不含待处理申诉）
func (s *DeliveryService) ListForUser(accountID uint64, scope string, page, pageSize int) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := s.userDeliveryQuery(accountID)
	switch scope {
	case "pending_confirm":
		q = q.Where("status = ? AND user_confirmed = ?", model.DeliveryDelivered, 0)
	case "appealing", "exception":
		q = q.Where("status = ?", model.DeliveryException)
	case "history":
		q = q.Where("status IN ?", []int{
			int(model.DeliveryConfirmed),
			int(model.DeliveryCancelled),
		})
	case "active", "delivering":
		q = q.Where("status IN ?", []int{
			int(model.DeliveryPendingAdminReview),
			int(model.DeliveryPendingAccept),
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering),
		})
	default:
		q = q.Where("status IN ?", []int{
			int(model.DeliveryPendingAdminReview),
			int(model.DeliveryPendingAccept),
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering), int(model.DeliveryDelivered),
		})
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := toDeliveryViews(s.DB, list)
	applyUserAppealStatusText(views)
	return views, total, nil
}

// applyUserAppealStatusText 用户端将配送异常展示为「申诉中」。
func applyUserAppealStatusText(views []DeliveryView) {
	for i := range views {
		if views[i].Status == model.DeliveryException {
			views[i].StatusText = "申诉中"
		}
	}
}

func (s *DeliveryService) GetForUser(accountID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := s.userDeliveryQuery(accountID).Where("id = ?", deliveryID).
		Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	view := toDeliveryView(s.DB, d)
	if view.Status == model.DeliveryException {
		view.StatusText = "申诉中"
	}
	return &view, nil
}

func (s *DeliveryService) Accept(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND status = ? AND rider_id IS NULL", deliveryID, model.DeliveryPendingAccept).
			First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if !IsBagErrand(&d) && !IsErrand(&d) && d.MerchantPrepared != 1 {
			return ErrDeliveryNotFound
		}
		if d.InventoryUsageID != nil || d.OrderID == nil {
			// ???????? usage ?????? usage ???????????????
			var active int64
			_ = query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
				Where("delivery_order_id = ? AND status NOT IN ?", d.ID, []int{
					int(model.InventoryUsageCancelled), int(model.InventoryUsageCancelPending),
				}).Count(&active)
			if active == 0 && d.InventoryUsageID != nil {
				var usage model.UserInventoryUsage
				if err := query.NotDeleted(tx).First(&usage, *d.InventoryUsageID).Error; err != nil {
					return ErrDeliveryNotFound
				}
				if usage.Status == model.InventoryUsageCancelPending || usage.Status == model.InventoryUsageCancelled {
					return ErrDeliveryNotFound
				}
				active = 1
			}
			if active == 0 && d.OrderID == nil && d.ErrandOrderID == nil {
				if d.TakeoutOrderID == nil {
					return ErrDeliveryNotFound
				}
				var to model.TakeoutOrder
				if err := query.NotDeleted(tx).Select("status").First(&to, *d.TakeoutOrderID).Error; err != nil {
					return ErrDeliveryNotFound
				}
				if to.Status == model.TakeoutStatusCancelled {
					return ErrDeliveryNotFound
				}
			}
		}
		now := time.Now()
		res := query.NotDeleted(tx.Model(&model.DeliveryOrder{})).
			Where("id = ? AND status = ? AND rider_id IS NULL", deliveryID, model.DeliveryPendingAccept).
			Updates(map[string]interface{}{
				"rider_id": riderID, "status": model.DeliveryAccepted, "accepted_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrDeliveryTaken
		}
		if d.OrderID != nil {
			if err := tx.Model(&model.Order{}).Where("id = ?", *d.OrderID).Update("status", model.OrderStatusShipping).Error; err != nil {
				return err
			}
		}
		if d.ErrandOrderID != nil {
			res := tx.Model(&model.ErrandOrder{}).
				Where("id = ? AND status = ?", *d.ErrandOrderID, model.ErrandStatusPendingAccept).
				Update("status", model.ErrandStatusAccepted)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrDeliveryNotFound
			}
		}
		rid := riderID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectDelivery,
			SubjectID:   deliveryID,
			EventCode:   model.EventRiderAccepted,
			ActorRole:   model.FulfillmentActorRider,
			ActorID:     &rid,
			Title:       "骑手已接单",
		})
		if d.TakeoutOrderID != nil {
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectTakeout,
				SubjectID:   *d.TakeoutOrderID,
				EventCode:   model.EventRiderAccepted,
				ActorRole:   model.FulfillmentActorRider,
				ActorID:     &rid,
				Title:       "骑手已接单",
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getRiderViewByID(deliveryID)
}

func (s *DeliveryService) Start(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).Where("id = ? AND rider_id = ?", deliveryID, riderID).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	if IsBagErrand(&d) {
		return nil, ErrBagErrandStartNotAllowed
	}
	if IsErrand(&d) {
		// 跑腿取货走专用接口（需录代购商品费/小票）
		return nil, ErrDeliveryStatusInvalid
	}
	if d.Status != model.DeliveryAccepted {
		return nil, ErrDeliveryStatusInvalid
	}
	if d.MerchantPrepared != 1 {
		return nil, ErrDeliveryStatusInvalid
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status": model.DeliveryDelivering, "started_at": now,
	}
	if err := s.DB.Model(&d).Updates(updates).Error; err != nil {
		return nil, err
	}
	if d.OrderID != nil {
		_ = s.DB.Model(&model.Order{}).Where("id = ?", *d.OrderID).Update("status", model.OrderStatusShipping).Error
	}
	rid := riderID
	AppendFulfillmentEvent(s.DB, FulfillmentEventInput{
		SubjectType: model.FulfillmentSubjectDelivery,
		SubjectID:   deliveryID,
		EventCode:   model.EventDelivering,
		ActorRole:   model.FulfillmentActorRider,
		ActorID:     &rid,
		Title:       "骑手配送中",
	})
	if d.TakeoutOrderID != nil {
		AppendFulfillmentEvent(s.DB, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectTakeout,
			SubjectID:   *d.TakeoutOrderID,
			EventCode:   model.EventDelivering,
			ActorRole:   model.FulfillmentActorRider,
			ActorID:     &rid,
			Title:       "骑手配送中",
		})
	}
	return s.getRiderViewByID(deliveryID)
}

// MarkPrepared ??????????(merchant_prepared=0) -> ????1)??
// ????????Preparing ????PendingShip(????????
func (s *DeliveryService) MarkPrepared(merchantID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&d, deliveryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if d.Status != model.DeliveryPendingAccept || d.MerchantPrepared != 0 {
			// ????(merchant_prepared=0)???????????????
			return ErrDeliveryStatusInvalid
		}
		// ??????
		if !deliveryBelongsToMerchant(tx, &d, merchantID) {
			return ErrDeliveryForbidden
		}
		now := time.Now()
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"merchant_prepared": 1,
			"prepared_at":        now,
		}).Error; err != nil {
			return err
		}
		// ????????????????
		if d.OrderID != nil {
			return tx.Model(&model.Order{}).
				Where("id = ? AND status = ?", *d.OrderID, model.OrderStatusPreparing).
				Update("status", model.OrderStatusPendingShip).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

// RejectPrepare 商家在备餐中拒绝出餐：取消配送、商品回退用户背包，并记录拒绝原因。
func (s *DeliveryService) RejectPrepare(merchantID, deliveryID uint64, reason string) (*DeliveryView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写拒绝原因", ErrDeliveryStatusInvalid)
	}
	reasonText := "商家拒单：" + reason

	var d model.DeliveryOrder
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&d, deliveryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		// 仅备餐中（待接单且未出餐、未指派骑手）可拒
		if d.Status != model.DeliveryPendingAccept || d.MerchantPrepared != 0 || d.RiderID != nil {
			return ErrDeliveryStatusInvalid
		}
		if !deliveryBelongsToMerchant(tx, &d, merchantID) {
			return ErrDeliveryForbidden
		}

		var usages []model.UserInventoryUsage
		if err := query.NotDeleted(tx).
			Where("delivery_order_id = ? AND status IN ?", deliveryID,
				[]int{int(model.InventoryUsagePendingShip), int(model.InventoryUsageCancelPending)}).
			Find(&usages).Error; err != nil {
			return err
		}
		if len(usages) == 0 && d.InventoryUsageID != nil {
			var u model.UserInventoryUsage
			if err := query.NotDeleted(tx).First(&u, *d.InventoryUsageID).Error; err == nil {
				// 仅允许备餐中的 usage 回退，避免已取消/已完成重复入账
				if u.Status == model.InventoryUsagePendingShip || u.Status == model.InventoryUsageCancelPending {
					usages = append(usages, u)
				}
			}
		}

		for i := range usages {
			u := usages[i]
			if u.Status != model.InventoryUsagePendingShip && u.Status != model.InventoryUsageCancelPending {
				continue
			}
			if s.InventorySvc == nil {
				return fmt.Errorf("inventory service unavailable")
			}
			var inv model.UserInventory
			if err := query.NotDeleted(tx).First(&inv, u.InventoryID).Error; err != nil {
				return err
			}
			if err := s.InventorySvc.restoreInventoryUseCancel(tx, &u, inv.Spec, &reasonText); err != nil {
				return err
			}
			if err := restorePackageComponentStock(tx, &u); err != nil {
				return err
			}
			if err := tx.Model(&u).Updates(map[string]interface{}{
				"status":        model.InventoryUsageCancelled,
				"cancel_reason": reasonText,
				"remark":        reasonText,
			}).Error; err != nil {
				return err
			}
			// 来源购买单备注拒因，便于用户在「全部订单」查看
			if u.SourceOrderID != nil {
				_ = tx.Model(&model.Order{}).Where("id = ?", *u.SourceOrderID).
					Update("remark", reasonText).Error
			}
		}

		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryCancelled,
			"exception_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		if s.DeliveryFeePaySvc != nil {
			if err := s.DeliveryFeePaySvc.RefundForDeliveryOrderInTx(tx, deliveryID, "取消配送退配送费"); err != nil {
				return err
			}
		}

		// 购买订单路径：回到待履约（已入背包），备注中保留拒单原因
		if d.OrderID != nil {
			_ = tx.Model(&model.Order{}).
				Where("id = ? AND status IN ?", *d.OrderID,
					[]int{int(model.OrderStatusPreparing), int(model.OrderStatusPendingShip)}).
				Updates(map[string]interface{}{
					"status":                model.OrderStatusPendingFulfill,
					"merchant_review_stage": model.MerchantReviewApproved,
					"remark":                reasonText,
				}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

// ReportException 骑手上报异常（已接单/配送中）。
func (s *DeliveryService) ReportException(riderID, deliveryID uint64, reason string) (*DeliveryView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写异常说明", ErrDeliveryStatusInvalid)
	}
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND rider_id = ?", deliveryID, riderID).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if d.Status != model.DeliveryAccepted && d.Status != model.DeliveryPicking && d.Status != model.DeliveryDelivering {
			return ErrDeliveryStatusInvalid
		}
		reasonText := "骑手上报：" + reason
		if d.ErrandOrderID != nil {
			_ = tx.Model(&model.ErrandOrder{}).
				Where("id = ?", *d.ErrandOrderID).
				Updates(map[string]interface{}{
					"status":           model.ErrandStatusException,
					"exception_reason": reasonText,
				}).Error
		}
		return tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryException,
			"exception_reason": reasonText,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.getRiderViewByID(deliveryID)
}

// ReportExceptionByUser 用户上报异常：仅骑手点击送达后（待确认收货）可报；进入管理端配送异常人工处理。
func (s *DeliveryService) ReportExceptionByUser(accountID, deliveryID uint64, reason string) (*DeliveryView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写异常说明", ErrDeliveryStatusInvalid)
	}
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ?", deliveryID).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if err := s.assertDeliveryOwner(tx, accountID, &d); err != nil {
			return err
		}
		// 仅待确认收货（骑手已送达）；配送中不可上报
		if d.Status != model.DeliveryDelivered {
			return ErrDeliveryStatusInvalid
		}
		reasonText := "用户上报：" + reason
		if d.ErrandOrderID != nil {
			_ = tx.Model(&model.ErrandOrder{}).
				Where("id = ?", *d.ErrandOrderID).
				Updates(map[string]interface{}{
					"status":           model.ErrandStatusException,
					"exception_reason": reasonText,
				}).Error
		}
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryException,
			"exception_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		aid := accountID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectDelivery,
			SubjectID:   deliveryID,
			EventCode:   model.EventExceptionReported,
			ActorRole:   model.FulfillmentActorUser,
			ActorID:     &aid,
			Title:       "用户发起申诉",
			Detail:      map[string]interface{}{"reason": reasonText},
		})
		if d.TakeoutOrderID != nil {
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectTakeout,
				SubjectID:   *d.TakeoutOrderID,
				EventCode:   model.EventExceptionReported,
				ActorRole:   model.FulfillmentActorUser,
				ActorID:     &aid,
				Title:       "用户发起申诉",
				Detail:      map[string]interface{}{"reason": reasonText},
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

// deliveryBelongsToMerchant ?????????????Order.MerchantID ??InventoryUsage.MerchantID???
func deliveryBelongsToMerchant(tx *gorm.DB, d *model.DeliveryOrder, merchantID uint64) bool {
	if d.OrderID != nil {
		var o model.Order
		if err := tx.Select("merchant_id").First(&o, *d.OrderID).Error; err == nil {
			return o.MerchantID == merchantID
		}
	}
	if d.InventoryUsageID != nil {
		var u model.UserInventoryUsage
		if err := tx.Select("merchant_id").First(&u, *d.InventoryUsageID).Error; err == nil {
			return u.MerchantID == merchantID
		}
	}
	return false
}

func (s *DeliveryService) Complete(riderID, deliveryID uint64, input CompleteDeliveryInput) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx).Select("errand_order_id").First(&d, deliveryID).Error; err == nil && IsErrand(&d) {
			return fmt.Errorf("%w: 跑腿单请使用送达收款接口", ErrDeliveryStatusInvalid)
		}
		if err := query.NotDeleted(tx).
			Where("id = ? AND rider_id = ? AND status IN ?", deliveryID, riderID,
				[]int{int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering)}).
			First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		now := time.Now()
		updates := map[string]interface{}{
			"status": model.DeliveryDelivered, "delivered_at": now,
		}
		if input.Remark != nil {
			updates["deliver_remark"] = *input.Remark
		}
		if len(input.Photos) > 0 {
			updates["deliver_photos"] = toJSONColumn(input.Photos)
		}
		if err := tx.Model(&d).Updates(updates).Error; err != nil {
			return err
		}
		if d.OrderID != nil {
			if err := tx.Model(&model.Order{}).Where("id = ?", *d.OrderID).
				Update("status", model.OrderStatusPendingConfirm).Error; err != nil {
				return err
			}
		}
		rid := riderID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectDelivery,
			SubjectID:   deliveryID,
			EventCode:   model.EventDelivered,
			ActorRole:   model.FulfillmentActorRider,
			ActorID:     &rid,
			Title:       "骑手已送达，待确认收货",
		})
		if d.TakeoutOrderID != nil {
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectTakeout,
				SubjectID:   *d.TakeoutOrderID,
				EventCode:   model.EventDelivered,
				ActorRole:   model.FulfillmentActorRider,
				ActorID:     &rid,
				Title:       "骑手已送达，待确认收货",
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getRiderViewByID(deliveryID)
}

func (s *DeliveryService) ConfirmReceipt(accountID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE ??????????????????
		q := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).Where("id = ? AND status = ?", deliveryID, model.DeliveryDelivered)
		if err := q.First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryStatusInvalid
			}
			return err
		}
		if err := s.assertDeliveryOwner(tx, accountID, &d); err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status": model.DeliveryConfirmed, "user_confirmed": 1,
		}).Error; err != nil {
			return err
		}
		if d.OrderID != nil {
			if err := tx.Model(&model.Order{}).Where("id = ?", *d.OrderID).
				Update("status", model.OrderStatusCompleted).Error; err != nil {
				return err
			}
		}
		if d.TakeoutOrderID != nil {
			if err := tx.Model(&model.TakeoutOrder{}).Where("id = ?", *d.TakeoutOrderID).
				Update("status", model.TakeoutStatusCompleted).Error; err != nil {
				return err
			}
		}
		if d.ErrandOrderID != nil && s.ErrandSvc != nil {
			var eo model.ErrandOrder
			if err := query.NotDeleted(tx).First(&eo, *d.ErrandOrderID).Error; err != nil {
				return ErrDeliveryNotFound
			}
			if err := s.ErrandSvc.confirmInTx(tx, &eo, model.FulfillmentActorUser); err != nil {
				return err
			}
			return nil
		}
		// 核销已完成 usage 时跳过；仅待发货 usage 推进为已完成
		usageQ := tx.Model(&model.UserInventoryUsage{}).
			Where("status = ?", model.InventoryUsagePendingShip)
		if d.InventoryUsageID != nil {
			usageQ = usageQ.Where("delivery_order_id = ? OR id = ?", d.ID, *d.InventoryUsageID)
		} else {
			usageQ = usageQ.Where("delivery_order_id = ?", d.ID)
		}
		if err := usageQ.Update("status", model.InventoryUsageCompleted).Error; err != nil {
			return err
		}
		// ???????????????? delivery_order ??????????
		// ???????????? > 0 ????????????????
		if d.RiderID != nil && d.RiderEarnings > 0 {
			merchantID := resolveDeliveryMerchantIDInTx(tx, &d)
			earning := model.RiderEarning{
				RiderID:         *d.RiderID,
				DeliveryOrderID: d.ID,
				OrderID:         d.OrderID,
				MerchantID:      merchantID,
				Amount:          d.RiderEarnings,
				Status:          model.RiderEarningPending,
				CreatedAt:       now,
			}
			if err := tx.Create(&earning).Error; err != nil {
				return err
			}
		}
		aid := accountID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectDelivery,
			SubjectID:   deliveryID,
			EventCode:   model.EventUserConfirmed,
			ActorRole:   model.FulfillmentActorUser,
			ActorID:     &aid,
			Title:       "用户已确认收货",
		})
		if d.TakeoutOrderID != nil {
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectTakeout,
				SubjectID:   *d.TakeoutOrderID,
				EventCode:   model.EventCompleted,
				ActorRole:   model.FulfillmentActorUser,
				ActorID:     &aid,
				Title:       "订单已完成",
			})
		}
		if d.OrderID != nil {
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectOrder,
				SubjectID:   *d.OrderID,
				EventCode:   model.EventCompleted,
				ActorRole:   model.FulfillmentActorUser,
				ActorID:     &aid,
				Title:       "订单已完成",
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) ConfirmReceiptByOrderID(accountID, orderID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).
		Where("order_id = ? AND status = ?", orderID, model.DeliveryDelivered).
		First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	return s.ConfirmReceipt(accountID, d.ID)
}

// resolveDeliveryMerchantIDInTx ???? order ??inventory_usage ???? ID?delivery_order ????merchant_id???
func resolveDeliveryMerchantIDInTx(tx *gorm.DB, d *model.DeliveryOrder) uint64 {
	if d.OrderID != nil {
		var o model.Order
		if err := tx.Select("merchant_id").First(&o, *d.OrderID).Error; err == nil {
			return o.MerchantID
		}
	}
	if d.InventoryUsageID != nil {
		var u model.UserInventoryUsage
		if err := tx.Select("merchant_id").First(&u, *d.InventoryUsageID).Error; err == nil {
			return u.MerchantID
		}
	}
	if d.TakeoutOrderID != nil {
		var to model.TakeoutOrder
		if err := tx.Select("merchant_id").First(&to, *d.TakeoutOrderID).Error; err == nil {
			return to.MerchantID
		}
	}
	return 0
}

func (s *DeliveryService) CreateForOrder(orderID uint64) (*model.DeliveryOrder, error) {
	d := model.DeliveryOrder{OrderID: &orderID, Status: model.DeliveryPendingAccept}
	if err := s.DB.Create(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *DeliveryService) userDeliveryQuery(accountID uint64) *gorm.DB {
	return query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where(
		`order_id IN (SELECT id FROM `+"`order`"+` WHERE account_id = ? AND is_deleted = 0)
		OR inventory_usage_id IN (SELECT id FROM user_inventory_usage WHERE account_id = ? AND is_deleted = 0)
		OR id IN (SELECT delivery_order_id FROM user_inventory_usage WHERE account_id = ? AND delivery_order_id IS NOT NULL AND is_deleted = 0)
		OR takeout_order_id IN (SELECT id FROM takeout_order WHERE account_id = ? AND is_deleted = 0)
		OR errand_order_id IN (SELECT id FROM errand_order WHERE account_id = ? AND is_deleted = 0)`,
		accountID, accountID, accountID, accountID, accountID,
	)
}

func (s *DeliveryService) assertDeliveryOwner(tx *gorm.DB, accountID uint64, d *model.DeliveryOrder) error {
	if d.OrderID != nil {
		var order model.Order
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.OrderID, accountID).First(&order).Error; err != nil {
			return ErrDeliveryForbidden
		}
		return nil
	}
	if d.TakeoutOrderID != nil {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.TakeoutOrderID, accountID).First(&to).Error; err != nil {
			return ErrDeliveryForbidden
		}
		return nil
	}
	if d.InventoryUsageID != nil {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.InventoryUsageID, accountID).First(&usage).Error; err != nil {
			// ??????? usage ????
			var n int64
			if err2 := query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
				Where("delivery_order_id = ? AND account_id = ?", d.ID, accountID).Count(&n).Error; err2 != nil || n == 0 {
				return ErrDeliveryForbidden
			}
			return nil
		}
		return nil
	}
	var n int64
	if err := query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
		Where("delivery_order_id = ? AND account_id = ?", d.ID, accountID).Count(&n).Error; err != nil || n == 0 {
		return ErrDeliveryForbidden
	}
	return nil
}

// ListPreparingForMerchant ????????????merchant_prepared=0??
// ????????order_id?????????inventory_usage_id???
func (s *DeliveryService) ListPreparingForMerchant(merchantID uint64, page, pageSize int) ([]DeliveryView, int64, error) {
	return s.listDeliveriesForMerchant(merchantID, page, pageSize, 0)
}

// ListPreparedForMerchant ???????????????merchant_prepared=1, status=PendingAccept???
func (s *DeliveryService) ListPreparedForMerchant(merchantID uint64, page, pageSize int) ([]DeliveryView, int64, error) {
	return s.listDeliveriesForMerchant(merchantID, page, pageSize, 1)
}

func (s *DeliveryService) listDeliveriesForMerchant(merchantID uint64, page, pageSize int, merchantPrepared uint8) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).
		Where("status = ? AND merchant_prepared = ?", model.DeliveryPendingAccept, merchantPrepared).
		Where("NOT (inventory_usage_id IS NOT NULL AND takeout_order_id IS NULL)").
		Where(
			"EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u2 WHERE u2.delivery_order_id = delivery_order.id AND u2.is_deleted = 0 AND u2.merchant_id = ?)",
			merchantID, merchantID, merchantID,
		)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toDeliveryViews(s.DB, list), total, nil
}

func (s *DeliveryService) getViewByID(id uint64) (*DeliveryView, error) {
	d, err := s.getByID(id)
	if err != nil {
		return nil, err
	}
	view := toDeliveryView(s.DB, *d)
	if view.Status == model.DeliveryException {
		view.StatusText = "申诉中"
	}
	return &view, nil
}

func (s *DeliveryService) getRiderViewByID(id uint64) (*DeliveryView, error) {
	d, err := s.getByID(id)
	if err != nil {
		return nil, err
	}
	view := toRiderDeliveryView(s.DB, *d)
	return &view, nil
}

// GetForRider ???????????????????????????????
func (s *DeliveryService) GetForRider(riderID, deliveryID uint64) (*DeliveryView, error) {
	d, err := s.getByID(deliveryID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	// ??????rider_id)????????????????
	if d.RiderID != nil && *d.RiderID != riderID {
		return nil, ErrDeliveryForbidden
	}
	view := toRiderDeliveryView(s.DB, *d)
	return &view, nil
}

func (s *DeliveryService) getByID(id uint64) (*model.DeliveryOrder, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).
		Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("Order.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func toDeliveryViews(db *gorm.DB, list []model.DeliveryOrder) []DeliveryView {
	views := make([]DeliveryView, 0, len(list))
	for i := range list {
		views = append(views, toDeliveryView(db, list[i]))
	}
	return views
}

func toDeliveryView(db *gorm.DB, d model.DeliveryOrder) DeliveryView {
	view := DeliveryView{
		DeliveryOrder: d,
		StatusText:    model.DeliveryStatusText(d.Status),
	}
	attachDeliveryUsageItems(db, &view)
	attachErrandDeliveryData(db, &view)
	if view.TakeoutOrderID != nil {
		attachTakeoutDeliveryData(db, &view)
	}
	attachDeliveryMerchant(db, &view)
	attachRiderContact(db, &view)
	finalizeDeliveryDisplay(&view)
	return view
}

func toRiderDeliveryViews(db *gorm.DB, list []model.DeliveryOrder) []DeliveryView {
	views := make([]DeliveryView, 0, len(list))
	for i := range list {
		views = append(views, toRiderDeliveryView(db, list[i]))
	}
	return views
}

func toRiderDeliveryView(db *gorm.DB, d model.DeliveryOrder) DeliveryView {
	view := toDeliveryView(db, d)
	attachRiderVerifyCode(db, &view)
	// 背包跑腿无出餐环节：骑手端不返回出餐号（含历史误写入的）
	if IsBagErrand(&view.DeliveryOrder) {
		view.PickupCode = ""
	}
	return view
}

func attachRiderContact(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil || view.RiderID == nil {
		return
	}
	var acc model.Account
	if err := query.NotDeleted(db).Select("id", "phone", "nickname").First(&acc, *view.RiderID).Error; err != nil {
		return
	}
	if acc.Phone != nil {
		view.RiderPhone = *acc.Phone
	}
	var app model.RiderApplication
	err := query.NotDeleted(db).
		Where("account_id = ? AND status = ?", *view.RiderID, model.RiderApplicationApproved).
		Order("id DESC").First(&app).Error
	if err == nil && app.RealName != "" {
		view.RiderName = app.RealName
	} else if acc.Nickname != nil {
		view.RiderName = *acc.Nickname
	}
	if view.RiderPhone == "" && app.Phone != "" {
		view.RiderPhone = app.Phone
	}
}

// attachRiderVerifyCode 背包跑腿配送单在骑手接单后附带核销码；外卖单永不返回。
func attachRiderVerifyCode(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil {
		return
	}
	if view.TakeoutOrderID != nil || view.InventoryUsageID == nil || view.RiderID == nil {
		return
	}
	usageIDs := make([]uint64, 0, 1+len(view.UsageItems))
	usageIDs = append(usageIDs, *view.InventoryUsageID)
	seen := map[uint64]struct{}{*view.InventoryUsageID: {}}
	for _, line := range view.UsageItems {
		if line.UsageID == 0 {
			continue
		}
		if _, ok := seen[line.UsageID]; ok {
			continue
		}
		seen[line.UsageID] = struct{}{}
		usageIDs = append(usageIDs, line.UsageID)
	}
	var codes []string
	for _, uid := range usageIDs {
		var vc model.VerificationCode
		if err := query.NotDeleted(db).
			Where("inventory_usage_id = ? AND status = ?", uid, model.VerificationCodeUnused).
			First(&vc).Error; err != nil {
			continue
		}
		codes = append(codes, vc.Code)
	}
	if len(codes) == 0 {
		return
	}
	joined := strings.Join(codes, "、")
	view.VerifyCode = &joined
}

func attachDeliveryUsageItems(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil {
		return
	}
	var usages []model.UserInventoryUsage
	q := query.NotDeleted(db).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("delivery_order_id = ?", view.ID).
		Order("id ASC")
	if err := q.Find(&usages).Error; err != nil || len(usages) == 0 {
		// ???? inventory_usage_id ????
		if view.InventoryUsageID != nil {
			var u model.UserInventoryUsage
			if err := query.NotDeleted(db).
				Preload("Product", "is_deleted = ?", model.NotDeleted).
				First(&u, *view.InventoryUsageID).Error; err == nil {
				usages = []model.UserInventoryUsage{u}
			}
		}
	}
	if len(usages) == 0 {
		return
	}
	if view.DeliveryTimeRemark == "" && usages[0].Remark != nil && *usages[0].Remark != "" {
		view.DeliveryTimeRemark = *usages[0].Remark
	}
	lines := make([]DeliveryUsageLine, 0, len(usages))
	for i := range usages {
		u := usages[i]
		name := ""
		cover := ""
		unitPrice := 0.0
		isPkg := false
		if u.Product != nil {
			name = u.Product.Name
			cover = u.Product.CoverURL
			unitPrice = u.Product.Price
			isPkg = u.Product.ItemType == model.ProductItemTypePackage
		}
		if unitPrice <= 0 && u.SourceOrderID != nil {
			var oi model.OrderItem
			if err := query.NotDeleted(db).
				Select("unit_price", "product_image", "product_name").
				Where("order_id = ? AND product_id = ?", *u.SourceOrderID, u.ProductID).
				Order("id ASC").Limit(1).Find(&oi).Error; err == nil && oi.UnitPrice > 0 {
				unitPrice = oi.UnitPrice
				if cover == "" && oi.ProductImage != nil {
					cover = *oi.ProductImage
				}
				if name == "" {
					name = oi.ProductName
				}
			}
		}
		lines = append(lines, DeliveryUsageLine{
			UsageID: u.ID, ProductID: u.ProductID, ProductName: name,
			ProductImage: cover, UnitPrice: unitPrice,
			Quantity: u.Quantity, IsPackage: isPkg,
			PackageSelectionText: u.PackageSelections.SummaryText(),
			OptionSelectionText:  u.OptionSelections.SummaryText(),
			OptionSelections:     u.OptionSelections,
		})
	}
	view.UsageItems = lines
	if view.Merchant == nil && usages[0].MerchantProfile != nil {
		view.Merchant = merchantBriefFromProfile(usages[0].MerchantProfile)
	}
}

// attachErrandDeliveryData 跑腿单：取送地址、商品描述、款项、取货点摘要。
func attachErrandDeliveryData(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil || view.ErrandOrderID == nil {
		return
	}
	var eo model.ErrandOrder
	if err := query.NotDeleted(db).First(&eo, *view.ErrandOrderID).Error; err != nil {
		return
	}
	// 未接单前不向骑手暴露券码/券码截图，接单后才可查看核销
	if view.RiderID == nil {
		eo.VoucherCode = ""
		eo.VoucherPhotos = nil
		eo.GoodsReceiptPhotos = nil
	}
	view.Errand = &eo
	view.AddressSnapshot = eo.DropSnapshot
	view.GoodsAmount = eo.GoodsAmount
	view.TotalAmount = eo.PayAmount
	if eo.Remark != nil {
		view.DeliveryTimeRemark = *eo.Remark
	}
	if eo.PickupSnapshot != nil {
		view.Merchant = &DeliveryMerchantBrief{
			ShopName:     eo.PickupSnapshot.ShopName,
			Address:      eo.PickupSnapshot.Address,
			ContactPhone: eo.PickupSnapshot.ContactPhone,
			Latitude:     eo.PickupSnapshot.Latitude,
			Longitude:    eo.PickupSnapshot.Longitude,
		}
	}
}

func attachTakeoutDeliveryData(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil || view.TakeoutOrderID == nil {
		return
	}
	var to model.TakeoutOrder
	if err := query.NotDeleted(db).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		First(&to, *view.TakeoutOrderID).Error; err != nil {
		return
	}
	view.AddressSnapshot = to.AddressSnapshot
	view.DeliveryTimeRemark = to.DeliveryTimeRemark
	view.GoodsAmount = to.GoodsAmount
	view.TotalAmount = to.GoodsAmount
	if view.DeliveryFee <= 0 && to.DeliveryFee > 0 {
		view.DeliveryFee = to.DeliveryFee
	}
	if view.RiderEarnings <= 0 && to.RiderEarnings > 0 {
		view.RiderEarnings = to.RiderEarnings
	}

	itemByPID := map[uint64]model.TakeoutOrderItem{}
	for i := range to.Items {
		itemByPID[to.Items[i].ProductID] = to.Items[i]
	}

	_, _, selLines := buildTakeoutSelectionDisplay(db, &to)
	if len(selLines) == 0 {
		// 无选配行时仍用外卖明细兜底
		lines := make([]DeliveryUsageLine, 0, len(to.Items))
		for _, it := range to.Items {
			cover := ""
			if it.ProductImage != nil {
				cover = *it.ProductImage
			}
			lines = append(lines, DeliveryUsageLine{
				ProductID: it.ProductID, ProductName: it.ProductName,
				ProductImage: cover, UnitPrice: it.UnitPrice, Quantity: it.Quantity,
			})
		}
		view.UsageItems = lines
	} else {
		lines := make([]DeliveryUsageLine, 0, len(selLines))
		for _, ln := range selLines {
			cover := ""
			unitPrice := 0.0
			if it, ok := itemByPID[ln.ProductID]; ok {
				if it.ProductImage != nil {
					cover = *it.ProductImage
				}
				unitPrice = it.UnitPrice
			}
			if cover == "" && ln.ProductID != 0 {
				var p model.Product
				if err := query.NotDeleted(db).Select("id", "cover_url", "price").First(&p, ln.ProductID).Error; err == nil {
					cover = p.CoverURL
					if unitPrice <= 0 {
						unitPrice = p.Price
					}
				}
			}
			lines = append(lines, DeliveryUsageLine{
				ProductID:            ln.ProductID,
				ProductName:          ln.ProductName,
				ProductImage:         cover,
				UnitPrice:            unitPrice,
				Quantity:             ln.Quantity,
				IsPackage:            ln.IsPackage,
				PackageSelectionText: ln.PackageSelectionText,
				OptionSelectionText:  ln.OptionSelectionText,
			})
		}
		view.UsageItems = lines
	}

	if view.Merchant == nil {
		var mp model.MerchantProfile
		if err := query.NotDeleted(db).First(&mp, to.MerchantID).Error; err == nil {
			view.Merchant = merchantBriefFromProfile(&mp)
		}
	}
}

func merchantBriefFromProfile(mp *model.MerchantProfile) *DeliveryMerchantBrief {
	if mp == nil {
		return nil
	}
	addr, phone := "", ""
	if mp.Address != nil {
		addr = *mp.Address
	}
	if mp.ContactPhone != nil {
		phone = *mp.ContactPhone
	}
	return &DeliveryMerchantBrief{
		ID:           mp.ID,
		ShopName:     mp.ShopName,
		Address:      addr,
		ContactPhone: phone,
		Latitude:     mp.Latitude,
		Longitude:    mp.Longitude,
	}
}

func attachDeliveryMerchant(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil || view.Merchant != nil {
		return
	}
	if view.Order != nil && view.Order.MerchantProfile != nil {
		view.Merchant = merchantBriefFromProfile(view.Order.MerchantProfile)
		return
	}
	if view.InventoryUsage != nil && view.InventoryUsage.MerchantProfile != nil {
		view.Merchant = merchantBriefFromProfile(view.InventoryUsage.MerchantProfile)
		return
	}
	if view.OrderID != nil {
		var o model.Order
		if err := query.NotDeleted(db).Select("id", "merchant_id").First(&o, *view.OrderID).Error; err == nil && o.MerchantID > 0 {
			var mp model.MerchantProfile
			if err := query.NotDeleted(db).First(&mp, o.MerchantID).Error; err == nil {
				view.Merchant = merchantBriefFromProfile(&mp)
			}
		}
	}
}

func finalizeDeliveryDisplay(view *DeliveryView) {
	if view == nil {
		return
	}
	var qty uint32
	var goods float64
	names := make([]string, 0, len(view.UsageItems))
	for _, ln := range view.UsageItems {
		qty += ln.Quantity
		goods = roundMoney(goods + ln.UnitPrice*float64(ln.Quantity))
		if ln.ProductName != "" {
			names = append(names, fmt.Sprintf("%s×%d", ln.ProductName, ln.Quantity))
		}
		if view.ProductImage == "" && ln.ProductImage != "" {
			view.ProductImage = ln.ProductImage
		}
	}
	if view.Quantity == 0 {
		view.Quantity = qty
	}
	if view.ProductName == "" && len(names) > 0 {
		view.ProductName = strings.Join(names, "、")
	}
	if view.GoodsAmount <= 0 && goods > 0 {
		view.GoodsAmount = goods
	}
	if view.TotalAmount <= 0 {
		view.TotalAmount = view.GoodsAmount
	}
	// 订单配送：从订单行补图/价/名
	if view.ProductImage == "" && view.Order != nil && len(view.Order.Items) > 0 {
		it := view.Order.Items[0]
		if it.ProductImage != nil {
			view.ProductImage = *it.ProductImage
		}
		if view.ProductName == "" {
			view.ProductName = it.ProductName
		}
		if view.Quantity == 0 {
			view.Quantity = it.Quantity
		}
		if view.TotalAmount <= 0 {
			view.TotalAmount = roundMoney(it.UnitPrice * float64(it.Quantity))
			view.GoodsAmount = view.TotalAmount
		}
	}
	if view.ProductImage == "" && view.InventoryUsage != nil && view.InventoryUsage.Product != nil {
		view.ProductImage = view.InventoryUsage.Product.CoverURL
		if view.ProductName == "" {
			view.ProductName = view.InventoryUsage.Product.Name
		}
	}
	if view.Errand != nil {
		view.ProductName = view.Errand.GoodsDesc
		if len(view.UsageItems) > 0 {
			view.UsageItems = nil
		}
		view.GoodsAmount = view.Errand.GoodsAmount
		view.TotalAmount = view.Errand.PayAmount
		if view.TotalAmount <= 0 {
			view.TotalAmount = view.Errand.DeliveryFee
		}
	}
}

// ListForAdmin 管理端配送单列表。
func (s *DeliveryService) ListForAdmin(merchantID *uint64, status *uint8, page, pageSize int, keyword string, startDate, endDate *time.Time) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	accountIDs, empty, err := FindAccountIDsByKeyword(s.DB, keyword)
	if err != nil {
		return nil, 0, err
	}
	if empty {
		return []DeliveryView{}, 0, nil
	}

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{}))
	if merchantID != nil {
		q = q.Where(
			"EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM takeout_order t WHERE t.id = delivery_order.takeout_order_id AND t.is_deleted = 0 AND t.merchant_id = ?)",
			*merchantID, *merchantID, *merchantID,
		)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if len(accountIDs) > 0 {
		q = q.Where(
			"EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.account_id IN ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.account_id IN ?) OR "+
				"EXISTS (SELECT 1 FROM takeout_order t WHERE t.id = delivery_order.takeout_order_id AND t.is_deleted = 0 AND t.account_id IN ?)",
			accountIDs, accountIDs, accountIDs,
		)
	}
	if startDate != nil {
		q = q.Where("created_at >= ?", *startDate)
	}
	if endDate != nil {
		q = q.Where("created_at < ?", *endDate)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toDeliveryViews(s.DB, list), total, nil
}

func requireExceptionLocked(tx *gorm.DB, deliveryID uint64) (*model.DeliveryOrder, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		First(&d, deliveryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	if d.Status != model.DeliveryException {
		return nil, ErrDeliveryStatusInvalid
	}
	return &d, nil
}

func appendAdminRemark(old *string, remark string) string {
	base := ""
	if old != nil {
		base = *old
	}
	note := "管理员处理：" + strings.TrimSpace(remark)
	if base == "" {
		return note
	}
	return note + "；原上报：" + base
}

func validateAdminResolveRemark(remark string) (string, error) {
	remark = strings.TrimSpace(remark)
	if remark == "" {
		return "", fmt.Errorf("%w: 请填写处理备注", ErrDeliveryStatusInvalid)
	}
	return remark, nil
}

func resumeTargetStatus(d *model.DeliveryOrder) uint8 {
	// 用户在「待确认收货」上报异常后恢复：回到待确认
	if d.DeliveredAt != nil {
		return model.DeliveryDelivered
	}
	if d.StartedAt != nil {
		return model.DeliveryDelivering
	}
	return model.DeliveryAccepted
}

// errandStatusForDelivery 将配送进度映射回跑腿单状态。
func errandStatusForDelivery(deliveryStatus uint8) (uint8, bool) {
	switch deliveryStatus {
	case model.DeliveryAccepted:
		return model.ErrandStatusAccepted, true
	case model.DeliveryDelivering:
		return model.ErrandStatusDelivering, true
	case model.DeliveryDelivered:
		return model.ErrandStatusPendingConfirm, true
	}
	return 0, false
}

func (s *DeliveryService) deliveryPaymentProvider() payment.Provider {
	if s.DeliveryFeePaySvc != nil {
		return s.DeliveryFeePaySvc.paymentProvider()
	}
	return &payment.MockProvider{DB: s.DB}
}

func cancelPendingRiderEarningsInTx(tx *gorm.DB, deliveryID uint64) error {
	return tx.Model(&model.RiderEarning{}).
		Where("delivery_order_id = ? AND status = ?", deliveryID, model.RiderEarningPending).
		Update("status", model.RiderEarningCancelled).Error
}

func (s *DeliveryService) restoreDeliveryUsagesForCancelInTx(tx *gorm.DB, d *model.DeliveryOrder, deliveryID uint64, reasonText string) error {
	// 含 Completed：跑腿到店核销后 usage 已完成，管理员取消异常仍须回包（AdminResolveCancel 文档约定）。
	restorable := []int{
		int(model.InventoryUsagePendingShip),
		int(model.InventoryUsageCancelPending),
		int(model.InventoryUsageCompleted),
	}
	var usages []model.UserInventoryUsage
	if err := query.NotDeleted(tx).
		Where("delivery_order_id = ? AND status IN ?", deliveryID, restorable).
		Find(&usages).Error; err != nil {
		return err
	}
	if len(usages) == 0 && d.InventoryUsageID != nil {
		var u model.UserInventoryUsage
		if err := query.NotDeleted(tx).First(&u, *d.InventoryUsageID).Error; err == nil {
			switch u.Status {
			case model.InventoryUsagePendingShip, model.InventoryUsageCancelPending, model.InventoryUsageCompleted:
				usages = append(usages, u)
			}
		}
	}
	for i := range usages {
		u := usages[i]
		switch u.Status {
		case model.InventoryUsagePendingShip, model.InventoryUsageCancelPending, model.InventoryUsageCompleted:
		default:
			continue
		}
		if s.InventorySvc == nil {
			return fmt.Errorf("inventory service unavailable")
		}
		var inv model.UserInventory
		if err := query.NotDeleted(tx).First(&inv, u.InventoryID).Error; err != nil {
			return err
		}
		if err := s.InventorySvc.restoreInventoryUseCancel(tx, &u, inv.Spec, &reasonText); err != nil {
			return err
		}
		if err := restorePackageComponentStock(tx, &u); err != nil {
			return err
		}
		if err := tx.Model(&u).Updates(map[string]interface{}{
			"status":        model.InventoryUsageCancelled,
			"cancel_reason": reasonText,
			"remark":        reasonText,
		}).Error; err != nil {
			return err
		}
		if err := InvalidateVerificationRecordsForUsage(tx, u.ID); err != nil {
			return err
		}
		if u.SourceOrderID != nil {
			_ = tx.Model(&model.Order{}).Where("id = ?", *u.SourceOrderID).
				Update("remark", reasonText).Error
		}
	}
	return nil
}

func (s *DeliveryService) cancelPaidTakeoutForDeliveryInTx(tx *gorm.DB, takeoutID uint64, reasonText string) error {
	var to model.TakeoutOrder
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&to, takeoutID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDeliveryNotFound
		}
		return err
	}
	if to.Status == model.TakeoutStatusCancelled {
		return nil
	}
	if to.PayStatus != model.PayStatusPaid {
		return ErrDeliveryStatusInvalid
	}
	sub, err := payment.TakeoutSubjectFromID(tx, takeoutID, 0)
	if err != nil {
		return err
	}
	if err := s.deliveryPaymentProvider().RefundSubjectAmountInTx(tx, sub, to.PayAmount, reasonText); err != nil {
		return err
	}
	if err := restoreTakeoutStockInTx(tx, &to); err != nil {
		return err
	}
	return tx.Model(&to).Updates(map[string]interface{}{
		"status": model.TakeoutStatusCancelled,
		"remark": reasonText,
	}).Error
}

func revertLinkedOrderOnDeliveryCancelInTx(tx *gorm.DB, orderID *uint64, reasonText string) {
	if orderID == nil {
		return
	}
	_ = tx.Model(&model.Order{}).
		Where("id = ? AND status IN ?", *orderID,
			[]int{int(model.OrderStatusPreparing), int(model.OrderStatusPendingShip)}).
		Updates(map[string]interface{}{
			"status":                model.OrderStatusPendingFulfill,
			"merchant_review_stage": model.MerchantReviewApproved,
			"remark":                reasonText,
		}).Error
}

// AdminResolveResume 管理员恢复异常配送：保留骑手，按是否已开始配送回到配送中/已接单。
func (s *DeliveryService) AdminResolveResume(deliveryID uint64, remark string) (*DeliveryView, error) {
	remark, err := validateAdminResolveRemark(remark)
	if err != nil {
		return nil, err
	}
	var jobs []payment.RefundJob
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		d, err := requireExceptionLocked(tx, deliveryID)
		if err != nil {
			return err
		}
		newReason := appendAdminRemark(d.ExceptionReason, remark)
		newStatus := resumeTargetStatus(d)
		if err := tx.Model(d).Updates(map[string]interface{}{
			"status":           newStatus,
			"exception_reason": newReason,
		}).Error; err != nil {
			return err
		}
		// 跑腿单同步恢复业务状态，否则骑手端仍按异常处理、无法继续配送
		if d.ErrandOrderID != nil {
			if errStatus, ok := errandStatusForDelivery(newStatus); ok {
				_ = tx.Model(&model.ErrandOrder{}).
					Where("id = ? AND status = ?", *d.ErrandOrderID, model.ErrandStatusException).
					Update("status", errStatus).Error
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

// AdminResolveReassign 管理员改派：清空骑手与接单时间，回到待接单。
func (s *DeliveryService) AdminResolveReassign(deliveryID uint64, remark string) (*DeliveryView, error) {
	remark, err := validateAdminResolveRemark(remark)
	if err != nil {
		return nil, err
	}
	var jobs []payment.RefundJob
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		d, err := requireExceptionLocked(tx, deliveryID)
		if err != nil {
			return err
		}
		if err := tx.Model(d).Updates(map[string]interface{}{
			"status":           model.DeliveryPendingAccept,
			"rider_id":         nil,
			"accepted_at":      nil,
			"started_at":       nil,
			"exception_reason": appendAdminRemark(d.ExceptionReason, remark),
		}).Error; err != nil {
			return err
		}
		// 跑腿单回到待接单，否则新骑手接单时状态校验不通过
		if d.ErrandOrderID != nil {
			_ = tx.Model(&model.ErrandOrder{}).
				Where("id = ? AND status = ?", *d.ErrandOrderID, model.ErrandStatusException).
				Update("status", model.ErrandStatusPendingAccept).Error
		}
		return cancelPendingRiderEarningsInTx(tx, deliveryID)
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

// AdminResolveCancel 管理员取消异常配送：外卖全额退款，背包/跑腿回退并退配送费。
func (s *DeliveryService) AdminResolveCancel(deliveryID uint64, remark string) (*DeliveryView, error) {
	remark, err := validateAdminResolveRemark(remark)
	if err != nil {
		return nil, err
	}
	var jobs []payment.RefundJob
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		d, err := requireExceptionLocked(tx, deliveryID)
		if err != nil {
			return err
		}
		reasonText := appendAdminRemark(d.ExceptionReason, remark)

		if d.ErrandOrderID != nil {
			var eo model.ErrandOrder
			if err := query.NotDeleted(tx).First(&eo, *d.ErrandOrderID).Error; err != nil {
				return ErrDeliveryNotFound
			}
			if eo.PayMode == model.ErrandPayModePlatform && eo.PayStatus == model.PayStatusPaid {
				sub, err := payment.ErrandSubjectFromID(tx, eo.ID, 0)
				if err != nil {
					return err
				}
				if err := s.deliveryPaymentProvider().RefundSubjectAmountInTx(tx, sub, 0, "管理员取消跑腿退款"); err != nil {
					return err
				}
			}
			if err := tx.Model(&eo).Updates(map[string]interface{}{
				"status":           model.ErrandStatusCancelled,
				"cancel_reason":    reasonText,
				"exception_reason": reasonText,
			}).Error; err != nil {
				return err
			}
			if err := tx.Model(d).Updates(map[string]interface{}{
				"status":           model.DeliveryCancelled,
				"exception_reason": reasonText,
			}).Error; err != nil {
				return err
			}
			return cancelPendingRiderEarningsInTx(tx, deliveryID)
		}

		if d.TakeoutOrderID != nil {
			if err := s.cancelPaidTakeoutForDeliveryInTx(tx, *d.TakeoutOrderID, reasonText); err != nil {
				return err
			}
		} else {
			if err := s.restoreDeliveryUsagesForCancelInTx(tx, d, deliveryID, reasonText); err != nil {
				return err
			}
			if s.DeliveryFeePaySvc != nil {
				if err := s.DeliveryFeePaySvc.RefundForDeliveryOrderInTx(tx, deliveryID, "取消配送退配送费"); err != nil {
					return err
				}
			}
		}

		if err := tx.Model(d).Updates(map[string]interface{}{
			"status":           model.DeliveryCancelled,
			"exception_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		revertLinkedOrderOnDeliveryCancelInTx(tx, d.OrderID, reasonText)
		return cancelPendingRiderEarningsInTx(tx, deliveryID)
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

func enrichBagReviewView(db *gorm.DB, d model.DeliveryOrder) BagDeliveryReviewView {
	return enrichExceptionReviewView(db, d)
}

// enrichExceptionReviewView 管理端异常/审核视图：客户账号、收货联系人、商家、骑手信息。
func enrichExceptionReviewView(db *gorm.DB, d model.DeliveryOrder) BagDeliveryReviewView {
	base := toDeliveryView(db, d)
	out := BagDeliveryReviewView{DeliveryView: base}

	if d.InventoryUsageID != nil {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(db).Preload("MerchantProfile").First(&usage, *d.InventoryUsageID).Error; err == nil {
			out.AccountID = usage.AccountID
			if usage.AddressSnapshot != nil {
				out.ContactPhone = usage.AddressSnapshot.ContactPhone
				out.ContactName = usage.AddressSnapshot.ContactName
			}
			if usage.MerchantProfile != nil {
				out.MerchantName = usage.MerchantProfile.ShopName
			}
		}
	}
	if out.AccountID == 0 && d.TakeoutOrderID != nil {
		var to model.TakeoutOrder
		if err := query.NotDeleted(db).First(&to, *d.TakeoutOrderID).Error; err == nil {
			out.AccountID = to.AccountID
			if to.AddressSnapshot != nil {
				out.ContactPhone = to.AddressSnapshot.ContactPhone
				out.ContactName = to.AddressSnapshot.ContactName
			}
			var mp model.MerchantProfile
			if err := query.NotDeleted(db).Select("id", "shop_name").First(&mp, to.MerchantID).Error; err == nil {
				out.MerchantName = mp.ShopName
			}
		}
	}
	if out.AccountID == 0 && d.OrderID != nil {
		var order model.Order
		if err := query.NotDeleted(db).First(&order, *d.OrderID).Error; err == nil {
			out.AccountID = order.AccountID
			if order.AddressSnapshot != nil {
				out.ContactPhone = order.AddressSnapshot.ContactPhone
				out.ContactName = order.AddressSnapshot.ContactName
			}
			var mp model.MerchantProfile
			if err := query.NotDeleted(db).Select("id", "shop_name").First(&mp, order.MerchantID).Error; err == nil {
				out.MerchantName = mp.ShopName
			}
		}
	}
	if out.ContactPhone == "" && base.AddressSnapshot != nil {
		out.ContactPhone = base.AddressSnapshot.ContactPhone
		out.ContactName = base.AddressSnapshot.ContactName
	}

	if out.AccountID != 0 {
		var acc model.Account
		if err := query.NotDeleted(db).Select("id", "phone", "nickname").First(&acc, out.AccountID).Error; err == nil {
			if acc.Phone != nil {
				out.AccountPhone = *acc.Phone
			}
			if acc.Nickname != nil {
				out.Nickname = *acc.Nickname
			}
		}
	}
	return out
}

func (s *DeliveryService) ListPendingBagReviews(page, pageSize int) ([]BagDeliveryReviewView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).
		Where("status = ? AND inventory_usage_id IS NOT NULL AND takeout_order_id IS NULL",
			model.DeliveryPendingAdminReview)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]BagDeliveryReviewView, 0, len(list))
	for i := range list {
		out = append(out, enrichBagReviewView(s.DB, list[i]))
	}
	return out, total, nil
}

func (s *DeliveryService) CountPendingBagReviews() (int64, error) {
	var n int64
	err := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).
		Where("status = ? AND inventory_usage_id IS NOT NULL AND takeout_order_id IS NULL",
			model.DeliveryPendingAdminReview).Count(&n).Error
	return n, err
}

func (s *DeliveryService) ApproveBagDelivery(deliveryID uint64) (*DeliveryView, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var d model.DeliveryOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&d, deliveryID).Error; err != nil {
			return ErrDeliveryNotFound
		}
		if !IsBagErrand(&d) || d.Status != model.DeliveryPendingAdminReview {
			return fmt.Errorf("%w: 当前状态不可审核通过", ErrDeliveryStatusInvalid)
		}
		return tx.Model(&d).Update("status", model.DeliveryPendingAccept).Error
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) CancelPendingBagByUser(accountID, deliveryID uint64) (*DeliveryView, error) {
	reasonText := "用户取消"
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		var d model.DeliveryOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&d, deliveryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if err := s.assertDeliveryOwner(tx, accountID, &d); err != nil {
			return err
		}
		if !IsBagErrand(&d) || d.Status != model.DeliveryPendingAdminReview {
			return fmt.Errorf("%w: 当前状态不可取消", ErrDeliveryStatusInvalid)
		}
		if err := s.restoreDeliveryUsagesForCancelInTx(tx, &d, deliveryID, reasonText); err != nil {
			return err
		}
		if s.DeliveryFeePaySvc != nil {
			if err := s.DeliveryFeePaySvc.RefundForDeliveryOrderInTx(tx, deliveryID, "用户取消跑腿退配送费"); err != nil {
				return err
			}
		}
		return tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryCancelled,
			"exception_reason": reasonText,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) RejectBagDelivery(deliveryID uint64, reasonKey, remark string) (*DeliveryView, error) {
	reasonText, err := FormatBagAdminRejectReason(reasonKey, remark)
	if err != nil {
		return nil, err
	}
	var jobs []payment.RefundJob
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		var d model.DeliveryOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&d, deliveryID).Error; err != nil {
			return ErrDeliveryNotFound
		}
		if !IsBagErrand(&d) || d.Status != model.DeliveryPendingAdminReview {
			return fmt.Errorf("%w: 当前状态不可拒绝", ErrDeliveryStatusInvalid)
		}
		if err := s.restoreDeliveryUsagesForCancelInTx(tx, &d, deliveryID, reasonText); err != nil {
			return err
		}
		if s.DeliveryFeePaySvc != nil {
			if err := s.DeliveryFeePaySvc.RefundForDeliveryOrderInTx(tx, deliveryID, "平台拒绝跑腿退配送费"); err != nil {
				return err
			}
		}
		return tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryCancelled,
			"exception_reason": reasonText,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

// ListExceptions 管理端配送异常列表。
// scope=pending（默认）待处理；resolved 已处理（exception_reason 含管理员处理备注且状态已离开异常）。
func (s *DeliveryService) ListExceptions(scope string, page, pageSize int) ([]BagDeliveryReviewView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	scope = strings.TrimSpace(strings.ToLower(scope))
	if scope == "" || scope == "pending" || scope == "open" {
		scope = "pending"
	} else if scope == "resolved" || scope == "history" || scope == "done" {
		scope = "resolved"
	} else {
		scope = "pending"
	}

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{}))
	if scope == "resolved" {
		// 恢复/改派/取消后会写入「管理员处理：」前缀，且状态离开异常
		q = q.Where("status <> ? AND exception_reason LIKE ?", model.DeliveryException, "管理员处理：%")
	} else {
		q = q.Where("status = ?", model.DeliveryException)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Order("updated_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]BagDeliveryReviewView, 0, len(list))
	for i := range list {
		out = append(out, enrichExceptionReviewView(s.DB, list[i]))
	}
	return out, total, nil
}

func FormatBagAdminRejectReason(reasonKey, remark string) (string, error) {
	label := map[string]string{
		"unclear_address": "地址不清",
		"out_of_zone":     "超区",
		"unreachable":     "联系不上",
		"other":           "其它",
	}[reasonKey]
	if label == "" {
		return "", fmt.Errorf("%w: 请选择拒绝原因", ErrInventoryUsageInvalid)
	}
	remark = strings.TrimSpace(remark)
	if reasonKey == "other" && remark == "" {
		return "", fmt.Errorf("%w: 选择其它时请填写备注", ErrInventoryUsageInvalid)
	}
	out := "平台拒绝：" + label
	if remark != "" {
		out += "｜" + remark
	}
	return out, nil
}
