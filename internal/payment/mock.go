package payment

import (
	"encoding/json"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MockProvider 模拟支付：下单事务内立即结算；取消/拒单事务内立即退款记账。
type MockProvider struct {
	DB *gorm.DB
}

func (p *MockProvider) Name() string          { return "mock" }
func (p *MockProvider) ImmediateSettle() bool { return true }

func (p *MockProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	sub, err := OrderSubjectFromID(tx, orderID, 0)
	if err != nil {
		return err
	}
	if payAmount >= 0 {
		sub.Amount = payAmount
	}
	return p.SettleSubjectPaidInTx(tx, sub, at)
}

func (p *MockProvider) SettleSubjectPaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	switch sub.Type {
	case model.PaySubjectOrder:
		return p.settleOrderPaidInTx(tx, sub.ID, at)
	case model.PaySubjectTakeout:
		return p.settleTakeoutPaidInTx(tx, sub.ID, at)
	case model.PaySubjectDeliveryFee:
		return p.settleDeliveryFeePaidInTx(tx, sub.ID, at)
	default:
		return ErrInvalidState
	}
}

func (p *MockProvider) settleOrderPaidInTx(tx *gorm.DB, orderID uint64, at time.Time) error {
	if orderID == 0 {
		return ErrInvalidState
	}
	res := query.NotDeleted(tx.Model(&model.Order{})).
		Where("id = ? AND pay_status = ?", orderID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"pay_status": model.PayStatusPaid,
			"paid_at":    at,
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
		return ErrInvalidState
	}
	return nil
}

func (p *MockProvider) settleTakeoutPaidInTx(tx *gorm.DB, takeoutID uint64, at time.Time) error {
	if takeoutID == 0 {
		return ErrInvalidState
	}
	res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
		Where("id = ? AND pay_status = ?", takeoutID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"pay_status": model.PayStatusPaid,
			"paid_at":    at,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Select("id", "pay_status").First(&to, takeoutID).Error; err != nil {
			return err
		}
		if to.PayStatus == model.PayStatusPaid {
			return nil
		}
		return ErrInvalidState
	}
	return nil
}

func (p *MockProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	return p.RefundAmountInTx(tx, orderID, 0, "全额退款")
}

func (p *MockProvider) RefundAmountInTx(tx *gorm.DB, orderID uint64, amount float64, reason string) error {
	sub, err := OrderSubjectFromID(tx, orderID, 0)
	if err != nil {
		return err
	}
	return p.RefundSubjectAmountInTx(tx, sub, amount, reason)
}

func (p *MockProvider) RefundSubjectAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	switch sub.Type {
	case model.PaySubjectOrder:
		return p.refundOrderAmountInTx(tx, sub.ID, amount, reason)
	case model.PaySubjectTakeout:
		return p.refundTakeoutAmountInTx(tx, sub.ID, amount, reason)
	case model.PaySubjectDeliveryFee:
		return p.refundDeliveryFeeAmountInTx(tx, sub.ID, amount, reason)
	default:
		return ErrInvalidState
	}
}

func (p *MockProvider) refundOrderAmountInTx(tx *gorm.DB, orderID uint64, amount float64, reason string) error {
	if orderID == 0 {
		return ErrInvalidState
	}
	_ = reason
	var o model.Order
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
		First(&o, orderID).Error; err != nil {
		return err
	}
	return p.applyRefundAmount(tx, o.PayStatus, o.PayAmount, o.RefundedAmount, o.RefundPendingAmount, amount, func(refund float64, status uint8, newRefunded float64) error {
		res := optimisticRefundWhere(
			query.NotDeleted(tx.Model(&model.Order{})),
			orderID, o.PayStatus, o.RefundedAmount, o.RefundPendingAmount,
		).Updates(map[string]interface{}{
			"pay_status":            status,
			"refunded_amount":       newRefunded,
			"refund_pending_amount": 0,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: order %d refund conflict", ErrInvalidState, orderID)
		}
		if status == model.PayStatusRefunded {
			_ = query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", orderID).
				Update("status", model.OrderStatusRefunded).Error
		}
		return nil
	})
}

func (p *MockProvider) refundTakeoutAmountInTx(tx *gorm.DB, takeoutID uint64, amount float64, reason string) error {
	if takeoutID == 0 {
		return ErrInvalidState
	}
	_ = reason
	var to model.TakeoutOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount").
		First(&to, takeoutID).Error; err != nil {
		return err
	}
	return p.applyRefundAmount(tx, to.PayStatus, to.PayAmount, to.RefundedAmount, 0, amount, func(refund float64, status uint8, newRefunded float64) error {
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND pay_status = ? AND refunded_amount = ?", takeoutID, to.PayStatus, to.RefundedAmount).
			Updates(map[string]interface{}{
				"pay_status":      status,
				"refunded_amount": newRefunded,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: takeout %d refund conflict", ErrInvalidState, takeoutID)
		}
		if status == model.PayStatusRefunded {
			_ = query.NotDeleted(tx.Model(&model.TakeoutOrder{})).Where("id = ?", takeoutID).
				Update("status", model.TakeoutStatusCancelled).Error
			detail, _ := json.Marshal(map[string]interface{}{"amount": refund})
			_ = tx.Create(&model.FulfillmentEvent{
				SubjectType: model.FulfillmentSubjectTakeout,
				SubjectID:   takeoutID,
				EventCode:   model.EventRefundSucceeded,
				ActorRole:   model.FulfillmentActorSystem,
				Title:       "退款已到账",
				Detail:      detail,
				CreatedAt:   time.Now(),
			}).Error
		}
		return nil
	})
}

type refundApplyFn func(refund float64, status uint8, newRefunded float64) error

func (p *MockProvider) applyRefundAmount(tx *gorm.DB, payStatus uint8, payAmount, refundedAmount, refundPendingAmount, amount float64, apply refundApplyFn) error {
	_ = tx
	switch payStatus {
	case model.PayStatusUnpaid:
		return nil
	case model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := payAmount - refundedAmount - refundPendingAmount
		if remain < 0 {
			remain = 0
		}
		// 零元已付单：本地记已退款（无实付可退）
		if payAmount <= 0.009 {
			return apply(0, model.PayStatusRefunded, 0)
		}
		refund := amount
		if refund <= 0 || refund >= remain {
			refund = remain
		}
		if refund <= 0 {
			if amount > 0 {
				return fmt.Errorf("%w: no refundable balance", ErrInvalidState)
			}
			return nil
		}
		refund = roundMoney(refund)
		newRefunded := roundMoney(refundedAmount + refund)
		status := model.PayStatusPartialRefunded
		if newRefunded+0.0001 >= payAmount {
			status = model.PayStatusRefunded
			newRefunded = roundMoney(payAmount)
		}
		return apply(refund, status, newRefunded)
	default:
		return ErrInvalidState
	}
}

func (p *MockProvider) CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error) {
	sub, err := OrderSubjectFromID(p.DB, orderID, accountID)
	if err != nil {
		return nil, err
	}
	return p.CreatePrepayForSubject(sub)
}

func (p *MockProvider) CreatePrepayForSubject(sub PaySubject) (*PrepayResult, error) {
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	switch sub.Type {
	case model.PaySubjectOrder:
		return p.createOrderPrepay(sub)
	case model.PaySubjectTakeout:
		return p.createTakeoutPrepay(sub)
	case model.PaySubjectDeliveryFee:
		return p.createDeliveryFeePrepay(sub)
	default:
		return nil, ErrInvalidState
	}
}

func (p *MockProvider) createOrderPrepay(sub PaySubject) (*PrepayResult, error) {
	var o model.Order
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", sub.ID, sub.AccountID).
		First(&o).Error; err != nil {
		return nil, err
	}
	if o.PayStatus == model.PayStatusPaid {
		return &PrepayResult{
			Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "模拟支付已结算",
		}, nil
	}
	if o.PayStatus != model.PayStatusUnpaid {
		return nil, ErrInvalidState
	}
	now := time.Now()
	if err := p.DB.Transaction(func(tx *gorm.DB) error {
		return p.settleOrderPaidInTx(tx, o.ID, now)
	}); err != nil {
		return nil, err
	}
	return &PrepayResult{
		Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
		Message: "模拟支付已结算",
	}, nil
}

func (p *MockProvider) createTakeoutPrepay(sub PaySubject) (*PrepayResult, error) {
	var to model.TakeoutOrder
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", sub.ID, sub.AccountID).
		First(&to).Error; err != nil {
		return nil, err
	}
	if to.PayStatus == model.PayStatusPaid {
		return &PrepayResult{
			Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "模拟支付已结算",
		}, nil
	}
	if to.PayStatus != model.PayStatusUnpaid {
		return nil, ErrInvalidState
	}
	now := time.Now()
	if err := p.DB.Transaction(func(tx *gorm.DB) error {
		return p.settleTakeoutPaidInTx(tx, to.ID, now)
	}); err != nil {
		return nil, err
	}
	return &PrepayResult{
		Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
		Message: "模拟支付已结算",
	}, nil
}

func (p *MockProvider) settleDeliveryFeePaidInTx(tx *gorm.DB, feeOrderID uint64, at time.Time) error {
	if feeOrderID == 0 {
		return ErrInvalidState
	}
	res := query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).
		Where("id = ? AND pay_status = ?", feeOrderID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"pay_status": model.PayStatusPaid,
			"paid_at":    at,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var fee model.DeliveryFeeOrder
		if err := query.NotDeleted(tx).Select("id", "pay_status").First(&fee, feeOrderID).Error; err != nil {
			return err
		}
		if fee.PayStatus == model.PayStatusPaid {
			return nil
		}
		return ErrInvalidState
	}
	return nil
}

func (p *MockProvider) refundDeliveryFeeAmountInTx(tx *gorm.DB, feeOrderID uint64, amount float64, reason string) error {
	if feeOrderID == 0 {
		return ErrInvalidState
	}
	_ = reason
	var fee model.DeliveryFeeOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount").
		First(&fee, feeOrderID).Error; err != nil {
		return err
	}
	return p.applyRefundAmount(tx, fee.PayStatus, fee.PayAmount, fee.RefundedAmount, 0, amount, func(refund float64, status uint8, newRefunded float64) error {
		res := query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).
			Where("id = ? AND pay_status = ? AND refunded_amount = ?", feeOrderID, fee.PayStatus, fee.RefundedAmount).
			Updates(map[string]interface{}{
				"pay_status":      status,
				"refunded_amount": newRefunded,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: delivery fee %d refund conflict", ErrInvalidState, feeOrderID)
		}
		return nil
	})
}

func (p *MockProvider) createDeliveryFeePrepay(sub PaySubject) (*PrepayResult, error) {
	var fee model.DeliveryFeeOrder
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", sub.ID, sub.AccountID).
		First(&fee).Error; err != nil {
		return nil, err
	}
	if fee.PayStatus == model.PayStatusPaid {
		return &PrepayResult{
			Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "模拟支付已结算",
		}, nil
	}
	if fee.PayStatus != model.PayStatusUnpaid {
		return nil, ErrInvalidState
	}
	now := time.Now()
	if err := p.DB.Transaction(func(tx *gorm.DB) error {
		return p.settleDeliveryFeePaidInTx(tx, fee.ID, now)
	}); err != nil {
		return nil, err
	}
	return &PrepayResult{
		Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
		Message: "模拟支付已结算",
	}, nil
}

func (p *MockProvider) HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error) {
	return nil, fmt.Errorf("%w: mock provider has no async notify", ErrNotSupported)
}
