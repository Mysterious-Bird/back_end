package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrInventoryNotFound           = errors.New("inventory not found")
	ErrInventoryInsufficient       = errors.New("inventory insufficient")
	ErrInventoryUsageNotFound      = errors.New("inventory usage not found")
	ErrInventoryUsageInvalid       = errors.New("inventory usage invalid")
	ErrInventoryRollback           = errors.New("inventory rollback failed")
	ErrInventoryCancelPending      = errors.New("inventory cancel pending review")
	ErrVirtualNotDeliverable       = errors.New("virtual product not deliverable")
	ErrDeliveryNotAllowed          = errors.New("product does not allow delivery")
	ErrDeliveryFeePaymentRequired  = errors.New("delivery fee payment required")
)

type InventoryService struct {
	DB               *gorm.DB
	ZoneSvc          *DeliveryZoneService
	DeliveryFeePaySvc *DeliveryFeePayService
}

func (s *InventoryService) GetOwned(accountID, inventoryID uint64) (*model.UserInventory, error) {
	var inv model.UserInventory
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND account_id = ?", inventoryID, accountID).
		First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryNotFound
		}
		return nil, err
	}
	return &inv, nil
}

type UseInventoryInput struct {
	Quantity          uint32
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
	PackageSelections []PackageSelectionInput
	OptionSelections  []OptionSelectionUnitInput
}

type InventoryUsageView struct {
	model.UserInventoryUsage
	StatusText               string  `json:"status_text"`
	VerifyCode               *string `json:"verify_code,omitempty"`
	Buyer                    *BuyerBrief `json:"buyer,omitempty"`
	IsPackage                bool    `json:"is_package"`
	HasOptions               bool    `json:"has_options"`
	PackageSelectStatusText  string  `json:"package_select_status_text,omitempty"`
	PackageSelectionText     string  `json:"package_selection_text,omitempty"`
	OptionSelectionText      string  `json:"option_selection_text,omitempty"`
}

func (s *InventoryService) CreditFromOrder(tx *gorm.DB, accountID, orderID uint64, items []model.OrderItem) error {
	var credited int64
	if err := query.NotDeleted(tx.Model(&model.UserInventoryLog{})).
		Where("order_id = ? AND event_type = ?", orderID, model.InventoryEventOrderCredit).
		Count(&credited).Error; err != nil {
		return err
	}
	if credited > 0 {
		return nil
	}

	for _, it := range items {
		spec := orderItemSpec(it)
		if err := s.adjustQuantity(tx, accountID, it.ProductID, spec, int32(it.Quantity), &orderID, nil, model.InventoryEventOrderCredit, nil); err != nil {
			return err
		}
		if err := tx.Model(&model.UserInventory{}).
			Where("account_id = ? AND product_id = ? AND spec = ? AND is_deleted = 0", accountID, it.ProductID, spec).
			Update("last_order_id", orderID).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *InventoryService) RollbackOrderCredit(tx *gorm.DB, orderID uint64) error {
	var logs []model.UserInventoryLog
	if err := query.NotDeleted(tx).
		Where("order_id = ?", orderID).
		Find(&logs).Error; err != nil {
		return err
	}
	if len(logs) == 0 {
		return nil
	}

	type balKey struct {
		AccountID uint64
		ProductID uint64
		Spec      string
	}
	type balVal struct {
		net     int32
		hadUse  bool
		hadCred bool
	}
	bals := map[balKey]*balVal{}
	keys := make([]balKey, 0)
	for _, lg := range logs {
		k := balKey{AccountID: lg.AccountID, ProductID: lg.ProductID, Spec: lg.Spec}
		b, ok := bals[k]
		if !ok {
			b = &balVal{}
			bals[k] = b
			keys = append(keys, k)
		}
		b.net += lg.DeltaQty
		switch lg.EventType {
		case model.InventoryEventOrderCredit:
			b.hadCred = true
		case model.InventoryEventUse:
			b.hadUse = true
		}
	}

	hadCred := false
	stillInBag := int32(0)
	for _, k := range keys {
		b := bals[k]
		if b.hadCred {
			hadCred = true
		}
		if b.net > 0 {
			stillInBag += b.net
		}
	}
	// 已入账但净余额为 0：说明已使用完毕，无法整单回滚
	if hadCred && stillInBag <= 0 {
		return ErrInventoryRollback
	}
	if stillInBag <= 0 {
		return nil
	}

	for _, k := range keys {
		need := bals[k].net
		if need <= 0 {
			continue
		}
		var inv model.UserInventory
		err := query.NotDeleted(tx).
			Where("account_id = ? AND product_id = ? AND spec = ?", k.AccountID, k.ProductID, k.Spec).
			First(&inv).Error
		avail := int32(0)
		if err == nil {
			avail = int32(inv.Quantity)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if avail < need {
			// 流水上仍有余额但背包不足：部分已使用，无法整单回滚
			return ErrInventoryRollback
		}
		oid := orderID
		if err := s.adjustQuantity(tx, k.AccountID, k.ProductID, k.Spec, -need, &oid, nil,
			model.InventoryEventOrderRollback, strPtr("订单取消回滚")); err != nil {
			if errors.Is(err, ErrInventoryInsufficient) {
				return ErrInventoryRollback
			}
			return err
		}
	}
	return nil
}

func (s *InventoryService) Use(accountID, inventoryID uint64, input UseInventoryInput) (*InventoryUsageView, error) {
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}

	var inv model.UserInventory
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND account_id = ?", inventoryID, accountID).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryNotFound
		}
		return nil, err
	}
	if inv.Quantity < input.Quantity {
		return nil, ErrInventoryInsufficient
	}
	if inv.Product.ID == 0 {
		return nil, ErrProductNotFound
	}
	// 虚拟商品（如电影票）只能到店核销，不支持骑手配送
	if err := validateFulfillmentFlags(inv.Product, deliveryType); err != nil {
		return nil, err
	}

	var addrSnap *model.AddressSnapshot
	if deliveryType == model.DeliveryTypeDelivery {
		var addr model.UserAddress
		if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
			return nil, ErrAddressRequired
		}
		addrSnap = AddressSnapshotFromUserAddress(&addr)
	}

	zoneSvc := s.ZoneSvc
	if zoneSvc == nil {
		zoneSvc = &DeliveryZoneService{DB: s.DB}
	}
	if err := zoneSvc.ValidateDelivery(accountID, inv.Product.MerchantID, deliveryType, DeliveryCoordinateInput{
		AddressID: input.AddressID, AddressSnapshot: addrSnap,
	}); err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery {
		var merchant model.MerchantProfile
		if err := query.NotDeleted(s.DB).Select("delivery_fee").First(&merchant, inv.Product.MerchantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrMerchantNotFound
			}
			return nil, err
		}
		if merchant.DeliveryFee > 0 {
			return nil, ErrDeliveryFeePaymentRequired
		}
	}

	status := model.InventoryUsagePendingVerify
	if deliveryType == model.DeliveryTypeDelivery {
		status = model.InventoryUsagePendingShip
	}

	var usage model.UserInventoryUsage
	var verifyCode *string

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		usageIDHolder := uint64(0)
		primaryOID, err := s.deductInventoryUseFIFO(tx, accountID, inv.ProductID, inv.Spec, input.Quantity)
		if err != nil {
			return err
		}

		var snap model.PackageSelectionSnapshot
		pkgStatus := uint8(model.PackageSelectNone)
		if deliveryType == model.DeliveryTypeDelivery && inv.Product.ItemType == model.ProductItemTypePackage {
			var err error
			snap, pkgStatus, err = s.applyPackageSelectionsForDelivery(tx, inv.Product, input.Quantity, input.PackageSelections)
			if err != nil {
				return err
			}
		}

		optSnap, optStatus, err := applyOptionSelectionsForUsage(
			tx, inv.Product, deliveryType, input.Quantity, input.PackageSelections, input.OptionSelections,
		)
		if err != nil {
			return err
		}

		srcOID := primaryOID
		if srcOID == nil {
			srcOID = inv.LastOrderID
		}

		var expireAt *time.Time
		if deliveryType == model.DeliveryTypePickup {
			var err error
			expireAt, err = resolveUsageExpireAt(tx, inv.ProductID, srcOID, time.Now())
			if err != nil {
				return err
			}
		}

		usage = model.UserInventoryUsage{
			AccountID: accountID, InventoryID: inv.ID, ProductID: inv.ProductID,
			MerchantID: inv.Product.MerchantID, SourceOrderID: srcOID,
			Quantity: input.Quantity, DeliveryType: deliveryType,
			AddressSnapshot: addrSnap, Status: status, Remark: input.Remark,
			PackageSelections: snap, PackageSelectStatus: pkgStatus,
			OptionSelections: optSnap, OptionSelectStatus: optStatus,
			ExpireAt: expireAt,
		}
		if err := tx.Create(&usage).Error; err != nil {
			return err
		}
		usageIDHolder = usage.ID

		// 回填 usage_id 到本事务刚写入的 use 流水
		var useLogIDs []uint64
		if err := tx.Model(&model.UserInventoryLog{}).
			Where("account_id = ? AND product_id = ? AND spec = ? AND event_type = ? AND usage_id IS NULL", accountID, inv.ProductID, inv.Spec, model.InventoryEventUse).
			Order("id DESC").Limit(int(input.Quantity)+4).
			Pluck("id", &useLogIDs).Error; err != nil {
			return err
		}
		if len(useLogIDs) > 0 {
			if err := tx.Model(&model.UserInventoryLog{}).Where("id IN ?", useLogIDs).
				Update("usage_id", usageIDHolder).Error; err != nil {
				return err
			}
		}

		if deliveryType == model.DeliveryTypePickup || deliveryType == model.DeliveryTypeDelivery {
			vc, err := createVerificationCodeForUsage(tx, accountID, usage.ID, expireAt)
			if err != nil {
				return err
			}
			verifyCode = &vc.Code
		}
		if deliveryType == model.DeliveryTypeDelivery {
			// 从商家配置读取配送费/骑手收益快照写入 delivery_order
			var merchant model.MerchantProfile
			var deliveryFee, riderEarnings float64
			if err := query.NotDeleted(tx).First(&merchant, usage.MerchantID).Error; err != nil {
				return ErrMerchantNotFound
			}
			deliveryFee = merchant.DeliveryFee
			riderEarnings = merchant.RiderEarnings
			now := time.Now()
			// 背包跑腿无出餐环节，不生成出餐号（出餐号仅外卖/订单配送备餐用）
			d := model.DeliveryOrder{
				InventoryUsageID: &usage.ID,
				Status:           model.DeliveryPendingAdminReview,
				MerchantPrepared: 1,
				PreparedAt:       &now,
				DeliveryFee:      deliveryFee,
				RiderEarnings:    riderEarnings,
			}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
			if err := tx.Model(&usage).Update("delivery_order_id", d.ID).Error; err != nil {
				return err
			}
			usage.DeliveryOrderID = &d.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	view := &InventoryUsageView{
		UserInventoryUsage: usage,
		StatusText:         model.InventoryUsageStatusText(usage.Status),
		VerifyCode:         verifyCode,
	}
	view.Product = &inv.Product
	enrichUsageViewPackage(view)
	enrichUsageViewOptions(s.DB, view)
	return view, nil
}

func (s *InventoryService) RequestCancelUsage(accountID, usageID uint64, reason *string) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", usageID, accountID).First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	if usage.Status == model.InventoryUsageCancelPending {
		return nil, ErrInventoryCancelPending
	}
	if usage.Status != model.InventoryUsagePendingVerify && usage.Status != model.InventoryUsagePendingShip {
		return nil, ErrInventoryUsageInvalid
	}

	// 自提待核销：禁止「取消回背包」，须走 RefundPendingVerifyUsage 直接退款
	if usage.Status == model.InventoryUsagePendingVerify {
		return nil, fmt.Errorf("%w: 待核销请申请退款，不可取消回背包", ErrInventoryUsageInvalid)
	}

	// 配送：未接单可直取消；骑手已接单需商家审核
	if usage.DeliveryOrderID == nil {
		return s.finalizeCancelUsage(accountID, &usage, reason)
	}
	var delivery model.DeliveryOrder
	if err := query.NotDeleted(s.DB).First(&delivery, *usage.DeliveryOrderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.finalizeCancelUsage(accountID, &usage, reason)
		}
		return nil, err
	}
	// 待平台审核 / 待骑手接单：均可直接取消并退配送费回包（勿进商家取消审批）
	if delivery.Status == model.DeliveryPendingAccept ||
		delivery.Status == model.DeliveryPendingAdminReview {
		return s.finalizeCancelUsage(accountID, &usage, reason)
	}
	if delivery.Status == model.DeliveryCancelled || delivery.Status == model.DeliveryConfirmed {
		return nil, ErrInventoryUsageInvalid
	}

	// 骑手已接单至用户确认收货前：提交取消申请，待商家审核
	updates := map[string]interface{}{
		"status":        model.InventoryUsageCancelPending,
		"cancel_reason": reason,
	}
	if err := s.DB.Model(&usage).Updates(updates).Error; err != nil {
		return nil, err
	}
	usage.Status = model.InventoryUsageCancelPending
	usage.CancelReason = reason
	view, err := s.GetUsageView(accountID, usageID)
	if err != nil {
		return nil, err
	}
	return view, nil
}

// CancelUsage 兼容旧调用。
func (s *InventoryService) CancelUsage(accountID, usageID uint64) (*InventoryUsageView, error) {
	return s.RequestCancelUsage(accountID, usageID, nil)
}

func (s *InventoryService) MerchantReviewCancelUsage(merchantID, usageID uint64, approve bool, rejectReason *string) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).Where("id = ? AND merchant_id = ?", usageID, merchantID).First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	if usage.Status != model.InventoryUsageCancelPending {
		return nil, ErrInventoryUsageInvalid
	}
	if approve {
		return s.finalizeCancelUsage(usage.AccountID, &usage, usage.CancelReason)
	}
	updates := map[string]interface{}{
		"status":        model.InventoryUsagePendingShip,
		"cancel_reason": rejectReason,
	}
	if err := s.DB.Model(&usage).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetUsageView(0, usageID)
}

func (s *InventoryService) finalizeCancelUsage(accountID uint64, usage *model.UserInventoryUsage, reason *string) (*InventoryUsageView, error) {
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		var inv model.UserInventory
		if err := query.NotDeleted(tx).First(&inv, usage.InventoryID).Error; err != nil {
			return err
		}
		if err := s.restoreInventoryUseCancel(tx, usage, inv.Spec, strPtr("取消使用回滚")); err != nil {
			return err
		}

		if usage.Status == model.InventoryUsagePendingVerify || usage.DeliveryType == model.DeliveryTypePickup {
			now := time.Now()
			if err := tx.Model(&model.VerificationCode{}).
				Where("inventory_usage_id = ? AND status = ?", usage.ID, model.VerificationCodeUnused).
				Updates(map[string]interface{}{"status": model.VerificationCodeExpired, "used_at": now}).Error; err != nil {
				return err
			}
		}

		if usage.DeliveryOrderID != nil {
			if err := restorePackageComponentStock(tx, usage); err != nil {
				return err
			}
			// 若同单仍有其他未取消的 usage，则不取消整笔配送单，并纠正主 usage 指针
			var next model.UserInventoryUsage
			errNext := query.NotDeleted(tx).
				Where("delivery_order_id = ? AND id <> ? AND status NOT IN ?",
					*usage.DeliveryOrderID, usage.ID,
					[]int{int(model.InventoryUsageCancelled)}).
				Order("id ASC").First(&next).Error
			if errNext == nil {
				var d model.DeliveryOrder
				if err := query.NotDeleted(tx).First(&d, *usage.DeliveryOrderID).Error; err != nil {
					return err
				}
				if d.InventoryUsageID != nil && *d.InventoryUsageID == usage.ID {
					if err := tx.Model(&d).Update("inventory_usage_id", next.ID).Error; err != nil {
						return err
					}
				}
			} else if errors.Is(errNext, gorm.ErrRecordNotFound) {
				deliveryUpdates := map[string]interface{}{
					"status": model.DeliveryCancelled,
				}
				var d model.DeliveryOrder
				if err := query.NotDeleted(tx).Select("id", "status", "exception_reason").
					First(&d, *usage.DeliveryOrderID).Error; err == nil &&
					d.Status == model.DeliveryPendingAdminReview &&
					(d.ExceptionReason == nil || *d.ExceptionReason == "") {
					deliveryUpdates["exception_reason"] = "用户取消"
				}
				if err := tx.Model(&model.DeliveryOrder{}).
					Where("id = ? AND status NOT IN ?", *usage.DeliveryOrderID, []int{int(model.DeliveryConfirmed), int(model.DeliveryCancelled)}).
					Updates(deliveryUpdates).Error; err != nil {
					return err
				}
				if s.DeliveryFeePaySvc != nil {
					if err := s.DeliveryFeePaySvc.RefundForDeliveryOrderInTx(tx, *usage.DeliveryOrderID, "取消配送退配送费"); err != nil {
						return err
					}
				}
			} else {
				return errNext
			}
		}

		updates := map[string]interface{}{"status": model.InventoryUsageCancelled}
		if reason != nil {
			updates["cancel_reason"] = *reason
		}
		if err := tx.Model(usage).Updates(updates).Error; err != nil {
			return err
		}
		// 部分核销过：保留已核销记录
		if usage.VerifiedQuantity > 0 {
			return nil
		}
		return InvalidateVerificationRecordsForUsage(tx, usage.ID)
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.GetUsageView(accountID, usage.ID)
}

func (s *InventoryService) GetUsageView(accountID, usageID uint64) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	q := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Preload("DeliveryOrder", "is_deleted = ?", model.NotDeleted).
		Where("id = ?", usageID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	view := &InventoryUsageView{
		UserInventoryUsage: usage,
		StatusText:         model.InventoryUsageStatusText(usage.Status),
	}
	if usage.Status == model.InventoryUsagePendingVerify {
		var vc model.VerificationCode
		if err := query.NotDeleted(s.DB).
			Where("inventory_usage_id = ? AND status = ?", usage.ID, model.VerificationCodeUnused).
			First(&vc).Error; err == nil {
			view.VerifyCode = &vc.Code
		}
	}
	s.enrichUsageBuyer(view)
	enrichUsageViewPackage(view)
	enrichUsageViewOptions(s.DB, view)
	return view, nil
}

func (s *InventoryService) GetUsageViewForMerchant(merchantID, usageID uint64) (*InventoryUsageView, error) {
	view, err := s.GetUsageView(0, usageID)
	if err != nil {
		return nil, err
	}
	if view.MerchantID != merchantID {
		return nil, ErrInventoryUsageNotFound
	}
	return view, nil
}

func (s *InventoryService) enrichUsageBuyer(view *InventoryUsageView) {
	var acc model.Account
	if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").
		First(&acc, view.AccountID).Error; err != nil {
		return
	}
	view.Buyer = &BuyerBrief{AccountID: acc.ID, Nickname: acc.Nickname, Phone: acc.Phone}
}

func (s *InventoryService) ListUsages(accountID uint64, page, pageSize int) ([]InventoryUsageView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.UserInventoryUsage{})).Where("account_id = ?", accountID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.UserInventoryUsage
	if err := q.Preload("Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]InventoryUsageView, 0, len(list))
	for i := range list {
		v, err := s.GetUsageView(accountID, list[i].ID)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *v)
	}
	return views, total, nil
}

func (s *InventoryService) ListUsagesForMerchant(merchantID uint64, status *uint8, page, pageSize int, keyword string, startDate, endDate *time.Time) ([]InventoryUsageView, int64, error) {
	mid := merchantID
	return s.listUsagesFiltered(&mid, status, page, pageSize, keyword, startDate, endDate)
}

func (s *InventoryService) ListUsagesForAdmin(merchantID *uint64, status *uint8, page, pageSize int, keyword string, startDate, endDate *time.Time) ([]InventoryUsageView, int64, error) {
	return s.listUsagesFiltered(merchantID, status, page, pageSize, keyword, startDate, endDate)
}

func (s *InventoryService) listUsagesFiltered(merchantID *uint64, status *uint8, page, pageSize int, keyword string, startDate, endDate *time.Time) ([]InventoryUsageView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	accountIDs, empty, err := FindAccountIDsByKeyword(s.DB, keyword)
	if err != nil {
		return nil, 0, err
	}
	if empty {
		return []InventoryUsageView{}, 0, nil
	}

	q := query.NotDeleted(s.DB.Model(&model.UserInventoryUsage{}))
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if len(accountIDs) > 0 {
		q = q.Where("account_id IN ?", accountIDs)
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
	var list []model.UserInventoryUsage
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]InventoryUsageView, 0, len(list))
	for i := range list {
		v, err := s.GetUsageView(0, list[i].ID)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *v)
	}
	return views, total, nil
}

func (s *InventoryService) CompleteUsageByVerify(tx *gorm.DB, usageID uint64, packageUnits []PackageUnitInput, optionSelections []OptionSelectionUnitInput) error {
	var usage model.UserInventoryUsage
	if err := query.NotDeleted(tx).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		First(&usage, usageID).Error; err != nil {
		return err
	}
	updates := map[string]interface{}{
		"status":            model.InventoryUsageCompleted,
		"verified_quantity": usage.Quantity,
	}
	// 部分核销后再核销剩余：本次的套餐/规格选配只针对剩余件数
	verifyQty := usage.Quantity
	if usage.VerifiedQuantity > 0 && usage.VerifiedQuantity < usage.Quantity {
		verifyQty = usage.Quantity - usage.VerifiedQuantity
	}
	var resolvedPackageUnits [][]PackageSelectionInput
	if usage.Product != nil && usage.Product.ItemType == model.ProductItemTypePackage {
		if len(packageUnits) == 0 {
			if usage.PackageSelectStatus == model.PackageSelectUserSet || usage.PackageSelectStatus == model.PackageSelectDone {
				if usage.PackageSelectStatus == model.PackageSelectUserSet {
					updates["package_select_status"] = model.PackageSelectDone
				}
			} else {
				return ErrPackageSelectionRequired
			}
		} else {
			units, err := normalizePackageUnits(packageUnits, nil, verifyQty)
			if err != nil {
				return err
			}
			resolvedPackageUnits = units
			snap, err := applyPackageUnitsInTx(tx, usage.ProductID, verifyQty, units)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(snap)
			if err != nil {
				return err
			}
			updates["package_selections"] = json.RawMessage(raw)
			updates["package_select_status"] = model.PackageSelectDone
		}
	}

	if usage.OptionSelectStatus == model.OptionSelectDone && len(optionSelections) == 0 {
		// 跑腿下单时已选配，核销不再覆盖
	} else {
		optSnap, optStatus, err := applyOptionSelectionsForVerify(
			tx, usage.Product, usage.ProductID, verifyQty, resolvedPackageUnits, optionSelections,
		)
		if err != nil {
			return err
		}
		// Always write status so Pending→None clears when options were removed after usage creation.
		updates["option_select_status"] = optStatus
		if optStatus == model.OptionSelectNone {
			updates["option_selections"] = nil
		} else {
			raw, err := json.Marshal(optSnap)
			if err != nil {
				return err
			}
			updates["option_selections"] = json.RawMessage(raw)
		}
	}
	// 用纯 Model 更新，避免 Preload 的 Product 触发关联 upsert；
	// map Updates 不会走字段 serializer:json，须自行 JSON 编码。
	result := query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
		Where("id = ? AND status IN ?", usageID,
			[]int{int(model.InventoryUsagePendingVerify), int(model.InventoryUsagePendingShip)}).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	if usage.DeliveryOrderID == nil {
		return nil
	}
	var d model.DeliveryOrder
	if err := query.NotDeleted(tx).First(&d, *usage.DeliveryOrderID).Error; err != nil {
		return err
	}
	if !IsBagErrand(&d) || d.RiderID == nil {
		return nil
	}
	if d.Status != model.DeliveryAccepted && d.Status != model.DeliveryPicking {
		return nil
	}
	now := time.Now()
	return tx.Model(&d).Updates(map[string]interface{}{
		"status":     model.DeliveryDelivering,
		"started_at": now,
	}).Error
}

// deductInventoryUseFIFO 按来源订单 FIFO 扣减背包，并在 use 流水上写入 order_id，
// 避免后续 use_cancel / 退款错记到 LastOrderID。
func (s *InventoryService) deductInventoryUseFIFO(
	tx *gorm.DB, accountID, productID uint64, spec string, quantity uint32,
) (*uint64, error) {
	if quantity == 0 {
		return nil, nil
	}
	nets, seq, err := orderNetBalances(tx, accountID, productID, spec)
	if err != nil {
		return nil, err
	}
	need := int32(quantity)
	var primary *uint64
	for _, oid := range seq {
		if need <= 0 {
			break
		}
		remain := nets[oid]
		if remain <= 0 {
			continue
		}
		take := remain
		if take > need {
			take = need
		}
		oidCopy := oid
		if err := s.adjustQuantity(tx, accountID, productID, spec, -take, &oidCopy, nil, model.InventoryEventUse, nil); err != nil {
			return nil, err
		}
		if primary == nil {
			primary = &oidCopy
		}
		nets[oid] -= take
		need -= take
	}
	if need > 0 {
		// 历史错账导致来源合计不足：剩余仍扣库存，不绑定来源（兼容旧数据）
		if err := s.adjustQuantity(tx, accountID, productID, spec, -need, nil, nil, model.InventoryEventUse, nil); err != nil {
			return nil, err
		}
	}
	return primary, nil
}

// restoreInventoryUseCancel 按原 use 流水的 order_id 回滚；无流水时回退 SourceOrderID。
func (s *InventoryService) restoreInventoryUseCancel(
	tx *gorm.DB, usage *model.UserInventoryUsage, spec string, remark *string,
) error {
	var useLogs []model.UserInventoryLog
	if err := query.NotDeleted(tx).
		Where("usage_id = ? AND event_type = ? AND delta_qty < 0", usage.ID, model.InventoryEventUse).
		Order("id ASC").
		Find(&useLogs).Error; err != nil {
		return err
	}
	if len(useLogs) == 0 {
		return s.adjustQuantity(tx, usage.AccountID, usage.ProductID, spec,
			int32(usage.Quantity), usage.SourceOrderID, &usage.ID, model.InventoryEventUseCancel, remark)
	}
	var restored int32
	for i := range useLogs {
		lg := useLogs[i]
		qty := -lg.DeltaQty
		if qty <= 0 {
			continue
		}
		if err := s.adjustQuantity(tx, usage.AccountID, usage.ProductID, spec,
			qty, lg.OrderID, &usage.ID, model.InventoryEventUseCancel, remark); err != nil {
			return err
		}
		restored += qty
	}
	if remain := int32(usage.Quantity) - restored; remain > 0 {
		return s.adjustQuantity(tx, usage.AccountID, usage.ProductID, spec,
			remain, usage.SourceOrderID, &usage.ID, model.InventoryEventUseCancel, remark)
	}
	return nil
}

func (s *InventoryService) adjustQuantity(
	tx *gorm.DB, accountID, productID uint64, spec string, delta int32,
	orderID, usageID *uint64, eventType string, remark *string,
) error {
	var inv model.UserInventory
	err := query.NotDeleted(tx).
		Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
		First(&inv).Error

	before := uint32(0)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if delta < 0 {
			return ErrInventoryInsufficient
		}
		inv = model.UserInventory{
			AccountID: accountID, ProductID: productID, Spec: spec, Quantity: uint32(delta),
		}
		if orderID != nil {
			inv.LastOrderID = orderID
		}
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		before = inv.Quantity
		after := int64(before) + int64(delta)
		if after < 0 {
			return ErrInventoryInsufficient
		}
		updates := map[string]interface{}{"quantity": after}
		if orderID != nil && delta > 0 {
			updates["last_order_id"] = *orderID
		}
		if err := tx.Model(&inv).Updates(updates).Error; err != nil {
			return err
		}
		inv.Quantity = uint32(after)
	}

	log := model.UserInventoryLog{
		AccountID: accountID, InventoryID: &inv.ID, ProductID: productID, Spec: spec,
		OrderID: orderID, UsageID: usageID, EventType: eventType, DeltaQty: delta,
		BeforeQty: before, AfterQty: inv.Quantity, Remark: remark,
	}
	return tx.Create(&log).Error
}

func createVerificationCodeForUsage(tx *gorm.DB, accountID, usageID uint64, expireAt *time.Time) (*model.VerificationCode, error) {
	code := genVerifyCodeStr()
	vc := model.VerificationCode{
		AccountID: accountID, InventoryUsageID: &usageID, Code: code,
		Status: model.VerificationCodeUnused, ExpiredAt: expireAt,
	}
	if err := tx.Create(&vc).Error; err != nil {
		return nil, err
	}
	return &vc, nil
}

func orderItemSpec(it model.OrderItem) string {
	if it.Spec != nil {
		return *it.Spec
	}
	return ""
}

func genVerifyCodeStr() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("V%s", hex.EncodeToString(b))
}

func strPtr(s string) *string { return &s }
