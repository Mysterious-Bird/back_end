package service

import (
	"errors"
	"fmt"
	"log"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// normalizeExpireDays 0/nil → 永不过期（返回 nil）。
func normalizeExpireDays(days *uint32) *uint32 {
	if days == nil || *days == 0 {
		return nil
	}
	v := *days
	return &v
}

// resolveUsageExpireAt 按订单行购买方式 + 活动覆盖解析待核销过期时间。
// 活动 expire_days 有值则覆盖；否则团购用 product.deal_expire_days，拼团用 product.group_expire_days。
func resolveUsageExpireAt(tx *gorm.DB, productID uint64, orderID *uint64, now time.Time) (*time.Time, error) {
	if orderID == nil || *orderID == 0 {
		return resolveProductChannelExpireAt(tx, productID, model.PurchaseTypeSolo, now)
	}

	var item model.OrderItem
	err := query.NotDeleted(tx).
		Where("order_id = ? AND product_id = ?", *orderID, productID).
		Order("id ASC").
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return resolveProductChannelExpireAt(tx, productID, model.PurchaseTypeSolo, now)
		}
		return nil, err
	}

	purchaseType := item.PurchaseType
	if purchaseType == 0 {
		purchaseType = model.PurchaseTypeSolo
	}

	if item.ActivityProductID != nil && *item.ActivityProductID > 0 {
		var ap model.ActivityProduct
		if err := query.NotDeleted(tx).First(&ap, *item.ActivityProductID).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		} else if days := normalizeExpireDays(ap.ExpireDays); days != nil {
			exp := now.AddDate(0, 0, int(*days))
			return &exp, nil
		}
	}

	return resolveProductChannelExpireAt(tx, productID, purchaseType, now)
}

func resolveProductChannelExpireAt(tx *gorm.DB, productID uint64, purchaseType uint8, now time.Time) (*time.Time, error) {
	var product model.Product
	if err := query.NotDeleted(tx).Select("id", "deal_expire_days", "group_expire_days").
		First(&product, productID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var days *uint32
	if purchaseType == model.PurchaseTypeGroup {
		days = product.GroupExpireDays
	} else {
		days = product.DealExpireDays
	}
	days = normalizeExpireDays(days)
	if days == nil {
		return nil, nil
	}
	exp := now.AddDate(0, 0, int(*days))
	return &exp, nil
}

// usageIsGroupBuyPurchase 判断待核销 usage 是否来自拼团成团订单。
func usageIsGroupBuyPurchase(db *gorm.DB, usage *model.UserInventoryUsage) (bool, error) {
	orderID := uint64(0)
	if usage.SourceOrderID != nil {
		orderID = *usage.SourceOrderID
	}
	if orderID == 0 {
		var oid uint64
		if err := query.NotDeleted(db.Model(&model.UserInventoryLog{})).
			Select("order_id").
			Where("usage_id = ? AND event_type = ? AND order_id IS NOT NULL AND order_id > 0",
				usage.ID, model.InventoryEventUse).
			Order("id ASC").
			Limit(1).
			Scan(&oid).Error; err != nil {
			return false, err
		}
		orderID = oid
	}
	if orderID == 0 {
		return false, nil
	}
	var purchaseType uint8
	err := query.NotDeleted(db.Model(&model.OrderItem{})).
		Select("purchase_type").
		Where("order_id = ? AND product_id = ?", orderID, usage.ProductID).
		Order("id ASC").
		Limit(1).
		Scan(&purchaseType).Error
	if err != nil {
		return false, err
	}
	return isGroupBuyPurchaseType(purchaseType), nil
}

// markUsageExpireRefundReview 拼团过期：作废核销码并进入管理员退款审核（不自动退款）。
func (s *OrderService) markUsageExpireRefundReview(usage *model.UserInventoryUsage) error {
	reason := "商品已过期待退款审核"
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.UserInventoryUsage
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND status = ?", usage.ID, model.InventoryUsagePendingVerify).
			First(&locked).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&model.VerificationCode{}).
			Where("inventory_usage_id = ? AND status = ?", locked.ID, model.VerificationCodeUnused).
			Updates(map[string]interface{}{"status": model.VerificationCodeExpired, "used_at": now}).Error; err != nil {
			return err
		}
		if err := InvalidateVerificationRecordsForUsage(tx, locked.ID); err != nil {
			return err
		}
		return tx.Model(&locked).Updates(map[string]interface{}{
			"status":        model.InventoryUsageRefundReview,
			"cancel_reason": reason,
		}).Error
	})
}

// ExpireStalePendingVerifyUsages 扫描已过期的自取待核销：
// 拼团 → 退款待审核；其它渠道 → 自动退款。
func (s *OrderService) ExpireStalePendingVerifyUsages(now time.Time) (int, error) {
	var list []model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).
		Where("status = ? AND delivery_type = ? AND expire_at IS NOT NULL AND expire_at <= ?",
			model.InventoryUsagePendingVerify, model.DeliveryTypePickup, now).
		Order("id ASC").
		Limit(100).
		Find(&list).Error; err != nil {
		return 0, err
	}
	var firstErr error
	n := 0
	for i := range list {
		u := list[i]
		isGroup, err := usageIsGroupBuyPurchase(s.DB, &u)
		if err != nil {
			log.Printf("[usage-expire] detect group-buy usage %d failed: %v", u.ID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("expire usage %d: %w", u.ID, err)
			}
			continue
		}
		if isGroup {
			if err := s.markUsageExpireRefundReview(&u); err != nil {
				log.Printf("[usage-expire] mark review usage %d failed: %v", u.ID, err)
				if firstErr == nil {
					firstErr = fmt.Errorf("expire usage %d: %w", u.ID, err)
				}
				continue
			}
			n++
			continue
		}
		if _, err := s.RefundPendingVerifyUsageWithReason(u.AccountID, u.ID, "商品已过期自动退款"); err != nil {
			log.Printf("[usage-expire] refund usage %d failed: %v", u.ID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("expire usage %d: %w", u.ID, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}
