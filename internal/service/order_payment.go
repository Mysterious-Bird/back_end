package service

import (
	"fmt"
	"log"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/payment/wechatv3"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *OrderService) settlePaymentInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	p := s.paymentProvider()
	if p.ImmediateSettle() {
		// mock：事务内立即结算 + 推进订单状态（PendingPay -> PendingFulfill，拼团 PendingGroup 不动）
		if err := p.SettlePaidInTx(tx, orderID, payAmount, at); err != nil {
			return err
		}
		return s.advanceAfterPaidInTx(tx, orderID)
	}
	// 零元单（砍价到底价 0 / 优惠券抵扣至 0）：微信无法下单 amount=0，本地免支付并推进履约
	if isZeroMoney(payAmount) {
		if err := markOrderPaidLocalInTx(tx, orderID, at); err != nil {
			return err
		}
		return s.advanceAfterPaidInTx(tx, orderID)
	}
	// wechat：正价不结算，订单保留 PendingPay，交由 CreatePrepay + HandleNotify 推进
	return nil
}

// markOrderPaidLocalInTx 本地标记入包订单已支付（零元免支付；不经微信回调）。
func markOrderPaidLocalInTx(tx *gorm.DB, orderID uint64, at time.Time) error {
	if orderID == 0 {
		return fmt.Errorf("invalid order id")
	}
	res := query.NotDeleted(tx.Model(&model.Order{})).
		Where("id = ? AND pay_status = ?", orderID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"pay_status": model.PayStatusPaid,
			"paid_at":    at,
			"prepay_id":  nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var o model.Order
		if err := query.NotDeleted(tx).Select("id", "pay_status").First(&o, orderID).Error; err != nil {
			return err
		}
		if o.PayStatus == model.PayStatusPaid {
			return nil
		}
		return fmt.Errorf("order %d pay status not unpaid", orderID)
	}
	return nil
}

// advanceAfterPaidInTx 仅把 status=PendingPay 的订单推进到 PendingFulfill。
// 若商家开启自动审核，则直接入背包；否则进入待商家审核。
// 拼团单（PendingGroup）不被推进，由 tryCompleteGroup 成团后推进。
func (s *OrderService) advanceAfterPaidInTx(tx *gorm.DB, orderID uint64) error {
	return s.AdvanceAfterPaidInTx(tx, orderID)
}

// AdvanceAfterPaidInTx 供支付渠道（微信回调）注入调用。
// 直购 PendingPay → 待履约；拼团 PendingGroup 保持待成团，按已支付人数尝试成团。
// 若本地已被 pay-expire 关成 Closed/Cancelled 但微信已付款，先恢复业务状态再入团/履约。
func (s *OrderService) AdvanceAfterPaidInTx(tx *gorm.DB, orderID uint64) error {
	if err := s.healPaidButClosedOrderInTx(tx, orderID); err != nil {
		return err
	}
	res := tx.Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, model.OrderStatusPendingPay).
		Updates(map[string]interface{}{
			"status":                model.OrderStatusPendingFulfill,
			"merchant_review_stage": model.MerchantReviewPending,
			"pay_expire_at":         nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return s.maybeAutoApproveInTx(tx, orderID)
	}
	// 拼团单支付成功：尝试成团并推进已付成员
	if err := s.tryCompleteGroupForOrderInTx(tx, orderID); err != nil {
		return err
	}
	// 团已成功时本单可能刚被推进为待履约，补一次自动审核
	return s.maybeAutoApproveInTx(tx, orderID)
}

// healPaidButClosedOrderInTx 迟到回调：微信已付但本地被超时关单时，按购买方式恢复状态。
// 关单时已回退库存/活动销量，此处重新占用，避免「已付款却无库存扣减」。
func (s *OrderService) healPaidButClosedOrderInTx(tx *gorm.DB, orderID uint64) error {
	var order model.Order
	if err := query.NotDeleted(tx).First(&order, orderID).Error; err != nil {
		return err
	}
	if order.PayStatus != model.PayStatusPaid {
		return nil
	}
	if order.Status != model.OrderStatusClosed && order.Status != model.OrderStatusCancelled {
		return nil
	}
	var groupCnt int64
	if err := query.NotDeleted(tx.Model(&model.OrderItem{})).
		Where("order_id = ? AND purchase_type = ?", orderID, model.PurchaseTypeGroup).
		Count(&groupCnt).Error; err != nil {
		return err
	}
	status := model.OrderStatusPendingFulfill
	review := model.MerchantReviewPending
	if groupCnt > 0 {
		status = model.OrderStatusPendingGroup
		review = model.MerchantReviewNone
	}
	if err := tx.Model(&order).Updates(map[string]interface{}{
		"status":                status,
		"merchant_review_stage": review,
		"pay_expire_at":         nil,
	}).Error; err != nil {
		return err
	}
	if err := deductProductStockForOrder(tx, orderID); err != nil {
		return err
	}
	if s.ActivitySvc != nil {
		// 关单时 RollbackSold 已减日限/已售；状态恢复后按订单对账重占日限，勿再 CreditSold（会重复加）。
		if err := s.ActivitySvc.ReholdAfterOrderRestoreInTx(tx, orderID); err != nil {
			return err
		}
	}
	return nil
}

func (s *OrderService) maybeAutoApproveInTx(tx *gorm.DB, orderID uint64) error {
	var order model.Order
	if err := query.NotDeleted(tx).First(&order, orderID).Error; err != nil {
		return err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewPending {
		return nil
	}
	if order.MerchantID == 0 {
		return nil
	}
	var mp model.MerchantProfile
	if err := query.NotDeleted(tx).Select("id", "auto_approve").First(&mp, order.MerchantID).Error; err != nil {
		return nil // 商家缺失时保持待审，不阻断支付
	}
	if mp.AutoApprove != 1 {
		return nil
	}
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ?", orderID).Find(&items).Error; err != nil {
		return err
	}
	if err := s.creditOrderInventory(tx, order.AccountID, orderID, items); err != nil {
		return err
	}
	if s.InventorySvc != nil {
		if err := s.InventorySvc.AutoPickupAfterCredit(tx, order.AccountID, orderID, resolveUsageMerchantID(&order)); err != nil {
			return err
		}
	}
	return tx.Model(&order).Update("merchant_review_stage", model.MerchantReviewApproved).Error
}

// healPendingIfAutoApprove 自动审核店铺下，把卡死的待审单标为已通过（入背包幂等）。
func (s *OrderService) healPendingIfAutoApprove(merchantID, orderID uint64) error {
	var mp model.MerchantProfile
	if err := query.NotDeleted(s.DB).Select("id", "auto_approve").First(&mp, merchantID).Error; err != nil {
		return err
	}
	if mp.AutoApprove != 1 {
		return fmt.Errorf("auto approve off")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		return s.maybeAutoApproveInTx(tx, orderID)
	})
}

// AutoApprovePendingForMerchant 商家开启自动审核后，将已有待审订单批量入背包。
func (s *OrderService) AutoApprovePendingForMerchant(merchantID uint64) (int, error) {
	if merchantID == 0 {
		return 0, nil
	}
	var ids []uint64
	if err := query.NotDeleted(s.DB.Model(&model.Order{})).
		Where("merchant_id = ? AND status = ? AND merchant_review_stage = ?",
			merchantID, model.OrderStatusPendingFulfill, model.MerchantReviewPending).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			return s.maybeAutoApproveInTx(tx, id)
		})
		if err != nil {
			log.Printf("[auto-approve] merchant %d order %d failed: %v", merchantID, id, err)
			continue
		}
		n++
	}
	return n, nil
}

func (s *OrderService) refundPaymentInTx(tx *gorm.DB, orderID uint64) error {
	p := s.Payment
	if p == nil {
		p = &payment.MockProvider{DB: s.DB}
	}
	return p.RefundInTx(tx, orderID)
}

func (s *OrderService) refundAmountInTx(tx *gorm.DB, orderID uint64, amount float64, reason string) error {
	return s.paymentProvider().RefundAmountInTx(tx, orderID, amount, reason)
}

// runTx 包装事务：绑定微信退款收集器，提交成功后再异步发起退款。
func (s *OrderService) runTx(fn func(tx *gorm.DB) error) error {
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		return fn(tx)
	})
	if err != nil {
		return err
	}
	payment.DispatchRefundJobs(jobs)
	return nil
}

func (s *OrderService) paymentProvider() payment.Provider {
	if s.Payment != nil {
		return s.Payment
	}
	return &payment.MockProvider{DB: s.DB}
}

// PaymentProviderInfo 当前支付渠道（供前端决定是否调起收银台）。
func (s *OrderService) PaymentProviderInfo() map[string]interface{} {
	p := s.paymentProvider()
	return map[string]interface{}{
		"provider":         p.Name(),
		"immediate_settle": p.ImmediateSettle(),
	}
}

// CreatePrepay 预支付。Mock：若未付则补结算；已付则幂等返回。
func (s *OrderService) CreatePrepay(accountID, orderID uint64) (*payment.PrepayResult, error) {
	return s.paymentProvider().CreatePrepay(orderID, accountID)
}

// CreatePrepayForSubject 按支付主体预支付（外卖/跑腿等）。
func (s *OrderService) CreatePrepayForSubject(sub payment.PaySubject) (*payment.PrepayResult, error) {
	return s.paymentProvider().CreatePrepayForSubject(sub)
}

func (s *OrderService) SettleSubjectPaidInTx(tx *gorm.DB, sub payment.PaySubject, at time.Time) error {
	p := s.paymentProvider()
	if err := p.SettleSubjectPaidInTx(tx, sub, at); err != nil {
		return err
	}
	if sub.Type == model.PaySubjectOrder && p.ImmediateSettle() {
		return s.advanceAfterPaidInTx(tx, sub.ID)
	}
	return nil
}

// HandlePaymentNotify 支付渠道异步回调入口（微信桩预留）。
func (s *OrderService) HandlePaymentNotify(headers map[string]string, body []byte) (*payment.NotifyResult, error) {
	return s.paymentProvider().HandleNotify(headers, body)
}

// orderHasSuccessfulGroup 成团成功后的订单：禁止用户单方面取消，避免打乱整团。
func orderHasSuccessfulGroup(tx *gorm.DB, orderID uint64) (bool, error) {
	var n int64
	// 多表 JOIN 必须带表前缀过滤 is_deleted，避免 MySQL ambiguous
	err := tx.Table("order_item oi").
		Joins("JOIN group_buy_team t ON t.id = oi.group_buy_team_id AND t.is_deleted = ?", model.NotDeleted).
		Where("oi.order_id = ? AND oi.is_deleted = ? AND t.status = ?", orderID, model.NotDeleted, model.GroupBuyTeamSuccess).
		Count(&n).Error
	return n > 0, err
}

// payTimeoutMinutes 返回待支付订单超时分钟数，未配置时用默认 5 分钟。
func (s *OrderService) payTimeoutMinutes() int {
	if s.PayTimeoutMinutes > 0 {
		return s.PayTimeoutMinutes
	}
	return 5
}

// ExpireStalePendingPayOrders 关闭超时未支付的订单：回滚库存/券/销量 + 退款 + 置 Closed。
// 含直购 PendingPay 与拼团 PendingGroup 未付款。
func (s *OrderService) ExpireStalePendingPayOrders(now time.Time) (int, error) {
	var orders []model.Order
	if err := query.NotDeleted(s.DB).
		Where("status IN ? AND pay_status = ? AND pay_expire_at IS NOT NULL AND pay_expire_at < ?",
			[]int{int(model.OrderStatusPendingPay), int(model.OrderStatusPendingGroup)},
			model.PayStatusUnpaid, now).
		Limit(100).
		Find(&orders).Error; err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for i := range orders {
		if err := s.expireOnePendingPayOrder(orders[i].ID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("expire pending-pay order %d: %w", orders[i].ID, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// expireOnePendingPayOrder 关闭单个待支付订单。关单前先查本地/微信侧是否已付：
// 已付则补入账+入团/履约，绝不关单（避免微信已扣款本地关成「已关闭」）。
func (s *OrderService) expireOnePendingPayOrder(orderID uint64) error {
	var snap model.Order
	if err := query.NotDeleted(s.DB).Select("id", "order_no", "status", "pay_status", "prepay_id").
		First(&snap, orderID).Error; err != nil {
		return err
	}
	if snap.Status != model.OrderStatusPendingPay && snap.Status != model.OrderStatusPendingGroup {
		return nil
	}
	if snap.PayStatus == model.PayStatusPaid {
		return s.DB.Transaction(func(tx *gorm.DB) error {
			return s.advanceAfterPaidInTx(tx, orderID)
		})
	}

	// 微信侧先查单 / 关单：ORDERPAID 或 SUCCESS 走补入账，不关本地单
	if wp, ok := s.Payment.(*payment.WeChatProvider); ok && wp.Client != nil {
		if settled, err := s.trySettleWechatPaidOnExpire(wp, snap); settled || err != nil {
			return err
		}
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&order, orderID).Error; err != nil {
			return err
		}
		if order.Status != model.OrderStatusPendingPay && order.Status != model.OrderStatusPendingGroup {
			return nil
		}
		if order.PayStatus == model.PayStatusPaid {
			return s.advanceAfterPaidInTx(tx, orderID)
		}
		isPackageParent := order.PackageProductID != nil && order.ParentOrderID == nil && order.MerchantID == 0
		if order.ParentOrderID != nil {
			return nil
		}
		if err := rollbackGroupTeamForOrder(tx, orderID); err != nil {
			return err
		}
		if s.CouponSvc != nil {
			if err := s.CouponSvc.ReleaseByOrderInTx(tx, &order); err != nil {
				return err
			}
		}
		if s.InventorySvc != nil {
			if err := s.InventorySvc.RollbackOrderCredit(tx, orderID); err != nil {
				return err
			}
		}
		if isPackageParent {
			if err := cancelPackageChildrenInTx(tx, orderID, s.InventorySvc, s.CouponSvc); err != nil {
				return err
			}
		} else if err := restoreProductStockForOrder(tx, orderID); err != nil {
			return err
		}
		if s.ActivitySvc != nil {
			if err := s.ActivitySvc.RollbackSoldInTx(tx, orderID); err != nil {
				return err
			}
		}
		return tx.Model(&order).Updates(map[string]interface{}{
			"status":        model.OrderStatusClosed,
			"pay_expire_at": nil,
		}).Error
	})
}

// trySettleWechatPaidOnExpire 查微信/关单探测是否已付。settled=true 表示已补入账（或无需再关）。
func (s *OrderService) trySettleWechatPaidOnExpire(wp *payment.WeChatProvider, order model.Order) (settled bool, err error) {
	tradeState, txID, qerr := wp.Client.QueryOrderByOutTradeNo(order.OrderNo)
	if qerr == nil && tradeState == "SUCCESS" {
		log.Printf("[pay-expire] order %s already paid on wechat, settle locally", order.OrderNo)
		return true, s.markPaidAndAdvanceFromExpire(order.ID, txID)
	}
	if qerr != nil {
		log.Printf("[pay-expire] query wechat order %s failed: %v", order.OrderNo, qerr)
	}

	closeErr := wp.Client.CloseOrder(wp.MchID, order.OrderNo)
	if closeErr == nil {
		return false, nil
	}
	if wechatv3.IsOrderPaid(closeErr) {
		log.Printf("[pay-expire] close %s got ORDERPAID, settle locally", order.OrderNo)
		if tradeState, txID, qerr = wp.Client.QueryOrderByOutTradeNo(order.OrderNo); qerr == nil && tradeState == "SUCCESS" {
			return true, s.markPaidAndAdvanceFromExpire(order.ID, txID)
		}
		// 查单失败仍按已付补入账，避免关本地未付单
		return true, s.markPaidAndAdvanceFromExpire(order.ID, "")
	}
	log.Printf("[pay-expire] close wechat order %s failed: %v", order.OrderNo, closeErr)
	return false, nil
}

// markPaidAndAdvanceFromExpire 超时任务发现微信已付时：标已付 + 推进/入团。
func (s *OrderService) markPaidAndAdvanceFromExpire(orderID uint64, transactionID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		res := query.NotDeleted(tx.Model(&model.Order{})).
			Where("id = ? AND pay_status = ?", orderID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    now,
				"prepay_id":  nil,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var o model.Order
			if err := query.NotDeleted(tx).Select("pay_status").First(&o, orderID).Error; err != nil {
				return err
			}
			if o.PayStatus != model.PayStatusPaid {
				return fmt.Errorf("order %d unexpected pay_status=%d on expire settle", orderID, o.PayStatus)
			}
		}
		if transactionID != "" {
			_ = tx.Model(&model.PaymentTransaction{}).Where("order_id = ?", orderID).
				Updates(map[string]interface{}{
					"status":         model.PayTxStatusPaid,
					"transaction_id": transactionID,
				})
		}
		return s.advanceAfterPaidInTx(tx, orderID)
	})
}
