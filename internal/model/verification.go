package model

import "time"

const (
	VerificationCodeUnused  uint8 = 0
	VerificationCodeUsed    uint8 = 1
	VerificationCodeExpired uint8 = 2
)

type VerificationCode struct {
	ID               uint64     `gorm:"primaryKey" json:"id"`
	OrderID          *uint64    `gorm:"column:order_id" json:"order_id,omitempty"`
	InventoryUsageID *uint64    `gorm:"column:inventory_usage_id" json:"inventory_usage_id,omitempty"`
	AccountID        uint64     `gorm:"not null" json:"account_id"`
	Code             string     `gorm:"size:32;not null" json:"code"`
	Status           uint8      `gorm:"not null;default:0" json:"status"`
	ExpiredAt        *time.Time `json:"expired_at,omitempty"`
	UsedAt           *time.Time `json:"used_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	SoftDelete
}

func (VerificationCode) TableName() string { return "verification_code" }

type VerificationRecord struct {
	ID                 uint64    `gorm:"primaryKey" json:"id"`
	VerificationCodeID uint64    `gorm:"not null" json:"verification_code_id"`
	OrderID            uint64    `gorm:"not null" json:"order_id"`
	MerchantID         uint64    `gorm:"not null" json:"merchant_id"`
	OperatorID         uint64    `gorm:"not null" json:"operator_id"`
	Quantity           uint32    `gorm:"not null;default:0" json:"quantity"` // 本次核销件数
	VerifiedAt         time.Time `json:"verified_at"`
	SoftDelete
}

func (VerificationRecord) TableName() string { return "verification_record" }
