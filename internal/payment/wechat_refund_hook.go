package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment/wechatv3"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const wechatRefundCollectorKey = "wechat_refund_collector"

type refundJobsCtxKey struct{}

// RefundJob 事务提交后待派发的微信退款任务。
type RefundJob struct {
	Provider    *WeChatProvider
	SubjectType string // model.PaySubject*；空则按入包订单处理
	SubjectID   uint64 // 支付主体 ID（外卖单等与 OrderID 可能不同）
	OrderID     uint64 // 入包订单 ID；背包回滚等 legacy 路径使用
	OrderNo     string
	OutRefundNo string
	PayAmount   float64
	RefundAmt   float64
	Reason      string
	// CreateRefund 失败时回滚事务内已扣的背包
	RestoreAccountID uint64
	RestoreProductID uint64
	RestoreSpec      string
	RestoreQty       uint32
}

// AttachRefundCollector 在事务开始时绑定退款任务收集器；Commit 成功后由 DispatchRefundJobs 派发。
// 同时写入 Context（克隆 session 可继承）与 InstanceSet（原 tx 指针可取）。
func AttachRefundCollector(tx *gorm.DB, jobs *[]RefundJob) {
	if tx == nil || jobs == nil {
		return
	}
	if tx.Statement != nil {
		ctx := tx.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		tx.Statement.Context = context.WithValue(ctx, refundJobsCtxKey{}, jobs)
	}
	tx.InstanceSet(wechatRefundCollectorKey, jobs)
}

func refundJobsFromTx(tx *gorm.DB) *[]RefundJob {
	if tx == nil {
		return nil
	}
	if tx.Statement != nil && tx.Statement.Context != nil {
		if v := tx.Statement.Context.Value(refundJobsCtxKey{}); v != nil {
			if ptr, ok := v.(*[]RefundJob); ok && ptr != nil {
				return ptr
			}
		}
	}
	if val, ok := tx.InstanceGet(wechatRefundCollectorKey); ok {
		if ptr, ok := val.(*[]RefundJob); ok && ptr != nil {
			return ptr
		}
	}
	return nil
}

func enqueueWeChatRefund(tx *gorm.DB, job RefundJob) error {
	ptr := refundJobsFromTx(tx)
	if ptr == nil {
		return fmt.Errorf("%w: refund collector missing (call AttachRefundCollector)", ErrInvalidState)
	}
	*ptr = append(*ptr, job)
	return nil
}

// DispatchRefundJobs 仅在事务成功提交后调用，发起微信退款。
func DispatchRefundJobs(jobs []RefundJob) {
	for i := range jobs {
		job := jobs[i]
		go dispatchWeChatRefund(job)
	}
}

func dispatchWeChatRefund(job RefundJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[wechat refund] panic recovered: %v", r)
			releaseRefundPending(job, "微信退款调用异常")
			restoreInventoryAfterRefundFail(job)
		}
	}()
	if job.Provider == nil || job.Provider.Client == nil {
		releaseRefundPending(job, "退款服务未配置")
		restoreInventoryAfterRefundFail(job)
		return
	}
	_, err := job.Provider.Client.CreateRefund(&wechatv3.CreateRefundRequest{
		OutTradeNo:  job.OrderNo,
		OutRefundNo: job.OutRefundNo,
		Reason:      truncateRunes(job.Reason, 80),
		NotifyURL:   job.Provider.NotifyURL,
		Amount: wechatv3.RefundAmount{
			Refund:   wechatv3.YuanToFen(job.RefundAmt),
			Total:    wechatv3.YuanToFen(job.PayAmount),
			Currency: "CNY",
		},
	})
	if err != nil {
		log.Printf("[wechat refund] order %s refund failed: %v", job.OrderNo, err)
		releaseRefundPending(job, truncateRunes(err.Error(), 180))
		restoreInventoryAfterRefundFail(job)
		job.Provider.alertRefundFailure(job.OrderNo, err.Error())
		return
	}
}

// alertRefundFailure 退款发起失败时推送告警（配置 REFUND_ALERT_WEBHOOK_URL 时）。
// 请求体同时带 text 与标准字段，兼容钉钉/企业微信/飞书自定义机器人及通用 webhook。
func (p *WeChatProvider) alertRefundFailure(orderNo, errMsg string) {
	if p == nil || p.AlertWebhookURL == "" {
		return
	}
	text := fmt.Sprintf("【雨季新江】退款发起失败：订单 %s（%s），已进入自动重试，超出次数请到管理端处理", orderNo, errMsg)
	payload := map[string]interface{}{
		"event":    "refund_failed",
		"order_no": orderNo,
		"error":    errMsg,
		"text":     text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	go func() {
		req, reqErr := http.NewRequest(http.MethodPost, p.AlertWebhookURL, bytes.NewReader(body))
		if reqErr != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			log.Printf("[wechat refund] 告警推送失败 order %s: %v", orderNo, doErr)
			return
		}
		_ = resp.Body.Close()
	}()
}

// AttachRestoreToLastRefundJob 微信 CreateRefund 失败时回滚已扣背包。
func AttachRestoreToLastRefundJob(tx *gorm.DB, accountID, productID uint64, spec string, qty uint32) {
	ptr := refundJobsFromTx(tx)
	if ptr == nil || len(*ptr) == 0 || qty == 0 {
		return
	}
	jobs := *ptr
	last := &jobs[len(jobs)-1]
	last.RestoreAccountID = accountID
	last.RestoreProductID = productID
	last.RestoreSpec = spec
	last.RestoreQty = qty
}

func restoreInventoryAfterRefundFail(job RefundJob) {
	if job.Provider == nil || job.Provider.DB == nil || job.RestoreAccountID == 0 || job.RestoreProductID == 0 || job.RestoreQty == 0 {
		return
	}
	remark := "微信退款发起失败，回滚背包"
	err := job.Provider.DB.Transaction(func(tx *gorm.DB) error {
		var inv model.UserInventory
		err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("account_id = ? AND product_id = ? AND spec = ?",
				job.RestoreAccountID, job.RestoreProductID, job.RestoreSpec).
			First(&inv).Error
		before := uint32(0)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			inv = model.UserInventory{
				AccountID: job.RestoreAccountID,
				ProductID: job.RestoreProductID,
				Spec:      job.RestoreSpec,
				Quantity:  job.RestoreQty,
			}
			if job.OrderID > 0 {
				oid := job.OrderID
				inv.LastOrderID = &oid
			}
			if err := tx.Create(&inv).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			before = inv.Quantity
			if err := tx.Model(&inv).Update("quantity", before+job.RestoreQty).Error; err != nil {
				return err
			}
			inv.Quantity = before + job.RestoreQty
		}
		oid := job.OrderID
		logRow := model.UserInventoryLog{
			AccountID: job.RestoreAccountID, InventoryID: &inv.ID, ProductID: job.RestoreProductID,
			Spec: job.RestoreSpec, OrderID: &oid, EventType: model.InventoryEventOrderCredit,
			DeltaQty: int32(job.RestoreQty), BeforeQty: before, AfterQty: inv.Quantity, Remark: &remark,
		}
		if err := tx.Create(&logRow).Error; err != nil {
			return err
		}
		// 冲回退款时加回的商品库存
		return tx.Model(&model.Product{}).Where("id = ?", job.RestoreProductID).
			Update("stock", gorm.Expr("CASE WHEN stock >= ? THEN stock - ? ELSE 0 END", job.RestoreQty, job.RestoreQty)).Error
	})
	if err != nil {
		log.Printf("[wechat refund] restore inventory order %d product %d failed: %v",
			job.OrderID, job.RestoreProductID, err)
	}
}

// releaseRefundPending 微信退款发起失败时释放预留，避免卡死可退余额；并标记退款失败待管理端重试。
func releaseRefundPending(job RefundJob, reason string) {
	if job.Provider == nil || job.Provider.DB == nil {
		return
	}
	switch job.SubjectType {
	case model.PaySubjectTakeout:
		releaseTakeoutRefundPending(job.Provider, job.SubjectID)
		return
	case model.PaySubjectDeliveryFee:
		releaseDeliveryFeeRefundPending(job.Provider, job.SubjectID)
		return
	case model.PaySubjectErrand:
		releaseErrandRefundPending(job.Provider, job.SubjectID)
		return
	default:
		releaseOrderRefundPending(job.Provider, job.OrderID, job.RefundAmt, reason)
	}
}

func releaseErrandRefundPending(p *WeChatProvider, errandID uint64) {
	if p == nil || p.DB == nil || errandID == 0 {
		return
	}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx).Select("id", "pay_status", "refunded_amount").
			First(&eo, errandID).Error; err != nil {
			return err
		}
		if eo.PayStatus != model.PayStatusRefunding {
			return nil
		}
		status := model.PayStatusPaid
		if eo.RefundedAmount > 0 {
			status = model.PayStatusPartialRefunded
		}
		return query.NotDeleted(tx.Model(&model.ErrandOrder{})).Where("id = ?", errandID).
			Updates(map[string]interface{}{"pay_status": status}).Error
	})
	if err != nil {
		log.Printf("[wechat refund] release errand pending %d failed: %v", errandID, err)
	}
}

func releaseTakeoutRefundPending(p *WeChatProvider, takeoutID uint64) {
	if p == nil || p.DB == nil || takeoutID == 0 {
		return
	}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		var takeout model.TakeoutOrder
		if err := query.NotDeleted(tx).Select("id", "pay_status", "refunded_amount").
			First(&takeout, takeoutID).Error; err != nil {
			return err
		}
		if takeout.PayStatus != model.PayStatusRefunding {
			return nil
		}
		status := model.PayStatusPaid
		if takeout.RefundedAmount > 0 {
			status = model.PayStatusPartialRefunded
		}
		return query.NotDeleted(tx.Model(&model.TakeoutOrder{})).Where("id = ?", takeoutID).
			Updates(map[string]interface{}{"pay_status": status}).Error
	})
	if err != nil {
		log.Printf("[wechat refund] release takeout pending %d failed: %v", takeoutID, err)
	}
}

func releaseDeliveryFeeRefundPending(p *WeChatProvider, feeOrderID uint64) {
	if p == nil || p.DB == nil || feeOrderID == 0 {
		return
	}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		var fee model.DeliveryFeeOrder
		if err := query.NotDeleted(tx).Select("id", "pay_status", "refunded_amount").
			First(&fee, feeOrderID).Error; err != nil {
			return err
		}
		if fee.PayStatus != model.PayStatusRefunding {
			return nil
		}
		status := model.PayStatusPaid
		if fee.RefundedAmount > 0 {
			status = model.PayStatusPartialRefunded
		}
		return query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).Where("id = ?", feeOrderID).
			Updates(map[string]interface{}{"pay_status": status}).Error
	})
	if err != nil {
		log.Printf("[wechat refund] release delivery fee pending %d failed: %v", feeOrderID, err)
	}
}

func releaseOrderRefundPending(p *WeChatProvider, orderID uint64, amount float64, reason string) {
	if p == nil || p.DB == nil || orderID == 0 || amount <= 0 {
		return
	}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := query.NotDeleted(tx).Select("id", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
			First(&order, orderID).Error; err != nil {
			return err
		}
		pending := roundMoney(order.RefundPendingAmount - amount)
		if pending < 0 {
			pending = 0
		}
		status := order.PayStatus
		orderStatus := (*uint8)(nil)
		markFailed := false
		switch {
		case order.RefundedAmount+0.0001 >= order.PayAmount:
			status = model.PayStatusRefunded
		case pending > 0:
			status = model.PayStatusRefunding
		case order.RefundedAmount > 0:
			status = model.PayStatusPartialRefunded
		default:
			status = model.PayStatusPaid
			// 微信发起失败且尚无已退金额：履约态从「退款中」恢复为待履约（已入背包场景）
			s := model.OrderStatusPendingFulfill
			orderStatus = &s
			// 标记退款失败，供管理端重试
			markFailed = true
		}
		updates := map[string]interface{}{
			"refund_pending_amount": pending,
			"pay_status":            status,
		}
		if orderStatus != nil {
			updates["status"] = *orderStatus
		}
		if markFailed {
			updates["refund_failed"] = 1
			updates["refund_fail_reason"] = reason
			updates["refund_failed_at"] = time.Now()
		}
		return query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", orderID).Updates(updates).Error
	})
	if err != nil {
		log.Printf("[wechat refund] release pending order %d failed: %v", orderID, err)
	}
}

func refundableRemain(o model.Order) float64 {
	remain := roundMoney(o.PayAmount - o.RefundedAmount - o.RefundPendingAmount)
	if remain < 0 {
		return 0
	}
	return remain
}

func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

func moneyFen(v float64) int64 {
	return int64(math.Round(v * 100))
}

// optimisticRefundWhere 用「分」比较金额，避免 DECIMAL 与 float64 直接相等导致 RowsAffected=0。
func optimisticRefundWhere(db *gorm.DB, orderID uint64, payStatus uint8, refunded, pending float64) *gorm.DB {
	return db.Where("id = ? AND pay_status = ?", orderID, payStatus).
		Where("ROUND(refunded_amount * 100) = ? AND ROUND(refund_pending_amount * 100) = ?",
			moneyFen(refunded), moneyFen(pending))
}

func fmtRefundConflict(orderID uint64) error {
	return fmt.Errorf("%w: order %d refund conflict", ErrInvalidState, orderID)
}
