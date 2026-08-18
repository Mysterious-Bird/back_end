package model

import "time"

const (
	HomeCarouselStatusOff uint8 = 0
	HomeCarouselStatusOn  uint8 = 1
	HomeCarouselMaxItems  = 8

	HomeCarouselChannelDeal  = "deal"
	HomeCarouselChannelGroup = "group"
)

// HomeCarousel 首页商品轮播（管理端配置；可绑普通商品或活动商品，并区分直购/拼团）。
type HomeCarousel struct {
	ID                uint64    `gorm:"primaryKey" json:"id"`
	ProductID         uint64    `gorm:"not null;index" json:"product_id"`
	ActivityID        *uint64   `gorm:"index" json:"activity_id,omitempty"`
	ActivityProductID *uint64   `gorm:"index" json:"activity_product_id,omitempty"`
	Channel           string    `gorm:"size:16;not null;default:deal" json:"channel"`
	SortOrder         int       `gorm:"not null;default:0" json:"sort_order"`
	Status            uint8     `gorm:"not null;default:1" json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	SoftDelete
	Product *Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (HomeCarousel) TableName() string { return "home_carousel" }
