package model

import "time"

const (
	BargainStatusOngoing   uint8 = 1
	BargainStatusOrdered   uint8 = 2
	BargainStatusExpired   uint8 = 3
	BargainStatusCancelled uint8 = 4
)

// BargainSession 砍价会话（发起人分享拉人砍价）。
type BargainSession struct {
	ID                 uint64    `gorm:"primaryKey" json:"id"`
	ActivityID         uint64    `gorm:"not null;index" json:"activity_id"`
	ActivityProductID  uint64    `gorm:"not null;index" json:"activity_product_id"`
	ProductID          uint64    `gorm:"not null" json:"product_id"`
	MerchantID         uint64    `gorm:"not null" json:"merchant_id"`
	InitiatorAccountID uint64    `gorm:"not null;index" json:"initiator_account_id"`
	OriginPrice        float64   `gorm:"type:decimal(10,2);not null" json:"origin_price"`
	FloorPrice         float64   `gorm:"type:decimal(10,2);not null" json:"floor_price"`
	CurrentPrice       float64   `gorm:"type:decimal(10,2);not null" json:"current_price"`
	Status             uint8     `gorm:"not null;default:1" json:"status"`
	SelfCutDone        uint8     `gorm:"not null;default:0" json:"self_cut_done"`
	ExpireAt           time.Time `gorm:"not null" json:"expire_at"`
	OrderID            *uint64   `json:"order_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	SoftDelete
}

func (BargainSession) TableName() string { return "bargain_session" }

// BargainHelp 单次帮砍记录；同一会话同一账号唯一。
type BargainHelp struct {
	ID              uint64    `gorm:"primaryKey" json:"id"`
	SessionID       uint64    `gorm:"not null;uniqueIndex:uk_bargain_help" json:"session_id"`
	HelperAccountID uint64    `gorm:"not null;uniqueIndex:uk_bargain_help;index" json:"helper_account_id"`
	CutAmount       float64   `gorm:"type:decimal(10,2);not null" json:"cut_amount"`
	IsNewUser       uint8     `gorm:"not null;default:0" json:"is_new_user"`
	CreatedAt       time.Time `json:"created_at"`
}

func (BargainHelp) TableName() string { return "bargain_help" }

// BargainSettings 砍价全局配置（单行 id=1）。
type BargainSettings struct {
	ID                   uint8     `gorm:"primaryKey" json:"id"`
	HelpDailyMax         uint32    `gorm:"not null;default:20" json:"help_daily_max"`
	HelpDailyRefreshTime string    `gorm:"type:time;not null;default:00:00:00" json:"help_daily_refresh_time"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (BargainSettings) TableName() string { return "bargain_settings" }

const (
	BargainCutModeRandom uint8 = 1
	BargainCutModeFixed  uint8 = 2
)
