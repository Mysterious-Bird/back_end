package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrVerifyCodeNotFound      = errors.New("verification code not found")
	ErrVerifyCodeUsed          = errors.New("verification code already used")
	ErrVerifyCodeExpired       = errors.New("verification code expired")
	ErrVerifyMerchantMismatch  = errors.New("verification merchant mismatch")
	ErrVerifyRiderRequired     = errors.New("verification rider required")
	ErrVerifyQuantityInvalid   = errors.New("verify quantity invalid")
	ErrVerifyPartialPackage    = errors.New("package product requires full verification")
)

type VerificationService struct {
	DB           *gorm.DB
	InventorySvc *InventoryService
	ProductSvc   *ProductService
}

func (s *VerificationService) assertMerchantCanVerify(productID, merchantID uint64) error {
	if s.ProductSvc == nil {
		return ErrVerifyMerchantMismatch
	}
	if err := s.ProductSvc.AssertMerchantApplicable(productID, merchantID); err != nil {
		return ErrVerifyMerchantMismatch
	}
	return nil
}

// InvalidateVerificationRecordsForUsage 使用记录被回退/取消时作废关联核销记录，
// 避免「核销次数」仍计入已撤销的到店核销（如跑腿核销后管理员取消配送）。
func InvalidateVerificationRecordsForUsage(tx *gorm.DB, usageID uint64) error {
	if tx == nil || usageID == 0 {
		return nil
	}
	var codeIDs []uint64
	if err := query.NotDeleted(tx.Model(&model.VerificationCode{})).
		Where("inventory_usage_id = ?", usageID).
		Pluck("id", &codeIDs).Error; err != nil {
		return err
	}
	if len(codeIDs) == 0 {
		return nil
	}
	return query.NotDeleted(tx.Model(&model.VerificationRecord{})).
		Where("verification_code_id IN ?", codeIDs).
		Update("is_deleted", model.Deleted).Error
}

// VerifyPreviewView 扫码后展示的核销信息（数量默认全部，可部分核销）。
type VerifyPreviewView struct {
	Code        string  `json:"code"`
	ProductID   uint64  `json:"product_id"`
	ProductName string  `json:"product_name"`
	CoverURL    string  `json:"cover_url,omitempty"`
	Spec        string  `json:"spec,omitempty"`
	Quantity    uint32  `json:"quantity"`
	VerifiedQuantity  uint32 `json:"verified_quantity"`
	RemainingQuantity uint32 `json:"remaining_quantity"`
	ItemType    uint8   `json:"item_type"`
	IsPackage   bool    `json:"is_package"`
	UsageID     *uint64 `json:"usage_id,omitempty"`
	OrderID     *uint64 `json:"order_id,omitempty"`
	OrderNo     string  `json:"order_no,omitempty"`
}

type verifyResolveResult struct {
	vc         model.VerificationCode
	merchantID uint64
	product    model.Product
	spec       string
	quantity   uint32
	verifiedQuantity uint32
	usageID    *uint64
	orderID    *uint64
	orderNo    string
}

// LookupByCode 扫码查询核销信息，仅商品所属商家可查看。
func (s *VerificationService) LookupByCode(merchantID uint64, code string) (*VerifyPreviewView, error) {
	resolved, err := s.resolveVerifyCode(code, false)
	if err != nil {
		return nil, err
	}
	if err := s.assertMerchantCanVerify(resolved.product.ID, merchantID); err != nil {
		return nil, err
	}
	return toVerifyPreviewView(resolved), nil
}

// Verify 核销：quantity 为本次核销件数（0 表示核销全部剩余）。
// 支持部分核销：部分核销后使用记录保持待核销、核销码继续可用；全部核销后置为已完成。
// 套餐商品涉及选配与组件库存，须一次核销全部剩余数量。
func (s *VerificationService) Verify(merchantID, operatorID uint64, code string, quantity uint32, packageUnits []PackageUnitInput, optionSelections []OptionSelectionUnitInput) (*model.VerificationRecord, error) {
	preview, err := s.resolveVerifyCode(code, false)
	if err != nil {
		return nil, err
	}
	if err := s.assertMerchantCanVerify(preview.product.ID, merchantID); err != nil {
		return nil, err
	}

	var vc model.VerificationCode
	var record model.VerificationRecord

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		resolved, err := s.resolveVerifyCodeInTx(tx, code, true)
		if err != nil {
			return err
		}
		vc = resolved.vc

		now := time.Now()

		orderID := uint64(0)
		if resolved.usageID != nil {
			var usage model.UserInventoryUsage
			if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
				First(&usage, *resolved.usageID).Error; err != nil {
				return ErrVerifyCodeNotFound
			}
			if usage.ProductID != resolved.product.ID {
				return ErrVerifyMerchantMismatch
			}
			remaining := uint32(0)
			if usage.Quantity > usage.VerifiedQuantity {
				remaining = usage.Quantity - usage.VerifiedQuantity
			}
			if remaining == 0 {
				return ErrVerifyCodeUsed
			}
			qty := quantity
			if qty == 0 {
				qty = remaining
			}
			if qty > remaining {
				return fmt.Errorf("%w: 本次核销数量须在 1~%d 之间", ErrVerifyQuantityInvalid, remaining)
			}
			full := qty == remaining
			if resolved.product.ItemType == model.ProductItemTypePackage && !full {
				return ErrVerifyPartialPackage
			}
			if full {
				result := tx.Model(&vc).Where("status = ?", model.VerificationCodeUnused).
					Updates(map[string]interface{}{"status": model.VerificationCodeUsed, "used_at": now})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return ErrVerifyCodeUsed
				}
			}
			if usage.SourceOrderID != nil {
				orderID = *usage.SourceOrderID
			}
			record = model.VerificationRecord{
				VerificationCodeID: vc.ID, OrderID: orderID,
				MerchantID: merchantID, OperatorID: operatorID, Quantity: qty, VerifiedAt: now,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			if full {
				if s.InventorySvc != nil {
					if err := s.InventorySvc.CompleteUsageByVerify(tx, usage.ID, packageUnits, optionSelections); err != nil {
						return err
					}
				} else if err := tx.Model(&usage).Updates(map[string]interface{}{
					"status":            model.InventoryUsageCompleted,
					"usage_merchant_id": merchantID,
					"verified_quantity": usage.Quantity,
				}).Error; err != nil {
					return err
				}
			} else {
				updates := map[string]interface{}{
					"verified_quantity": usage.VerifiedQuantity + qty,
					"usage_merchant_id": merchantID,
				}
				// 规格选配按本次核销数量校验并记录（快照保留最近一次）
				optSnap, optStatus, optErr := applyOptionSelectionsForVerify(
					tx, &resolved.product, usage.ProductID, qty, nil, optionSelections,
				)
				if optErr != nil {
					return optErr
				}
				if optStatus != model.OptionSelectNone {
					raw, mErr := json.Marshal(optSnap)
					if mErr != nil {
						return mErr
					}
					updates["option_selections"] = json.RawMessage(raw)
					updates["option_select_status"] = optStatus
				}
				// 乐观校验已核销数，避免并发重复核销
				res := query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
					Where("id = ? AND verified_quantity = ?", usage.ID, usage.VerifiedQuantity).
					Updates(updates)
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected == 0 {
					return ErrVerifyCodeUsed
				}
			}
			// 核销写入订单履约时间线
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectOrder,
				SubjectID:   orderID,
				EventCode:   model.EventVerified,
				Title:       "已核销",
				Detail:      map[string]interface{}{"code": vc.Code, "quantity": qty},
			})
			return nil
		}

		if resolved.orderID == nil {
			return ErrVerifyCodeNotFound
		}
		var order model.Order
		if err := query.NotDeleted(tx).First(&order, *resolved.orderID).Error; err != nil {
			return ErrVerifyMerchantMismatch
		}
		// 订单直购核销码：一次性整单核销，不支持部分数量
		if quantity != 0 && resolved.quantity != 0 && quantity != resolved.quantity {
			return fmt.Errorf("%w: 该核销码不支持部分核销", ErrVerifyQuantityInvalid)
		}
		result := tx.Model(&vc).Where("status = ?", model.VerificationCodeUnused).
			Updates(map[string]interface{}{"status": model.VerificationCodeUsed, "used_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVerifyCodeUsed
		}
		record = model.VerificationRecord{
			VerificationCodeID: vc.ID, OrderID: order.ID,
			MerchantID: merchantID, OperatorID: operatorID, Quantity: resolved.quantity, VerifiedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&order).Update("status", model.OrderStatusCompleted).Error; err != nil {
			return err
		}
		// 核销写入订单履约时间线
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectOrder,
			SubjectID:   order.ID,
			EventCode:   model.EventVerified,
			Title:       "已核销",
			Detail:      map[string]interface{}{"code": vc.Code, "quantity": resolved.quantity},
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrVerifyMerchantMismatch) {
			return nil, ErrVerifyMerchantMismatch
		}
		if errors.Is(err, ErrVerifyCodeNotFound) || errors.Is(err, ErrVerifyCodeUsed) || errors.Is(err, ErrVerifyCodeExpired) || errors.Is(err, ErrVerifyRiderRequired) {
			return nil, err
		}
		if errors.Is(err, ErrVerifyQuantityInvalid) || errors.Is(err, ErrVerifyPartialPackage) {
			return nil, err
		}
		if errors.Is(err, ErrPackageSelectionRequired) || errors.Is(err, ErrOptionRequired) || errors.Is(err, ErrOptionInvalid) || errors.Is(err, ErrInsufficientStock) || errors.Is(err, ErrInvalidProductArg) {
			return nil, err
		}
		return nil, fmt.Errorf("核销失败: %w", err)
	}
	return &record, nil
}

func (s *VerificationService) resolveVerifyCode(code string, forUpdate bool) (*verifyResolveResult, error) {
	if code == "" {
		return nil, ErrVerifyCodeNotFound
	}
	q := query.NotDeleted(s.DB).Where("code = ?", code)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var vc model.VerificationCode
	if err := q.First(&vc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerifyCodeNotFound
		}
		return nil, err
	}
	return s.buildVerifyResolveResult(s.DB, &vc)
}

func (s *VerificationService) resolveVerifyCodeInTx(tx *gorm.DB, code string, forUpdate bool) (*verifyResolveResult, error) {
	if code == "" {
		return nil, ErrVerifyCodeNotFound
	}
	q := query.NotDeleted(tx).Where("code = ?", code)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var vc model.VerificationCode
	if err := q.First(&vc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerifyCodeNotFound
		}
		return nil, err
	}
	return s.buildVerifyResolveResult(tx, &vc)
}

func (s *VerificationService) buildVerifyResolveResult(db *gorm.DB, vc *model.VerificationCode) (*verifyResolveResult, error) {
	if vc.Status == model.VerificationCodeUsed {
		return nil, ErrVerifyCodeUsed
	}
	if vc.Status == model.VerificationCodeExpired {
		return nil, ErrVerifyCodeExpired
	}
	if vc.ExpiredAt != nil && vc.ExpiredAt.Before(time.Now()) {
		return nil, ErrVerifyCodeExpired
	}

	if vc.InventoryUsageID != nil {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(db).
			Preload("Product", "is_deleted = ?", model.NotDeleted).
			Preload("Inventory", "is_deleted = ?", model.NotDeleted).
			First(&usage, *vc.InventoryUsageID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrVerifyCodeNotFound
			}
			return nil, err
		}
		switch usage.Status {
		case model.InventoryUsagePendingVerify:
			if usage.ExpireAt != nil && usage.ExpireAt.Before(time.Now()) {
				return nil, ErrVerifyCodeExpired
			}
			// 自取待核销
		case model.InventoryUsagePendingShip:
			if usage.DeliveryOrderID == nil {
				return nil, ErrVerifyCodeUsed
			}
			var d model.DeliveryOrder
			if err := query.NotDeleted(db).First(&d, *usage.DeliveryOrderID).Error; err != nil {
				return nil, ErrVerifyCodeUsed
			}
			if !IsBagErrand(&d) || d.RiderID == nil {
				return nil, ErrVerifyRiderRequired
			}
			if d.Status != model.DeliveryAccepted && d.Status != model.DeliveryPicking && d.Status != model.DeliveryDelivering {
				return nil, ErrVerifyCodeUsed
			}
		default:
			return nil, ErrVerifyCodeUsed
		}
		var product model.Product
		if usage.Product != nil && usage.Product.ID != 0 {
			product = *usage.Product
		} else if err := query.NotDeleted(db).First(&product, usage.ProductID).Error; err != nil {
			return nil, ErrVerifyCodeNotFound
		}
		if product.MerchantID != usage.MerchantID {
			return nil, ErrVerifyMerchantMismatch
		}
		spec := ""
		if usage.Inventory != nil {
			spec = usage.Inventory.Spec
		}
		return &verifyResolveResult{
			vc: *vc, merchantID: product.MerchantID, product: product,
			spec: spec, quantity: usage.Quantity, verifiedQuantity: usage.VerifiedQuantity,
			usageID: vc.InventoryUsageID,
		}, nil
	}

	if vc.OrderID == nil {
		return nil, ErrVerifyCodeNotFound
	}
	var order model.Order
	if err := query.NotDeleted(db).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		First(&order, *vc.OrderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerifyCodeNotFound
		}
		return nil, err
	}
	if order.Status != model.OrderStatusPendingVerify {
		return nil, ErrVerifyCodeUsed
	}
	if len(order.Items) == 0 {
		return nil, ErrVerifyCodeNotFound
	}
	item := order.Items[0]
	var product model.Product
	if err := query.NotDeleted(db).First(&product, item.ProductID).Error; err != nil {
		return nil, ErrVerifyCodeNotFound
	}
	if product.MerchantID != order.MerchantID {
		return nil, ErrVerifyMerchantMismatch
	}
	spec := ""
	if item.Spec != nil {
		spec = *item.Spec
	}
	orderID := order.ID
	return &verifyResolveResult{
		vc: *vc, merchantID: product.MerchantID, product: product,
		spec: spec, quantity: item.Quantity, orderID: &orderID, orderNo: order.OrderNo,
	}, nil
}

func toVerifyPreviewView(resolved *verifyResolveResult) *VerifyPreviewView {
	remaining := uint32(0)
	if resolved.quantity > resolved.verifiedQuantity {
		remaining = resolved.quantity - resolved.verifiedQuantity
	}
	return &VerifyPreviewView{
		Code:        resolved.vc.Code,
		ProductID:   resolved.product.ID,
		ProductName: resolved.product.Name,
		CoverURL:    resolved.product.CoverURL,
		Spec:        resolved.spec,
		Quantity:    resolved.quantity,
		VerifiedQuantity:  resolved.verifiedQuantity,
		RemainingQuantity: remaining,
		ItemType:    resolved.product.ItemType,
		IsPackage:   resolved.product.ItemType == model.ProductItemTypePackage,
		UsageID:     resolved.usageID,
		OrderID:     resolved.orderID,
		OrderNo:     resolved.orderNo,
	}
}

// VerificationRecordView 核销记录展示：含核销码、商品、件数与成交金额。
type VerificationRecordView struct {
	ID           uint64    `json:"id"`
	Code         string    `json:"code"`
	VoucherCode  string    `json:"voucher_code"`
	OrderID      uint64    `json:"order_id"`
	OrderNo      string    `json:"order_no,omitempty"`
	MerchantID   uint64    `json:"merchant_id"`
	ProductID    uint64    `json:"product_id"`
	ProductName  string    `json:"product_name"`
	Quantity     uint32    `json:"quantity"`
	UseCount     uint32    `json:"use_count"`
	UnitPrice    float64   `json:"unit_price"`
	TotalAmount  float64   `json:"total_amount"`
	VerifyType   string    `json:"verify_type"`
	VerifiedAt   time.Time `json:"verified_at"`
	OperatorID   uint64    `json:"operator_id"`
	AccountID    uint64    `json:"account_id,omitempty"`
	UserNickname string    `json:"user_nickname,omitempty"`
	UserPhone    string    `json:"user_phone,omitempty"`
}

// VerificationListFilter 核销记录列表筛选。
type VerificationListFilter struct {
	MerchantID *uint64
	Keyword    string
	StartDate  *time.Time
	EndDate    *time.Time
	Page       int
	PageSize   int
}

func (s *VerificationService) effectiveVerificationBase() *gorm.DB {
	// 必须限定 verification_record.is_deleted：JOIN 多表后裸 is_deleted 会触发 MySQL Error 1052 ambiguous
	// 不再限定 usage.status=已完成：部分核销后 usage 仍为待核销，但核销记录需展示
	return s.DB.Session(&gorm.Session{NewDB: true}).
		Model(&model.VerificationRecord{}).
		Joins("JOIN verification_code vc ON vc.id = verification_record.verification_code_id AND vc.is_deleted = ?", model.NotDeleted).
		Joins("JOIN user_inventory_usage u ON u.id = vc.inventory_usage_id AND u.is_deleted = ?", model.NotDeleted).
		Where("verification_record.is_deleted = ?", model.NotDeleted)
}

func (s *VerificationService) ListByMerchant(merchantID uint64, page, pageSize int) ([]VerificationRecordView, int64, error) {
	return s.ListFiltered(VerificationListFilter{
		MerchantID: &merchantID,
		Page:       page,
		PageSize:   pageSize,
	})
}

func (s *VerificationService) ListAll(page, pageSize int) ([]VerificationRecordView, int64, error) {
	return s.ListFiltered(VerificationListFilter{Page: page, PageSize: pageSize})
}

func (s *VerificationService) ListFiltered(filter VerificationListFilter) ([]VerificationRecordView, int64, error) {
	page, pageSize := filter.Page, filter.PageSize
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

	accountIDs, empty, err := FindAccountIDsByKeyword(s.DB, filter.Keyword)
	if err != nil {
		return nil, 0, err
	}
	if empty {
		return []VerificationRecordView{}, 0, nil
	}

	base := s.effectiveVerificationBase()
	if filter.MerchantID != nil {
		base = base.Where("verification_record.merchant_id = ?", *filter.MerchantID)
	}
	if len(accountIDs) > 0 {
		base = base.Where("u.account_id IN ?", accountIDs)
	}
	if filter.StartDate != nil {
		base = base.Where("verification_record.verified_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		base = base.Where("verification_record.verified_at < ?", *filter.EndDate)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.VerificationRecord
	if err := base.Session(&gorm.Session{}).
		Order("verification_record.verified_at DESC, verification_record.id DESC").
		Offset(offset).Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return s.toVerificationRecordViews(list), total, nil
}

func (s *VerificationService) toVerificationRecordViews(list []model.VerificationRecord) []VerificationRecordView {
	out := make([]VerificationRecordView, 0, len(list))
	for i := range list {
		out = append(out, s.buildVerificationRecordView(&list[i]))
	}
	return out
}

func (s *VerificationService) buildVerificationRecordView(rec *model.VerificationRecord) VerificationRecordView {
	view := VerificationRecordView{
		ID:         rec.ID,
		OrderID:    rec.OrderID,
		MerchantID: rec.MerchantID,
		OperatorID: rec.OperatorID,
		VerifiedAt: rec.VerifiedAt,
		VerifyType: "merchant",
		Quantity:   rec.Quantity,
		UseCount:   rec.Quantity,
	}
	if view.Quantity == 0 {
		view.Quantity = 1
		view.UseCount = 1
	}
	var vc model.VerificationCode
	if err := query.NotDeleted(s.DB).First(&vc, rec.VerificationCodeID).Error; err == nil {
		view.Code = vc.Code
		view.VoucherCode = vc.Code
		if vc.InventoryUsageID != nil {
			var usage model.UserInventoryUsage
			if err := query.NotDeleted(s.DB).First(&usage, *vc.InventoryUsageID).Error; err == nil {
				view.ProductID = usage.ProductID
				view.AccountID = usage.AccountID
				var product model.Product
				if err := query.NotDeleted(s.DB).Select("id", "name", "price").First(&product, usage.ProductID).Error; err == nil {
					view.ProductName = product.Name
					view.UnitPrice = product.Price
				}
				orderID := rec.OrderID
				if usage.SourceOrderID != nil && *usage.SourceOrderID > 0 {
					orderID = *usage.SourceOrderID
				}
				if orderID > 0 {
					view.OrderID = orderID
					unit, amount, orderNo := s.lookupOrderItemMoney(orderID, usage.ProductID, view.Quantity)
					if orderNo != "" {
						view.OrderNo = orderNo
					}
					if unit > 0 {
						view.UnitPrice = unit
					}
					if amount > 0 {
						view.TotalAmount = amount
					}
				}
			}
		}
	}
	if view.AccountID == 0 && rec.OrderID > 0 {
		var order model.Order
		if err := query.NotDeleted(s.DB).Select("id", "account_id", "order_no").First(&order, rec.OrderID).Error; err == nil {
			view.AccountID = order.AccountID
			if view.OrderNo == "" {
				view.OrderNo = order.OrderNo
			}
		}
	}
	if view.AccountID > 0 {
		var acc model.Account
		if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").First(&acc, view.AccountID).Error; err == nil {
			if acc.Nickname != nil {
				view.UserNickname = *acc.Nickname
			}
			if acc.Phone != nil {
				view.UserPhone = *acc.Phone
			}
		}
	}
	if view.ProductName == "" && rec.OrderID > 0 {
		var order model.Order
		if err := query.NotDeleted(s.DB).
			Preload("Items", "is_deleted = ?", model.NotDeleted).
			First(&order, rec.OrderID).Error; err == nil {
			view.OrderNo = order.OrderNo
			if len(order.Items) > 0 {
				it := order.Items[0]
				view.ProductID = it.ProductID
				view.ProductName = it.ProductName
				view.Quantity = it.Quantity
				view.UseCount = it.Quantity
				if it.Quantity == 0 {
					view.Quantity = 1
					view.UseCount = 1
				}
				unit := it.UnitPrice
				if it.Quantity > 0 && it.Subtotal > 0 {
					unit = it.Subtotal / float64(it.Quantity)
				}
				view.UnitPrice = roundMoney(unit)
				view.TotalAmount = roundMoney(it.Subtotal)
				if view.TotalAmount <= 0 && order.PayAmount > 0 {
					view.TotalAmount = roundMoney(order.PayAmount)
				}
			}
		}
	}
	if view.TotalAmount <= 0 && view.UnitPrice > 0 {
		view.TotalAmount = roundMoney(view.UnitPrice * float64(view.Quantity))
	}
	view.TotalAmount = roundMoney(view.TotalAmount)
	view.UnitPrice = roundMoney(view.UnitPrice)
	if view.ProductName == "" {
		view.ProductName = "商品"
	}
	return view
}

func (s *VerificationService) lookupOrderItemMoney(orderID, productID uint64, qty uint32) (unitPrice, amount float64, orderNo string) {
	var order model.Order
	if err := query.NotDeleted(s.DB).Select("id", "order_no", "pay_amount").First(&order, orderID).Error; err == nil {
		orderNo = order.OrderNo
	}
	var items []model.OrderItem
	if err := query.NotDeleted(s.DB).Where("order_id = ?", orderID).Find(&items).Error; err != nil || len(items) == 0 {
		return 0, 0, orderNo
	}
	var matched *model.OrderItem
	for i := range items {
		if items[i].ProductID == productID {
			matched = &items[i]
			break
		}
	}
	if matched == nil {
		matched = &items[0]
	}
	unit := matched.UnitPrice
	if matched.Quantity > 0 && matched.Subtotal > 0 {
		unit = matched.Subtotal / float64(matched.Quantity)
	}
	unit = roundMoney(unit)
	if qty == 0 {
		qty = matched.Quantity
	}
	if qty == 0 {
		qty = 1
	}
	amt := roundMoney(unit * float64(qty))
	if amt <= 0 && matched.Subtotal > 0 {
		amt = roundMoney(matched.Subtotal)
	}
	return unit, amt, orderNo
}
