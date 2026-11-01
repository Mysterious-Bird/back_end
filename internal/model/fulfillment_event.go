package model

import (
	"encoding/json"
	"time"
)

const (
	FulfillmentSubjectOrder       = "order"
	FulfillmentSubjectTakeout     = "takeout"
	FulfillmentSubjectDelivery    = "delivery"
	FulfillmentSubjectUsage       = "usage"
	FulfillmentSubjectDeliveryFee = "delivery_fee"
	FulfillmentSubjectErrand      = "errand"
)

const (
	FulfillmentActorUser     = "user"
	FulfillmentActorMerchant = "merchant"
	FulfillmentActorRider    = "rider"
	FulfillmentActorAdmin    = "admin"
	FulfillmentActorSystem   = "system"
)

// 常用 event_code
const (
	EventCreated            = "created"
	EventPaid               = "paid"
	EventCancelled          = "cancelled"
	EventMerchantApproved   = "merchant_approved"
	EventMerchantRejected   = "merchant_rejected"
	EventPrepared           = "prepared"
	EventRiderAccepted      = "rider_accepted"
	EventPicking            = "picking"
	EventDelivering         = "delivering"
	EventDelivered          = "delivered"
	EventUserConfirmed      = "user_confirmed"
	EventCompleted          = "completed"
	EventExceptionReported  = "exception_reported"
	EventAdminResolved      = "admin_resolved"
	EventRefundRequested    = "refund_requested"
	EventRefundSucceeded    = "refund_succeeded"
	EventUseRequested       = "use_requested"
	EventInventoryCredited  = "inventory_credited"
	EventCancelRequested    = "cancel_requested"
	EventCancelApproved     = "cancel_approved"
	EventCancelRejected     = "cancel_rejected"
	EventVerified           = "verified"
)

// FulfillmentEvent 履约时间线事件（追加写入，不替代业务状态字段）。
type FulfillmentEvent struct {
	ID          uint64          `gorm:"primaryKey" json:"id"`
	SubjectType string          `gorm:"size:32;not null;index:idx_fe_subject,priority:1" json:"subject_type"`
	SubjectID   uint64          `gorm:"not null;index:idx_fe_subject,priority:2" json:"subject_id"`
	EventCode   string          `gorm:"size:64;not null" json:"event_code"`
	ActorRole   string          `gorm:"size:16;not null;default:system" json:"actor_role"`
	ActorID     *uint64         `json:"actor_id,omitempty"`
	Title       string          `gorm:"size:128;not null" json:"title"`
	Detail      json.RawMessage `gorm:"type:json" json:"detail,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (FulfillmentEvent) TableName() string { return "fulfillment_event" }
