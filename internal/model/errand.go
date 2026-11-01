package model

import (
	"time"
)

// 跑腿单状态
const (
	ErrandStatusPendingAccept  uint8 = 1 // 待接单（跑腿费已付）
	ErrandStatusAccepted       uint8 = 2 // 已接单（待取货/代购）
	ErrandStatusDelivering     uint8 = 3 // 配送中（已取货）
	ErrandStatusPendingConfirm uint8 = 4 // 待用户确认（已送达并完成收款）
	ErrandStatusCompleted      uint8 = 5 // 已完成
	ErrandStatusCancelled      uint8 = 6 // 已取消
	ErrandStatusException      uint8 = 7 // 异常处理中
	ErrandStatusPendingPay     uint8 = 8 // 待支付跑腿费（下单即进入，支付成功后才可被接单）
)

// 跑腿收款方式（送达时确定）
const (
	ErrandPayModePlatform uint8 = 1 // 平台支付（微信）
	ErrandPayModeCash     uint8 = 2 // 线下现金
)

// PaySubjectErrand 跑腿单支付主体（下单付跑腿费、送达付代购商品费）。
const PaySubjectErrand = "errand"

// ErrandPickupSnapshot 取货点快照（店外商家，不在平台内）。
type ErrandPickupSnapshot struct {
	ShopName     string   `json:"shop_name,omitempty"`
	ContactName  string   `json:"contact_name,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
	Address      string   `json:"address"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
}

// ErrandOrder 跑腿单（非平台商品代取代送）。
type ErrandOrder struct {
	ID                 uint64                `gorm:"primaryKey" json:"id"`
	OrderNo            string                `gorm:"size:32;not null;uniqueIndex" json:"order_no"`
	AccountID          uint64                `gorm:"not null;index" json:"account_id"`
	Status             uint8                 `gorm:"not null;default:1;index" json:"status"`
	PayStatus          uint8                 `gorm:"not null;default:0" json:"pay_status"`
	GoodsPayStatus     uint8                 `gorm:"column:goods_pay_status;not null;default:0" json:"goods_pay_status"`
	PayMode            uint8                 `gorm:"not null;default:0" json:"pay_mode"`
	DeliveryFee        float64               `gorm:"type:decimal(10,2);not null;default:1" json:"delivery_fee"`
	ItemCount          int                   `gorm:"not null;default:1" json:"item_count"`
	GoodsAmount        float64               `gorm:"type:decimal(10,2);not null;default:0" json:"goods_amount"`
	PayAmount          float64               `gorm:"type:decimal(10,2);not null;default:0" json:"pay_amount"`
	RefundedAmount     float64               `gorm:"type:decimal(10,2);not null;default:0" json:"refunded_amount"`
	CollectedAmount    float64               `gorm:"type:decimal(10,2);not null;default:0" json:"collected_amount"`
	RiderEarnings      float64               `gorm:"type:decimal(10,2);not null;default:0" json:"rider_earnings"`
	PlatformCommission float64               `gorm:"type:decimal(10,2);not null;default:0" json:"platform_commission"`
	GoodsDesc          string                `gorm:"size:512;not null;default:''" json:"goods_desc"`
	GoodsPhotos        []string              `gorm:"serializer:json" json:"goods_photos,omitempty"`
	VoucherCode        string                `gorm:"size:128;not null;default:''" json:"voucher_code,omitempty"`
	VoucherPhotos      []string              `gorm:"serializer:json" json:"voucher_photos,omitempty"`
	GoodsReceiptPhotos []string              `gorm:"serializer:json" json:"goods_receipt_photos,omitempty"`
	PickupSnapshot     *ErrandPickupSnapshot `gorm:"type:json;serializer:json" json:"pickup_snapshot,omitempty"`
	DropSnapshot       *AddressSnapshot      `gorm:"type:json" json:"drop_snapshot,omitempty"`
	DistanceKm         float64               `gorm:"type:decimal(10,2);not null;default:0" json:"distance_km"`
	ExpectMinutes      int                   `gorm:"not null;default:0" json:"expect_minutes"`
	DeliveryOrderID    *uint64               `json:"delivery_order_id,omitempty"`
	PayExpireAt        *time.Time            `gorm:"column:pay_expire_at" json:"pay_expire_at,omitempty"`
	CancelReason       *string               `gorm:"size:256" json:"cancel_reason,omitempty"`
	ExceptionReason    *string               `gorm:"size:512" json:"exception_reason,omitempty"`
	Remark             *string               `gorm:"size:512" json:"remark,omitempty"`
	PaidAt             *time.Time            `json:"paid_at,omitempty"`
	FinishedAt         *time.Time            `json:"finished_at,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	SoftDelete
}

func (ErrandOrder) TableName() string { return "errand_order" }

func ErrandStatusText(status uint8) string {
	switch status {
	case ErrandStatusPendingAccept:
		return "待接单"
	case ErrandStatusAccepted:
		return "已接单"
	case ErrandStatusDelivering:
		return "配送中"
	case ErrandStatusPendingConfirm:
		return "待确认送达"
	case ErrandStatusCompleted:
		return "已完成"
	case ErrandStatusCancelled:
		return "已取消"
	case ErrandStatusException:
		return "异常处理中"
	case ErrandStatusPendingPay:
		return "待支付"
	default:
		return "未知"
	}
}

// ErrandSettings 跑腿全局配置（单例 id=1）。
type ErrandSettings struct {
	ID                        uint64    `gorm:"primaryKey" json:"id"`
	DeliveryFee               float64   `gorm:"type:decimal(10,2);not null;default:1" json:"delivery_fee"`
	PlatformCommissionPercent float64   `gorm:"type:decimal(5,2);not null;default:0" json:"platform_commission_percent"`
	AutoConfirmHours          int       `gorm:"not null;default:24" json:"auto_confirm_hours"`
	GoodsMaxAmount            float64   `gorm:"type:decimal(10,2);not null;default:5000" json:"goods_max_amount"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

func (ErrandSettings) TableName() string { return "errand_settings" }
