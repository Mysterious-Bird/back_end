package service

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// ErrCatalogInUse 商品/活动商品仍有未完成履约，禁止删除。
var ErrCatalogInUse = errors.New("catalog still in use")

type CatalogInUseError struct {
	Reason  string
	Message string
}

func (e *CatalogInUseError) Error() string {
	if e == nil || e.Message == "" {
		return ErrCatalogInUse.Error()
	}
	return ErrCatalogInUse.Error() + ": " + e.Message
}

func (e *CatalogInUseError) Unwrap() error { return ErrCatalogInUse }

func catalogInUse(reason, message string) error {
	return &CatalogInUseError{Reason: reason, Message: message}
}

func CatalogInUseMessage(err error) string {
	var e *CatalogInUseError
	if errors.As(err, &e) && e != nil && e.Message != "" {
		return e.Message
	}
	return "当前有未完成的订单或未使用商品，无法删除"
}

var catalogOrderDone = []int{
	int(model.OrderStatusCompleted),
	int(model.OrderStatusCancelled),
	int(model.OrderStatusGroupFailed),
	int(model.OrderStatusRefunded),
	int(model.OrderStatusClosed),
}

var catalogOrderVoid = []int{
	int(model.OrderStatusCancelled),
	int(model.OrderStatusGroupFailed),
	int(model.OrderStatusRefunded),
	int(model.OrderStatusClosed),
}

var catalogUsageOpen = []int{
	int(model.InventoryUsagePendingVerify),
	int(model.InventoryUsagePendingShip),
	int(model.InventoryUsageCancelPending),
}

var catalogDeliveryDone = []int{
	int(model.DeliveryConfirmed),
	int(model.DeliveryCancelled),
}

var catalogTakeoutDone = []int{
	int(model.TakeoutStatusCompleted),
	int(model.TakeoutStatusCancelled),
}

func existsCount(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Limit(1).Count(&n).Error
	return n, err
}

func assertProductDeletable(db *gorm.DB, productID uint64) error {
	if db == nil || productID == 0 {
		return nil
	}
	if n, err := existsCount(query.NotDeleted(db.Model(&model.UserInventory{})).
		Where("product_id = ? AND quantity > 0", productID)); err != nil {
		return err
	} else if n > 0 {
		var qty int64
		_ = query.NotDeleted(db.Model(&model.UserInventory{})).
			Where("product_id = ?", productID).
			Select("COALESCE(SUM(quantity), 0)").Scan(&qty)
		if qty < 1 {
			qty = 1
		}
		return catalogInUse("bag", fmt.Sprintf("有用户尚未使用（背包剩 %d 件），无法删除", qty))
	}

	if err := assertOpenUsages(db.Table("user_inventory_usage u").
		Where("u.is_deleted = ? AND u.product_id = ? AND u.status IN ?", model.NotDeleted, productID, catalogUsageOpen)); err != nil {
		return err
	}

	if n, err := existsCount(db.Table("delivery_order d").
		Joins("JOIN user_inventory_usage u ON u.id = d.inventory_usage_id AND u.is_deleted = ?", model.NotDeleted).
		Where("d.is_deleted = ? AND d.status NOT IN ? AND u.product_id = ?", model.NotDeleted, catalogDeliveryDone, productID)); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("delivery", "有配送尚未完成（用户未确认收货），无法删除")
	}

	if n, err := existsCount(db.Table("`order` AS o").
		Joins("JOIN order_item oi ON oi.order_id = o.id AND oi.is_deleted = ?", model.NotDeleted).
		Where("o.is_deleted = ? AND o.status NOT IN ? AND oi.product_id = ?", model.NotDeleted, catalogOrderDone, productID)); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("order", "有未完成的订单，无法删除")
	}

	if n, err := existsCount(db.Table("takeout_order t").
		Joins("JOIN takeout_order_item ti ON ti.takeout_order_id = t.id AND ti.is_deleted = ?", model.NotDeleted).
		Where("t.is_deleted = ? AND t.status NOT IN ? AND ti.product_id = ?", model.NotDeleted, catalogTakeoutDone, productID)); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("takeout", "有未完成的外卖订单，无法删除")
	}
	return nil
}

func assertActivityProductDeletable(db *gorm.DB, ap *model.ActivityProduct) error {
	if db == nil || ap == nil || ap.ID == 0 {
		return nil
	}
	return assertActivityScopedDeletable(db, ap.ProductID, &ap.ID, nil)
}

func assertActivityDeletable(db *gorm.DB, activityID uint64) error {
	if db == nil || activityID == 0 {
		return nil
	}
	return assertActivityScopedDeletable(db, 0, nil, &activityID)
}

func assertActivityScopedDeletable(db *gorm.DB, productID uint64, apID, activityID *uint64) error {
	itemMatch := func(alias string) string {
		if apID != nil {
			return alias + ".activity_product_id = ?"
		}
		return alias + ".activity_id = ?"
	}
	itemArg := uint64(0)
	if apID != nil {
		itemArg = *apID
	} else if activityID != nil {
		itemArg = *activityID
	}

	if err := assertNoActiveBargain(db, apID, activityID); err != nil {
		return err
	}

	bagQ := db.Table("user_inventory i").
		Joins("JOIN `order` o ON o.account_id = i.account_id AND o.is_deleted = ?", model.NotDeleted).
		Joins("JOIN order_item oi ON oi.order_id = o.id AND oi.is_deleted = ? AND oi.product_id = i.product_id AND "+itemMatch("oi"), model.NotDeleted, itemArg).
		Where("i.is_deleted = ? AND i.quantity > 0 AND o.status NOT IN ?", model.NotDeleted, catalogOrderVoid)
	if productID > 0 {
		bagQ = bagQ.Where("i.product_id = ?", productID)
	}
	if n, err := existsCount(bagQ); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("bag", "有用户尚未使用该活动商品，无法删除")
	}

	usageQ := db.Table("user_inventory_usage u").
		Joins("JOIN order_item oi ON oi.order_id = u.source_order_id AND oi.is_deleted = ? AND "+itemMatch("oi"), model.NotDeleted, itemArg).
		Where("u.is_deleted = ? AND u.status IN ?", model.NotDeleted, catalogUsageOpen)
	if err := assertOpenUsages(usageQ); err != nil {
		return err
	}

	if n, err := existsCount(db.Table("delivery_order d").
		Joins("JOIN user_inventory_usage u ON u.id = d.inventory_usage_id AND u.is_deleted = ?", model.NotDeleted).
		Joins("JOIN order_item oi ON oi.order_id = u.source_order_id AND oi.is_deleted = ? AND "+itemMatch("oi"), model.NotDeleted, itemArg).
		Where("d.is_deleted = ? AND d.status NOT IN ?", model.NotDeleted, catalogDeliveryDone)); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("delivery", "有配送尚未完成（用户未确认收货），无法删除")
	}

	if n, err := existsCount(db.Table("`order` AS o").
		Joins("JOIN order_item oi ON oi.order_id = o.id AND oi.is_deleted = ? AND "+itemMatch("oi"), model.NotDeleted, itemArg).
		Where("o.is_deleted = ? AND o.status NOT IN ?", model.NotDeleted, catalogOrderDone)); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("order", "有未完成的订单，无法删除")
	}
	return nil
}

// assertNoActiveBargain 删除活动/活动商品前：存在未过期的进行中砍价则禁止。
func assertNoActiveBargain(db *gorm.DB, apID, activityID *uint64) error {
	if db == nil {
		return nil
	}
	q := query.NotDeleted(db.Model(&model.BargainSession{})).
		Where("status = ? AND expire_at > ?", model.BargainStatusOngoing, time.Now())
	switch {
	case apID != nil && *apID > 0:
		q = q.Where("activity_product_id = ?", *apID)
	case activityID != nil && *activityID > 0:
		q = q.Where("activity_id = ?", *activityID)
	default:
		return nil
	}
	if n, err := existsCount(q); err != nil {
		return err
	} else if n > 0 {
		return catalogInUse("bargain", "有用户正在砍价中，无法删除")
	}
	return nil
}

func assertOpenUsages(q *gorm.DB) error {
	var row struct{ Status uint8 }
	err := q.Select("u.status AS status").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	switch row.Status {
	case model.InventoryUsagePendingVerify:
		return catalogInUse("usage", "有用户待到店核销，无法删除")
	case model.InventoryUsageCancelPending:
		return catalogInUse("usage", "有使用取消审核未完成，无法删除")
	case model.InventoryUsagePendingShip:
		return catalogInUse("delivery", "有配送尚未完成（用户未确认收货），无法删除")
	default:
		return catalogInUse("usage", "有用户尚未完成使用，无法删除")
	}
}
