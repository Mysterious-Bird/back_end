package service

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRiderEarningNotFound  = errors.New("rider earning not found")
	ErrSettlementNotFound    = errors.New("settlement not found")
	ErrSettlementInvalid     = errors.New("settlement invalid")
	ErrInsufficientEarnings  = errors.New("insufficient pending earnings")
	ErrRiderNotFound         = errors.New("rider not found")
)

// RiderEarningService 骑手收益与结账服务。
// 金额安全：所有结账操作在事务+行锁内完成，保证 SUM(已结账收益) == settlement.amount。
type RiderEarningService struct {
	DB *gorm.DB
}

// EarningsSummary 骑手收益汇总。
type EarningsSummary struct {
	PendingAmount  float64 `json:"pending_amount"`   // 待结账金额（含待审批申请占用的部分仍属于待结账）
	SettledAmount  float64 `json:"settled_amount"`   // 已结账金额
	PendingCount   int64   `json:"pending_count"`    // 待结账笔数
	SettledCount   int64   `json:"settled_count"`    // 已结账笔数
	WithdrawingAmount float64 `json:"withdrawing_amount"` // 待审批结账申请占用金额（不可重复申请）
	WithdrawingCount  int64   `json:"withdrawing_count"`  // 待审批结账单数
}

// EarningView 骑手收益记录视图。
type EarningView struct {
	model.RiderEarning
	StatusText    string  `json:"status_text"`
	ShopName      string  `json:"shop_name"`
	OrderNo       string  `json:"order_no"`
	ProductName   string  `json:"product_name"`
}

// SettlementView 骑手结账记录视图。
type SettlementView struct {
	model.RiderSettlement
	StatusText   string `json:"status_text"`
	SourceText   string `json:"source_text"`
	RiderName    string `json:"rider_name"`
	OperatorName string `json:"operator_name"`
}

// RiderOverview 管理端骑手概览。
type RiderOverview struct {
	AccountID     uint64  `json:"account_id"`
	Nickname      string  `json:"nickname"`
	Phone         string  `json:"phone"`
	RealName      string  `json:"real_name"`
	IsRider       uint8   `json:"is_rider"`
	PendingAmount float64 `json:"pending_amount"`
	SettledAmount float64 `json:"settled_amount"`
	WithdrawingAmount float64 `json:"withdrawing_amount"`
	WithdrawingCount  int64   `json:"withdrawing_count"` // 待审批结账单数（>0 表示有提现申请待处理）
	TotalDeliveries int64 `json:"total_deliveries"`
	CompletedDeliveries int64 `json:"completed_deliveries"`
}

// GetSummary 骑手查自己的收益汇总。
func (s *RiderEarningService) GetSummary(riderID uint64) (*EarningsSummary, error) {
	out := &EarningsSummary{}
	db := query.NotDeleted(s.DB.Model(&model.RiderEarning{})).Where("rider_id = ?", riderID)
	if err := db.Where("status = ?", model.RiderEarningPending).Count(&out.PendingCount).Error; err != nil {
		return nil, err
	}
	if err := db.Where("status = ?", model.RiderEarningSettled).Count(&out.SettledCount).Error; err != nil {
		return nil, err
	}

	var pendingSum, settledSum float64
	if err := query.NotDeleted(s.DB.Model(&model.RiderEarning{})).
		Where("rider_id = ? AND status = ?", riderID, model.RiderEarningPending).
		Select("COALESCE(SUM(amount),0)").Row().Scan(&pendingSum); err != nil {
		return nil, err
	}
	if err := query.NotDeleted(s.DB.Model(&model.RiderEarning{})).
		Where("rider_id = ? AND status = ?", riderID, model.RiderEarningSettled).
		Select("COALESCE(SUM(amount),0)").Row().Scan(&settledSum); err != nil {
		return nil, err
	}
	out.PendingAmount = roundMoney(pendingSum)
	out.SettledAmount = roundMoney(settledSum)

	// 待审批结账申请占用金额 + 笔数
	var withdrawingSum float64
	if err := query.NotDeleted(s.DB.Model(&model.RiderSettlement{})).
		Where("rider_id = ? AND status = ?", riderID, model.RiderSettlementPending).
		Select("COALESCE(SUM(amount),0)").Row().Scan(&withdrawingSum); err != nil {
		return nil, err
	}
	out.WithdrawingAmount = roundMoney(withdrawingSum)
	if err := query.NotDeleted(s.DB.Model(&model.RiderSettlement{})).
		Where("rider_id = ? AND status = ?", riderID, model.RiderSettlementPending).
		Count(&out.WithdrawingCount).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ListEarnings 骑手查自己的收益记录。
func (s *RiderEarningService) ListEarnings(riderID uint64, status string, page, pageSize int) ([]EarningView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.RiderEarning{})).Where("rider_id = ?", riderID)
	if status != "" {
		switch status {
		case "pending":
			q = q.Where("status = ?", model.RiderEarningPending)
		case "settled":
			q = q.Where("status = ?", model.RiderEarningSettled)
		case "cancelled":
			q = q.Where("status = ?", model.RiderEarningCancelled)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.RiderEarning
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]EarningView, 0, len(list))
	for i := range list {
		views = append(views, s.toEarningView(&list[i]))
	}
	return views, total, nil
}

func (s *RiderEarningService) toEarningView(e *model.RiderEarning) EarningView {
	text := "待结账"
	switch e.Status {
	case model.RiderEarningSettled:
		text = "已结账"
	case model.RiderEarningCancelled:
		text = "已取消"
	}
	v := EarningView{
		RiderEarning: *e,
		StatusText:   text,
	}
	// 补充商家名/订单号/商品名（冗余查询，收益记录量级不大）
	if e.MerchantID > 0 {
		var m model.MerchantProfile
		if err := s.DB.Select("shop_name").First(&m, e.MerchantID).Error; err == nil {
			v.ShopName = m.ShopName
		}
	}
	if e.OrderID != nil {
		var o model.Order
		if err := s.DB.Select("order_no").First(&o, *e.OrderID).Error; err == nil {
			v.OrderNo = o.OrderNo
		}
	}
	if e.DeliveryOrderID > 0 {
		var d model.DeliveryOrder
		if err := s.DB.Preload("Order.Items").First(&d, e.DeliveryOrderID).Error; err == nil {
			if d.Order != nil && len(d.Order.Items) > 0 {
				v.ProductName = d.Order.Items[0].ProductName
			}
			if v.OrderNo == "" && d.Order != nil {
				v.OrderNo = d.Order.OrderNo
			}
		}
	}
	if e.ErrandOrderID != nil {
		var eo model.ErrandOrder
		if err := s.DB.Select("id", "order_no", "goods_desc", "pickup_snapshot").First(&eo, *e.ErrandOrderID).Error; err == nil {
			v.OrderNo = eo.OrderNo
			desc := []rune(eo.GoodsDesc)
			if len(desc) > 24 {
				desc = append(desc[:24], '…')
			}
			v.ProductName = "跑腿：" + string(desc)
			if eo.PickupSnapshot != nil && eo.PickupSnapshot.ShopName != "" {
				v.ShopName = eo.PickupSnapshot.ShopName
			} else {
				v.ShopName = "店外商家"
			}
		}
	}
	return v
}

// ListSettlements 骑手查自己的结账记录。
func (s *RiderEarningService) ListSettlements(riderID uint64, page, pageSize int) ([]SettlementView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.RiderSettlement{})).Where("rider_id = ?", riderID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.RiderSettlement
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]SettlementView, 0, len(list))
	for i := range list {
		views = append(views, toSettlementView(&list[i]))
	}
	return views, total, nil
}

func toSettlementView(st *model.RiderSettlement) SettlementView {
	statusText := "待审批"
	switch st.Status {
	case model.RiderSettlementApproved:
		statusText = "已通过"
	case model.RiderSettlementRejected:
		statusText = "已拒绝"
	}
	sourceText := "骑手申请"
	if st.Source == model.RiderSettlementSourceAdmin {
		sourceText = "管理员主动"
	}
	return SettlementView{
		RiderSettlement: *st,
		StatusText:      statusText,
		SourceText:      sourceText,
	}
}

// RequestSettlement 骑手申请全额结账。
// 申请金额 = 当前待结账余额 - 待审批申请占用金额（防止重复申请）。
func (s *RiderEarningService) RequestSettlement(riderID uint64) (*model.RiderSettlement, error) {
	var st *model.RiderSettlement
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// 锁定骑手的待结账收益记录
		var earnings []model.RiderEarning
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("rider_id = ? AND status = ?", riderID, model.RiderEarningPending).
			Find(&earnings).Error; err != nil {
			return err
		}
		var pendingSum float64
		for _, e := range earnings {
			pendingSum += e.Amount
		}
		pendingSum = roundMoney(pendingSum)

		// 待审批申请占用金额（行锁防并发超占）
			var withdrawingSum float64
			if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&model.RiderSettlement{})).
				Where("rider_id = ? AND status = ?", riderID, model.RiderSettlementPending).
				Select("COALESCE(SUM(amount),0)").Row().Scan(&withdrawingSum); err != nil {
				return err
			}
			withdrawingSum = roundMoney(withdrawingSum)

			available := roundMoney(pendingSum - withdrawingSum)
			if available <= 0 {
				return ErrInsufficientEarnings
			}

			now := time.Now()
			st = &model.RiderSettlement{
				RiderID:     riderID,
				Amount:      available,
				Status:      model.RiderSettlementPending,
				Source:      model.RiderSettlementSourceRider,
				ApplicantID: &riderID,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			return tx.Create(st).Error
		})
		if err != nil {
			return nil, err
		}
		return st, nil
	}

	// AdminCreateSettlement 管理员主动结账（输入金额）。
	func (s *RiderEarningService) AdminCreateSettlement(riderID uint64, amount float64, operatorID uint64) (*model.RiderSettlement, error) {
		amount = roundMoney(amount)
		if amount <= 0 {
			return nil, ErrSettlementInvalid
		}
		var st *model.RiderSettlement
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			// 锁定待结账收益
			var earnings []model.RiderEarning
			if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
				Where("rider_id = ? AND status = ?", riderID, model.RiderEarningPending).
				Find(&earnings).Error; err != nil {
				return err
			}
			var pendingSum float64
			for _, e := range earnings {
				pendingSum += e.Amount
			}
			pendingSum = roundMoney(pendingSum)

			// 扣除待审批申请占用（行锁防并发超占）
			var withdrawingSum float64
			if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&model.RiderSettlement{})).
				Where("rider_id = ? AND status = ?", riderID, model.RiderSettlementPending).
				Select("COALESCE(SUM(amount),0)").Row().Scan(&withdrawingSum); err != nil {
				return err
			}
		withdrawingSum = roundMoney(withdrawingSum)
		available := roundMoney(pendingSum - withdrawingSum)
		if amount > available {
			return ErrInsufficientEarnings
		}

		now := time.Now()
		st = &model.RiderSettlement{
			RiderID:    riderID,
			Amount:     amount,
			Status:     model.RiderSettlementPending,
			Source:     model.RiderSettlementSourceAdmin,
			OperatorID: &operatorID,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		return tx.Create(st).Error
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// AdminReviewSettlement 管理员审批结账申请。
// 通过：按金额匹配若干条 Pending 收益标记 Settled，保证 SUM == amount。
// 拒绝：仅更新状态，收益不变（仍在待结账）。
func (s *RiderEarningService) AdminReviewSettlement(settlementID uint64, approve bool, operatorID uint64, rejectReason *string) (*model.RiderSettlement, error) {
	var st *model.RiderSettlement
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// 锁定结账单并读取最新值
		var cur model.RiderSettlement
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&cur, settlementID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSettlementNotFound
			}
			return err
		}
		if cur.Status != model.RiderSettlementPending {
			return ErrSettlementInvalid
		}
		now := time.Now()

		if !approve {
			// 拒绝：仅更新状态
			if err := tx.Model(&cur).Updates(map[string]interface{}{
				"status":        model.RiderSettlementRejected,
				"operator_id":   operatorID,
				"reviewed_at":   now,
				"reject_reason": rejectReason,
				"updated_at":    now,
			}).Error; err != nil {
				return err
			}
			cur.Status = model.RiderSettlementRejected
			cur.OperatorID = &operatorID
			cur.ReviewedAt = &now
			cur.RejectReason = rejectReason
			st = &cur
			return nil
		}

		// 通过：锁定该骑手的待结账收益，按金额匹配若干条标记已结账
		var earnings []model.RiderEarning
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("rider_id = ? AND status = ?", cur.RiderID, model.RiderEarningPending).
			Order("id ASC").Find(&earnings).Error; err != nil {
			return err
		}

		// 按金额匹配收益：累加直到达到结账金额，最后一条若超出则拆分
		remaining := cur.Amount
		var fullyMatchedIDs []uint64
		var splitEarning *model.RiderEarning // 需要拆分的收益（部分结账）
		var splitKeepAmount float64          // 拆分后保留待结账的金额
		var matchedSum float64
		for i := range earnings {
			if remaining <= 0.0001 {
				break
			}
			e := &earnings[i]
			if e.Amount <= remaining+0.0001 {
				// 整条匹配
				fullyMatchedIDs = append(fullyMatchedIDs, e.ID)
				matchedSum += e.Amount
				remaining = roundMoney(remaining - e.Amount)
			} else {
				// 单条收益 > 剩余金额：拆分此条
				splitEarning = e
				splitKeepAmount = roundMoney(e.Amount - remaining)
				matchedSum += remaining
				remaining = 0
				break
			}
		}
		if remaining > 0.01 {
			// 待结账收益总额不足以支付结账金额（可能被其他结账单占用或已取消）
			var available float64
			for i := range earnings {
				available += earnings[i].Amount
			}
			return fmt.Errorf("%w: 待结账收益 ¥%.2f 不足结账金额 ¥%.2f", ErrInsufficientEarnings, available, cur.Amount)
		}

		// 标记整条匹配的收益为已结账
		if len(fullyMatchedIDs) > 0 {
			if err := tx.Model(&model.RiderEarning{}).Where("id IN ?", fullyMatchedIDs).
				Updates(map[string]interface{}{
					"status":        model.RiderEarningSettled,
					"settlement_id": cur.ID,
					"settled_at":    now,
				}).Error; err != nil {
				return err
			}
		}
		// 拆分：把原收益金额改为已结账部分（remaining 的补数），标记已结账；
		// 再创建一条新的待结账收益记录保留剩余金额。
		if splitEarning != nil {
			settledPart := roundMoney(splitEarning.Amount - splitKeepAmount)
			if err := tx.Model(splitEarning).Updates(map[string]interface{}{
				"amount":        settledPart,
				"status":        model.RiderEarningSettled,
				"settlement_id": cur.ID,
				"settled_at":    now,
			}).Error; err != nil {
				return err
			}
			keep := model.RiderEarning{
				RiderID:         splitEarning.RiderID,
				DeliveryOrderID: splitEarning.DeliveryOrderID,
				OrderID:         splitEarning.OrderID,
				MerchantID:      splitEarning.MerchantID,
				Amount:          splitKeepAmount,
				Status:          model.RiderEarningPending,
				CreatedAt:       splitEarning.CreatedAt,
			}
			if err := tx.Create(&keep).Error; err != nil {
				return err
			}
		}

		// 更新结账单状态
		if err := tx.Model(&cur).Updates(map[string]interface{}{
			"status":      model.RiderSettlementApproved,
			"operator_id": operatorID,
			"reviewed_at": now,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}
		cur.Status = model.RiderSettlementApproved
		cur.OperatorID = &operatorID
		cur.ReviewedAt = &now
		st = &cur
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// AdminListRiders 管理端骑手列表（带收益汇总）。
func (s *RiderEarningService) AdminListRiders(keyword string, page, pageSize int) ([]RiderOverview, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := s.DB.Model(&model.Account{}).Where("is_rider = ?", 1)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("nickname LIKE ? OR phone LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var accounts []model.Account
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}

	overviews := make([]RiderOverview, 0, len(accounts))
	for _, acc := range accounts {
		ov := RiderOverview{
			AccountID: acc.ID,
			IsRider:   acc.IsRider,
		}
		if acc.Nickname != nil {
			ov.Nickname = *acc.Nickname
		}
		if acc.Phone != nil {
			ov.Phone = *acc.Phone
		}
		// 真实姓名从骑手申请表取
		var app model.RiderApplication
		if err := s.DB.Where("account_id = ? AND status = ?", acc.ID, model.RiderApplicationApproved).
			Order("id DESC").First(&app).Error; err == nil {
			ov.RealName = app.RealName
		}
		// 收益汇总
		if sum, err := s.GetSummary(acc.ID); err == nil {
			ov.PendingAmount = sum.PendingAmount
			ov.SettledAmount = sum.SettledAmount
			ov.WithdrawingAmount = sum.WithdrawingAmount
			ov.WithdrawingCount = sum.WithdrawingCount
		}
	// 送餐统计
	query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where("rider_id = ?", acc.ID).Count(&ov.TotalDeliveries)
	query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where("rider_id = ? AND status = ?", acc.ID, model.DeliveryConfirmed).Count(&ov.CompletedDeliveries)
	overviews = append(overviews, ov)
}
return overviews, total, nil
}

// AdminGetRider 管理端骑手详情。
func (s *RiderEarningService) AdminGetRider(riderID uint64) (*RiderOverview, error) {
	var acc model.Account
	if err := s.DB.First(&acc, riderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRiderNotFound
		}
		return nil, err
	}
	ov := &RiderOverview{
		AccountID: acc.ID,
		IsRider:   acc.IsRider,
	}
	if acc.Nickname != nil {
		ov.Nickname = *acc.Nickname
	}
	if acc.Phone != nil {
		ov.Phone = *acc.Phone
	}
	var app model.RiderApplication
	if err := s.DB.Where("account_id = ? AND status = ?", acc.ID, model.RiderApplicationApproved).
		Order("id DESC").First(&app).Error; err == nil {
		ov.RealName = app.RealName
	}
	if sum, err := s.GetSummary(acc.ID); err == nil {
		ov.PendingAmount = sum.PendingAmount
		ov.SettledAmount = sum.SettledAmount
		ov.WithdrawingAmount = sum.WithdrawingAmount
		ov.WithdrawingCount = sum.WithdrawingCount
	}
	query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where("rider_id = ?", acc.ID).Count(&ov.TotalDeliveries)
	query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where("rider_id = ? AND status = ?", acc.ID, model.DeliveryConfirmed).Count(&ov.CompletedDeliveries)
	return ov, nil
}

// AdminRevokeRider 撤销骑手身份（is_rider=0），不删除历史收益。
func (s *RiderEarningService) AdminRevokeRider(riderID uint64) error {
	return s.DB.Model(&model.Account{}).Where("id = ?", riderID).Update("is_rider", 0).Error
}

// AdminListRiderEarnings 管理端查某骑手收益记录。
func (s *RiderEarningService) AdminListRiderEarnings(riderID uint64, page, pageSize int) ([]EarningView, int64, error) {
	return s.ListEarnings(riderID, "", page, pageSize)
}

// AdminListRiderSettlements 管理端查某骑手结账记录。
func (s *RiderEarningService) AdminListRiderSettlements(riderID uint64, page, pageSize int) ([]SettlementView, int64, error) {
	return s.ListSettlements(riderID, page, pageSize)
}

// AdminListRiderDeliveries 管理端查某骑手送餐记录。
func (s *RiderEarningService) AdminListRiderDeliveries(riderID uint64, page, pageSize int) ([]DeliveryBriefView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where("rider_id = ?", riderID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]DeliveryBriefView, 0, len(list))
	for i := range list {
		views = append(views, toDeliveryBriefView(&list[i]))
	}
	return views, total, nil
}

// DeliveryBriefView 送餐记录简要视图（管理端用）。
type DeliveryBriefView struct {
	ID              uint64  `json:"id"`
	Status          uint8   `json:"status"`
	StatusText      string  `json:"status_text"`
	ProductName     string  `json:"product_name"`
	Quantity        uint32  `json:"quantity"`
	OrderNo         string  `json:"order_no"`
	RiderEarnings   float64 `json:"rider_earnings"`
	CreatedAt       string  `json:"created_at"`
	DeliveredAt     string  `json:"delivered_at"`
}

func toDeliveryBriefView(d *model.DeliveryOrder) DeliveryBriefView {
	v := DeliveryBriefView{
		ID:            d.ID,
		Status:        d.Status,
		StatusText:    model.DeliveryStatusText(d.Status),
		RiderEarnings: d.RiderEarnings,
		CreatedAt:     formatTimeGo(d.CreatedAt),
	}
	if d.DeliveredAt != nil {
		v.DeliveredAt = formatTimeGo(*d.DeliveredAt)
	}
	if d.Order != nil {
		v.OrderNo = d.Order.OrderNo
		if len(d.Order.Items) > 0 {
			v.ProductName = d.Order.Items[0].ProductName
			v.Quantity = d.Order.Items[0].Quantity
		}
	}
	if d.InventoryUsage != nil {
		v.Quantity = d.InventoryUsage.Quantity
		if d.InventoryUsage.Product != nil {
			v.ProductName = d.InventoryUsage.Product.Name
		}
	}
	return v
}

func formatTimeGo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// AdminListSettlements 管理端查结账列表，支持按 status 筛选。
// status: ""=全部, "pending"=待审批, "approved"=已通过, "rejected"=已拒绝
func (s *RiderEarningService) AdminListSettlements(status string, page, pageSize int) ([]SettlementView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	q := query.NotDeleted(s.DB.Model(&model.RiderSettlement{}))
	switch status {
	case "pending":
		q = q.Where("status = ?", model.RiderSettlementPending)
	case "approved":
		q = q.Where("status = ?", model.RiderSettlementApproved)
	case "rejected":
		q = q.Where("status = ?", model.RiderSettlementRejected)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.RiderSettlement
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]SettlementView, 0, len(list))
	for i := range list {
		v := toSettlementView(&list[i])
		// 补充骑手名
		var acc model.Account
		if err := s.DB.Select("nickname, phone").First(&acc, list[i].RiderID).Error; err == nil {
			if acc.Nickname != nil && *acc.Nickname != "" {
				v.RiderName = *acc.Nickname
			} else if acc.Phone != nil {
				v.RiderName = *acc.Phone
			}
		}
		views = append(views, v)
	}
	return views, total, nil
}

// AdminListPendingSettlements 管理端查待审批结账列表。
func (s *RiderEarningService) AdminListPendingSettlements(page, pageSize int) ([]SettlementView, int64, error) {
	return s.AdminListSettlements("pending", page, pageSize)
}

// AdminCountPendingSettlements 管理端待审批结账总数（用于角标）。
func (s *RiderEarningService) AdminCountPendingSettlements() (int64, error) {
	var n int64
	if err := query.NotDeleted(s.DB.Model(&model.RiderSettlement{})).
		Where("status = ?", model.RiderSettlementPending).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
