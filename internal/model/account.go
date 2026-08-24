package model

import "time"

const (
	AccountTypeUser     uint8 = 1
	AccountTypeMerchant uint8 = 2
	AccountTypeAdmin    uint8 = 3
	// AccountTypeRider 已废弃：骑手资格见 IsRider 字段，不再占用 type。
	AccountTypeRider uint8 = 4
)

const (
	AccountStatusDisabled uint8 = 0 // 拉黑/禁用：不可登录与使用
	AccountStatusActive   uint8 = 1 // 正常
)

func AccountStatusText(status uint8) string {
	switch status {
	case AccountStatusActive:
		return "正常"
	case AccountStatusDisabled:
		return "已拉黑"
	default:
		return "未知"
	}
}

func AccountTypeText(accountType uint8) string {
	switch accountType {
	case AccountTypeUser:
		return "普通用户"
	case AccountTypeMerchant:
		return "商家"
	case AccountTypeAdmin:
		return "管理员"
	case AccountTypeRider:
		return "骑手"
	default:
		return "未知"
	}
}

type Account struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`
	Type         uint8      `gorm:"not null" json:"type"`
	OpenID       *string    `gorm:"column:openid;size:64" json:"openid,omitempty"`
	UnionID      *string    `gorm:"column:unionid;size:64" json:"unionid,omitempty"`
	Phone        *string    `gorm:"size:20" json:"phone,omitempty"`
	Email        *string    `gorm:"size:128" json:"email,omitempty"`
	PasswordHash *string    `gorm:"column:password_hash;size:255" json:"-"`
	Nickname     *string    `gorm:"size:64" json:"nickname,omitempty"`
	AvatarURL    *string    `gorm:"column:avatar_url;size:512" json:"avatar_url,omitempty"`
	Gender       uint8      `gorm:"not null;default:0" json:"gender"`
	Status       uint8      `gorm:"not null;default:1" json:"status"`
	IsRider      uint8      `gorm:"column:is_rider;not null;default:0" json:"is_rider"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at" json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SoftDelete
}

func (Account) TableName() string {
	return "account"
}
