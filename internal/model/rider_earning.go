package model

import "time"

const (
	RiderEarningPending   uint8 = 0 // 待结账
	RiderEarningSettled   uint8 = 1 // 已结账
	RiderEarningCancelled uint8 = 2 // 已取消（异常单/退款不记收益）
)

type RiderEarning struct {
	ID              uint64     `gorm:"primaryKey" json:"id"`
	RiderID         uint64     `gorm:"not null;index:idx_rider_status,priority:1" json:"rider_id"`
	DeliveryOrderID uint64     `gorm:"not null;index:idx_delivery" json:"delivery_order_id"`
	OrderID         *uint64    `json:"order_id,omitempty"`
	ErrandOrderID   *uint64    `gorm:"column:errand_order_id" json:"errand_order_id,omitempty"`
	MerchantID      uint64     `gorm:"not null" json:"merchant_id"`
	Amount          float64    `gorm:"type:decimal(10,2);not null" json:"amount"`
	Status          uint8      `gorm:"not null;default:0;index:idx_rider_status,priority:2" json:"status"`
	SettlementID    *uint64    `gorm:"index:idx_settlement" json:"settlement_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	SettledAt       *time.Time `json:"settled_at,omitempty"`
	SoftDelete
}

func (RiderEarning) TableName() string { return "rider_earning" }
