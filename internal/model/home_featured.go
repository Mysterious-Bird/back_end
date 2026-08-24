package model

import "time"

const (
	HomeFeaturedStatusOff uint8 = 0
	HomeFeaturedStatusOn  uint8 = 1

	HomeFeaturedSectionPickup  = "pickup"
	HomeFeaturedSectionDeal    = "deal"
	HomeFeaturedSectionFood    = "food"
	HomeFeaturedSectionHomeRail = "home_rail"

	HomeFeaturedTypePinned = "pinned"
	HomeFeaturedTypeHidden = "hidden"
)

// HomeFeatured 首页专区展示（自取 / 团购 / 美食），管理端可手动配置排序。
type HomeFeatured struct {
	ID                uint64    `gorm:"primaryKey" json:"id"`
	Section           string    `gorm:"size:16;not null;index" json:"section"`
	ItemType          string    `gorm:"size:16;not null;default:pinned;index" json:"item_type"`
	ProductID         *uint64   `gorm:"index" json:"product_id,omitempty"`
	MerchantID        *uint64   `gorm:"index" json:"merchant_id,omitempty"`
	ActivityID        *uint64   `gorm:"index" json:"activity_id,omitempty"`
	ActivityProductID *uint64   `gorm:"index" json:"activity_product_id,omitempty"`
	Channel           string    `gorm:"size:16;not null;default:deal" json:"channel"`
	SortOrder         int       `gorm:"not null;default:0" json:"sort_order"`
	Status            uint8     `gorm:"not null;default:1" json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	SoftDelete
}

func (HomeFeatured) TableName() string { return "home_featured" }

func ValidHomeFeaturedSection(section string) bool {
	switch section {
	case HomeFeaturedSectionPickup, HomeFeaturedSectionDeal, HomeFeaturedSectionFood, HomeFeaturedSectionHomeRail:
		return true
	default:
		return false
	}
}
