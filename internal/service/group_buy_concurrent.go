package service

import (
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

// ErrGroupBuyTeamLimit 进行中的团已达配置上限，禁止开新团（仍可参已有团）。
var ErrGroupBuyTeamLimit = fmt.Errorf("%w: 进行中的团已达上限，请加入已有团", ErrGroupBuyInvalid)

func resolveMaxConcurrentTeams(product model.Product, actGB *ActivityGroupBuyConfig) uint32 {
	if actGB != nil {
		return actGB.GroupBuyMaxConcurrentTeams
	}
	return product.GroupBuyMaxConcurrentTeams
}

// countConcurrentPendingTeams 统计未过期的进行中团数量。
// 普通团 / 活动 / 同一活动下不同活动商品彼此隔离。
// 仅计含已支付订单的团，未付款不占开团名额。
func countConcurrentPendingTeams(db *gorm.DB, groupBuyID uint64, activityID, activityProductID *uint64) (uint32, error) {
	now := time.Now()
	q := db.Table("group_buy_team t").
		Joins("JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ? AND o.pay_status = ?",
			model.NotDeleted, model.PayStatusPaid).
		Where("t.is_deleted = ? AND t.group_buy_id = ? AND t.status = ? AND t.expire_at > ?",
			model.NotDeleted, groupBuyID, model.GroupBuyTeamPending, now)
	q = applyActivityTeamScope(q, activityID, activityProductID)
	var count int64
	if err := q.Select("COUNT(DISTINCT t.id)").Scan(&count).Error; err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, nil
	}
	return uint32(count), nil
}

func canStartNewTeam(maxTeams, current uint32) bool {
	if maxTeams == 0 {
		return true
	}
	return current < maxTeams
}

func assertCanStartNewTeam(db *gorm.DB, product model.Product, gb model.GroupBuy, actGB *ActivityGroupBuyConfig, activityID, activityProductID *uint64) error {
	maxTeams := resolveMaxConcurrentTeams(product, actGB)
	if maxTeams == 0 {
		return nil
	}
	current, err := countConcurrentPendingTeams(db, gb.ID, activityID, activityProductID)
	if err != nil {
		return err
	}
	if !canStartNewTeam(maxTeams, current) {
		return ErrGroupBuyTeamLimit
	}
	return nil
}

func buildConcurrentTeamMeta(db *gorm.DB, product model.Product, gb model.GroupBuy, actGB *ActivityGroupBuyConfig, activityID, activityProductID *uint64) (canStart bool, maxTeams, current uint32, err error) {
	maxTeams = resolveMaxConcurrentTeams(product, actGB)
	current, err = countConcurrentPendingTeams(db, gb.ID, activityID, activityProductID)
	if err != nil {
		return false, maxTeams, 0, err
	}
	return canStartNewTeam(maxTeams, current), maxTeams, current, nil
}

// applyActivityTeamScope 把团列表/计数限定到普通商品、某活动、或某条活动商品。
func applyActivityTeamScope(q *gorm.DB, activityID, activityProductID *uint64) *gorm.DB {
	if activityProductID != nil {
		return q.Where("oi.activity_product_id = ?", *activityProductID)
	}
	if activityID != nil {
		return q.Where("oi.activity_id = ?", *activityID)
	}
	return q.Where("oi.activity_id IS NULL")
}
