package model

import "time"

const (
	DeliveryPendingAccept uint8 = 0
	DeliveryAccepted      uint8 = 1
	DeliveryPicking       uint8 = 2
	DeliveryDelivering    uint8 = 3
	DeliveryDelivered     uint8 = 4
	DeliveryConfirmed     uint8 = 5
	DeliveryCancelled          uint8 = 6
	DeliveryException          uint8 = 7
	DeliveryPendingAdminReview uint8 = 8
)

type DeliveryOrder struct {
	ID               uint64     `gorm:"primaryKey" json:"id"`
	OrderID          *uint64    `gorm:"column:order_id" json:"order_id,omitempty"`
	InventoryUsageID *uint64    `gorm:"column:inventory_usage_id" json:"inventory_usage_id,omitempty"`
	TakeoutOrderID   *uint64    `gorm:"column:takeout_order_id" json:"takeout_order_id,omitempty"`
	ErrandOrderID    *uint64    `gorm:"column:errand_order_id" json:"errand_order_id,omitempty"`
	RiderID          *uint64    `json:"rider_id,omitempty"`
	Status           uint8      `gorm:"not null;default:0" json:"status"`
	UserConfirmed    uint8      `gorm:"not null;default:0" json:"user_confirmed"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	DeliverRemark    *string    `gorm:"size:512" json:"deliver_remark,omitempty"`
	DeliverPhotos    []string   `gorm:"serializer:json" json:"deliver_photos,omitempty"`
	DeliveryFee      float64    `gorm:"type:decimal(10,2);not null;default:0" json:"delivery_fee"`
	RiderEarnings    float64    `gorm:"type:decimal(10,2);not null;default:0" json:"rider_earnings"`
	PickupCode       string     `gorm:"size:8" json:"pickup_code,omitempty"`
	MerchantPrepared uint8      `gorm:"not null;default:0" json:"merchant_prepared"`
	PreparedAt       *time.Time `json:"prepared_at,omitempty"`
	ExceptionReason  *string    `gorm:"size:512" json:"exception_reason,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	SoftDelete
	Order          *Order              `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	InventoryUsage *UserInventoryUsage `gorm:"foreignKey:InventoryUsageID" json:"inventory_usage,omitempty"`
}

func (DeliveryOrder) TableName() string { return "delivery_order" }

func DeliveryStatusText(status uint8) string {
	switch status {
	case DeliveryPendingAccept:
		return "待接单"
	case DeliveryAccepted:
		return "已接单"
	case DeliveryPicking:
		return "取货中"
	case DeliveryDelivering:
		return "配送中"
	case DeliveryDelivered:
		return "待确认收货"
	case DeliveryConfirmed:
		return "已完成"
	case DeliveryCancelled:
		return "已取消"
	case DeliveryException:
		return "配送异常"
	case DeliveryPendingAdminReview:
		return "待平台审核"
	default:
		return "未知"
	}
}
