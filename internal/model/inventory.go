package model

import (
	"fmt"
	"time"
)

const (
	InventoryEventOrderCredit   = "order_credit"
	InventoryEventOrderRollback = "order_rollback"
	InventoryEventUse           = "use"
	InventoryEventUseCancel     = "use_cancel"
	InventoryEventRefund        = "inventory_refund"
)

const (
	InventoryUsagePendingVerify  uint8 = 1
	InventoryUsagePendingShip    uint8 = 2
	InventoryUsageCompleted      uint8 = 3
	InventoryUsageCancelled      uint8 = 4
	InventoryUsageCancelPending  uint8 = 5
	InventoryUsageRefundReview   uint8 = 6 // 拼团过期：待管理员退款审核
)

// 套餐选配状态（仅 item_type=套餐 的使用记录有意义）
const (
	PackageSelectNone    uint8 = 0 // 非套餐 / 不适用
	PackageSelectPending uint8 = 1 // 自取核销后待商家确认选配
	PackageSelectDone    uint8 = 2 // 商家已确认选配
	PackageSelectUserSet uint8 = 3 // 外卖下单时用户已选配
)

type UserInventoryLog struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	AccountID   uint64    `gorm:"not null" json:"account_id"`
	InventoryID *uint64   `gorm:"column:inventory_id" json:"inventory_id,omitempty"`
	ProductID   uint64    `gorm:"not null" json:"product_id"`
	Spec        string    `gorm:"size:128;not null;default:''" json:"spec"`
	OrderID     *uint64   `gorm:"column:order_id" json:"order_id,omitempty"`
	UsageID     *uint64   `gorm:"column:usage_id" json:"usage_id,omitempty"`
	EventType   string    `gorm:"size:32;not null" json:"event_type"`
	DeltaQty    int32     `gorm:"not null" json:"delta_qty"`
	BeforeQty   uint32    `gorm:"not null;default:0" json:"before_qty"`
	AfterQty    uint32    `gorm:"not null;default:0" json:"after_qty"`
	Remark      *string   `gorm:"size:256" json:"remark,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	SoftDelete
}

func (UserInventoryLog) TableName() string { return "user_inventory_log" }

type UserInventoryUsage struct {
	ID              uint64           `gorm:"primaryKey" json:"id"`
	AccountID       uint64           `gorm:"not null" json:"account_id"`
	InventoryID     uint64           `gorm:"not null" json:"inventory_id"`
	ProductID       uint64           `gorm:"not null" json:"product_id"`
	MerchantID      uint64           `gorm:"not null" json:"merchant_id"`
	UsageMerchantID uint64           `gorm:"column:usage_merchant_id;not null;default:0" json:"usage_merchant_id"`
	SourceOrderID   *uint64          `gorm:"column:source_order_id" json:"source_order_id,omitempty"`
	Quantity        uint32           `gorm:"not null" json:"quantity"`
	DeliveryType    uint8            `gorm:"not null" json:"delivery_type"`
	AddressSnapshot *AddressSnapshot `gorm:"type:json" json:"address_snapshot,omitempty"`
	Status          uint8            `gorm:"not null;default:1" json:"status"`
	DeliveryOrderID *uint64          `gorm:"column:delivery_order_id" json:"delivery_order_id,omitempty"`
	CancelReason         *string          `gorm:"size:256" json:"cancel_reason,omitempty"`
	Remark               *string          `gorm:"size:256" json:"remark,omitempty"`
	PackageSelections   PackageSelectionSnapshot `gorm:"type:json;serializer:json" json:"package_selections,omitempty"`
	PackageSelectStatus uint8                    `gorm:"not null;default:0" json:"package_select_status"`
	OptionSelections    OptionSelectionSnapshot  `gorm:"type:json;serializer:json" json:"option_selections,omitempty"`
	OptionSelectStatus  uint8                    `gorm:"not null;default:0" json:"option_select_status"`
	ExpireAt            *time.Time               `gorm:"column:expire_at" json:"expire_at,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
	SoftDelete
	Product         *Product           `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Inventory       *UserInventory     `gorm:"foreignKey:InventoryID" json:"inventory,omitempty"`
	DeliveryOrder   *DeliveryOrder     `gorm:"foreignKey:DeliveryOrderID" json:"delivery_order,omitempty"`
	MerchantProfile *MerchantProfile   `gorm:"foreignKey:MerchantID" json:"merchant_profile,omitempty"`
}

// PackageSelectionSnapshot 使用记录上的套餐选配快照。
type PackageSelectionSnapshot []PackageSelectionGroupSnap

type PackageSelectionGroupSnap struct {
	GroupID   uint64                       `json:"group_id"`
	GroupName string                       `json:"group_name,omitempty"`
	Items     []PackageSelectionItemSnap   `json:"items"`
}

type PackageSelectionItemSnap struct {
	ProductID   uint64 `json:"product_id"`
	ProductName string `json:"product_name,omitempty"`
	Qty         uint32 `json:"qty"`
}

func (UserInventoryUsage) TableName() string { return "user_inventory_usage" }

func InventoryUsageStatusText(status uint8) string {
	switch status {
	case InventoryUsagePendingVerify:
		return "待核销"
	case InventoryUsagePendingShip:
		return "待发货"
	case InventoryUsageCompleted:
		return "已完成"
	case InventoryUsageCancelled:
		return "已取消"
	case InventoryUsageCancelPending:
		return "取消待审核"
	case InventoryUsageRefundReview:
		return "退款待审核"
	default:
		return "未知"
	}
}

func PackageSelectStatusText(status uint8) string {
	switch status {
	case PackageSelectPending:
		return "待套餐选配"
	case PackageSelectDone:
		return "套餐已选配"
	case PackageSelectUserSet:
		return "已选配"
	default:
		return ""
	}
}

// SummaryText 生成「可乐×1、汉堡×1」类摘要。
func (s PackageSelectionSnapshot) SummaryText() string {
	if len(s) == 0 {
		return ""
	}
	parts := make([]string, 0, 8)
	for _, g := range s {
		for _, it := range g.Items {
			if it.Qty == 0 {
				continue
			}
			name := it.ProductName
			if name == "" {
				name = fmt.Sprintf("#%d", it.ProductID)
			}
			parts = append(parts, fmt.Sprintf("%s×%d", name, it.Qty))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "、" + parts[i]
	}
	return out
}
