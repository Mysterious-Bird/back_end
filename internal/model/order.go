package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// 主状态，见 docs/state-machines.md
const (
	OrderStatusPendingPay     uint8 = 0
	OrderStatusPendingGroup   uint8 = 1
	OrderStatusPendingFulfill uint8 = 2
	OrderStatusPendingShip    uint8 = 3
	OrderStatusShipping       uint8 = 4
	OrderStatusPendingVerify  uint8 = 5
	OrderStatusPendingConfirm uint8 = 6
	OrderStatusCompleted      uint8 = 7
	OrderStatusCancelled      uint8 = 8
	OrderStatusGroupFailed    uint8 = 9
	OrderStatusRefunding      uint8 = 10
	OrderStatusRefunded       uint8 = 11
	OrderStatusClosed         uint8 = 12
	OrderStatusPreparing      uint8 = 13 // 备餐中（商家库存确认通过后，待商家确认出餐）
)

const (
	PayStatusUnpaid          uint8 = 0
	PayStatusPaid            uint8 = 1
	PayStatusRefunding       uint8 = 2
	PayStatusRefunded        uint8 = 3
	PayStatusPartialRefunded uint8 = 4
)

// 商家两阶段审核（在 PENDING_FULFILL 等状态下使用）
const (
	MerchantReviewNone        uint8 = 0
	MerchantReviewPending     uint8 = 1 // 待订单审核 → 前端 pending_merchant
	MerchantReviewRejected    uint8 = 2 // 已拒绝
	MerchantReviewApproved    uint8 = 3 // 已通过，待用户申请使用 → approved
	MerchantReviewPendingUse  uint8 = 4 // 待库存确认 → pending_use_merchant
	MerchantReviewUseApproved uint8 = 5 // 库存已确认，进入履约
)

const (
	DeliveryTypePickup   uint8 = 1
	DeliveryTypeDelivery uint8 = 2
)

type AddressSnapshot struct {
	ContactName  string   `json:"contact_name"`
	ContactPhone string   `json:"contact_phone"`
	Province     string   `json:"province"`
	City         string   `json:"city"`
	District     string   `json:"district"`
	Detail       string   `json:"detail"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	LocationName *string  `json:"location_name,omitempty"`
}

func (a AddressSnapshot) Value() (driver.Value, error) {
	return json.Marshal(a)
}

func (a *AddressSnapshot) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, a)
}

type Order struct {
	ID                  uint64           `gorm:"primaryKey" json:"id"`
	ParentOrderID       *uint64          `gorm:"column:parent_order_id;index" json:"parent_order_id,omitempty"`
	OrderNo             string           `gorm:"size:32;not null" json:"order_no"`
	AccountID           uint64           `gorm:"not null" json:"account_id"`
	MerchantID          uint64           `gorm:"not null" json:"merchant_id"`
	UsageMerchantID     *uint64          `gorm:"column:usage_merchant_id" json:"usage_merchant_id,omitempty"`
	PackageProductID    *uint64          `gorm:"column:package_product_id;index" json:"package_product_id,omitempty"`
	ActivityID          *uint64          `gorm:"column:activity_id" json:"activity_id,omitempty"`
	Status              uint8            `gorm:"not null;default:0" json:"status"`
	MerchantReviewStage uint8            `gorm:"not null;default:0" json:"merchant_review_stage"`
	DeliveryType        uint8            `gorm:"not null" json:"delivery_type"`
	AddressSnapshot     *AddressSnapshot `gorm:"type:json" json:"address_snapshot,omitempty"`
	TotalAmount         float64          `gorm:"type:decimal(10,2);not null" json:"total_amount"`
	DiscountAmount      float64          `gorm:"type:decimal(10,2);not null;default:0" json:"discount_amount"`
	UserCouponID        *uint64          `gorm:"column:user_coupon_id" json:"user_coupon_id,omitempty"`
	PayAmount           float64          `gorm:"type:decimal(10,2);not null" json:"pay_amount"`
	RefundedAmount      float64          `gorm:"type:decimal(10,2);not null;default:0" json:"refunded_amount"`
	RefundPendingAmount float64          `gorm:"type:decimal(10,2);not null;default:0" json:"refund_pending_amount"`
	RefundFailed        uint8            `gorm:"not null;default:0" json:"refund_failed"`          // 微信退款发起失败待重试
	RefundFailReason    *string          `gorm:"size:191" json:"refund_fail_reason,omitempty"`
	RefundFailedAt      *time.Time       `json:"refund_failed_at,omitempty"`
	DeliveryFee         float64          `gorm:"type:decimal(10,2);not null;default:0" json:"delivery_fee"`
	RiderEarnings       float64          `gorm:"type:decimal(10,2);not null;default:0" json:"rider_earnings"`
	PayStatus           uint8            `gorm:"not null;default:0" json:"pay_status"`
	PaidAt              *time.Time       `json:"paid_at,omitempty"`
	PayExpireAt         *time.Time       `gorm:"column:pay_expire_at" json:"pay_expire_at,omitempty"`
	PrepayID            *string          `gorm:"column:prepay_id;size:64" json:"prepay_id,omitempty"`
	Remark              *string          `gorm:"size:256" json:"remark,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	SoftDelete
	Items            []OrderItem          `gorm:"foreignKey:OrderID" json:"items,omitempty"`
	Children         []Order              `gorm:"foreignKey:ParentOrderID" json:"children,omitempty"`
	MerchantProfile  *MerchantProfile     `gorm:"foreignKey:MerchantID" json:"merchant_profile,omitempty"`
}

func (Order) TableName() string { return "order" }

type OrderItem struct {
	ID             uint64    `gorm:"primaryKey" json:"id"`
	OrderID        uint64    `gorm:"not null" json:"order_id"`
	ProductID          uint64    `gorm:"not null" json:"product_id"`
	ActivityID         *uint64   `gorm:"column:activity_id" json:"activity_id,omitempty"`
	ActivityProductID  *uint64   `gorm:"column:activity_product_id" json:"activity_product_id,omitempty"`
	PlatformDailyBucket *string  `gorm:"column:platform_daily_bucket;size:32" json:"platform_daily_bucket,omitempty"`
	PurchaseType       uint8     `gorm:"not null;default:1" json:"purchase_type"`
	GroupBuyID     *uint64   `gorm:"column:group_buy_id" json:"group_buy_id,omitempty"`
	GroupBuyTeamID *uint64   `gorm:"column:group_buy_team_id" json:"group_buy_team_id,omitempty"`
	ProductName    string    `gorm:"size:128;not null" json:"product_name"`
	ProductImage   *string   `gorm:"size:512" json:"product_image,omitempty"`
	Spec           *string   `gorm:"size:128" json:"spec,omitempty"`
	UnitPrice      float64   `gorm:"type:decimal(10,2);not null" json:"unit_price"`
	Quantity       uint32    `gorm:"not null" json:"quantity"`
	Subtotal       float64   `gorm:"type:decimal(10,2);not null" json:"subtotal"`
	// RefundedQuantity 由库存退款流水汇总，非表字段
	RefundedQuantity uint32 `gorm:"-" json:"refunded_quantity"`
	CreatedAt      time.Time `json:"created_at"`
	SoftDelete
}

func (OrderItem) TableName() string { return "order_item" }

func OrderStatusText(status uint8) string {
	switch status {
	case OrderStatusPendingPay:
		return "待支付"
	case OrderStatusPendingGroup:
		return "待成团"
	case OrderStatusPendingFulfill:
		return "待履约"
	case OrderStatusPendingShip:
		return "待发货"
	case OrderStatusShipping:
		return "配送中"
	case OrderStatusPendingVerify:
		return "待核销"
	case OrderStatusPendingConfirm:
		return "待确认收货"
	case OrderStatusCompleted:
		return "已完成"
	case OrderStatusCancelled:
		return "已取消"
	case OrderStatusGroupFailed:
		return "拼团失败"
	case OrderStatusRefunding:
		return "退款中"
	case OrderStatusRefunded:
		return "已退款"
	case OrderStatusClosed:
		return "已关闭"
	case OrderStatusPreparing:
		return "备餐中"
	default:
		return "未知"
	}
}

// OrderStatusDisplayText 与 OrderStatusCode 对齐的用户可见文案。
func OrderStatusDisplayText(status, reviewStage uint8) string {
	switch OrderStatusCode(status, reviewStage) {
	case "pending_pay":
		return "待支付"
	case "pending_group":
		return "待成团"
	case "pending_merchant":
		return "待审核"
	case "rejected":
		return "订单已拒绝"
	case "approved":
		return "已入背包"
	case "pending_use_merchant":
		return "待库存确认"
	case "pending_fulfill":
		return "待履约"
	case "preparing":
		return "备餐中"
	case "ready_pickup":
		return "待自取"
	case "pending_rider":
		return "待骑手接单"
	case "delivering":
		return "配送中"
	case "pending_confirm":
		return "待确认收货"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	case "group_failed":
		return "拼团失败"
	case "closed":
		return "已关闭"
	case "refunding":
		return "退款中"
	case "refunded":
		return "已退款"
	case "partial_refunded":
		return "部分退款"
	default:
		return OrderStatusText(status)
	}
}

// OrderStatusCode 供小程序映射的复合状态码
func OrderStatusCode(status, reviewStage uint8) string {
	if status == OrderStatusPendingPay {
		return "pending_pay"
	}
	if status == OrderStatusPendingGroup {
		return "pending_group"
	}
	if status == OrderStatusPreparing {
		return "preparing"
	}
	if status == OrderStatusPendingFulfill {
		switch reviewStage {
		case MerchantReviewPending:
			return "pending_merchant"
		case MerchantReviewRejected:
			return "rejected"
		case MerchantReviewApproved:
			return "approved"
		case MerchantReviewPendingUse:
			return "pending_use_merchant"
		case MerchantReviewUseApproved:
			return "approved"
		case MerchantReviewNone:
			// 套餐父单等无商家审核阶段的待履约
			return "pending_fulfill"
		}
	}
	if status == OrderStatusPendingVerify {
		return "ready_pickup"
	}
	if status == OrderStatusPendingShip {
		return "pending_rider"
	}
	if status == OrderStatusShipping {
		return "delivering"
	}
	if status == OrderStatusPendingConfirm {
		return "pending_confirm"
	}
	if status == OrderStatusCompleted {
		return "completed"
	}
	if status == OrderStatusCancelled {
		return "cancelled"
	}
	if status == OrderStatusGroupFailed {
		return "group_failed"
	}
	if status == OrderStatusClosed {
		return "closed"
	}
	if status == OrderStatusRefunding {
		return "refunding"
	}
	if status == OrderStatusRefunded {
		return "refunded"
	}
	return "unknown"
}
