package payment

import (
	"fmt"
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// PaySubject 支付主体：入包订单、外卖单、跑腿配送费等。
type PaySubject struct {
	Type      string // model.PaySubject*
	ID        uint64
	OrderNo   string
	Amount    float64
	AccountID uint64
}

func (s PaySubject) Validate() error {
	if s.Type == "" || s.ID == 0 || s.OrderNo == "" || s.AccountID == 0 {
		return ErrInvalidState
	}
	if s.Amount < 0 {
		return fmt.Errorf("%w: amount invalid", ErrInvalidState)
	}
	return nil
}

// GoodsOrderNoSuffix 跑腿代购商品费支付单后缀（同一跑腿单第二次支付时使用）。
const GoodsOrderNoSuffix = "G"

// GoodsOrderNo 生成跑腿商品费支付单号（在跑腿订单号后加后缀）。
func GoodsOrderNo(orderNo string) string { return orderNo + GoodsOrderNoSuffix }

// IsGoodsOrderNo 判断是否为跑腿商品费支付单号。
func IsGoodsOrderNo(orderNo string) bool { return strings.HasSuffix(orderNo, GoodsOrderNoSuffix) }

// SubjectTypeFromOrderNo 按业务单号前缀推断支付主体类型。
func SubjectTypeFromOrderNo(orderNo string) string {
	switch {
	case strings.HasPrefix(orderNo, "T"):
		return model.PaySubjectTakeout
	case strings.HasPrefix(orderNo, "F"):
		return model.PaySubjectDeliveryFee
	case strings.HasPrefix(orderNo, "P"):
		return model.PaySubjectErrand
	default:
		return model.PaySubjectOrder
	}
}

// OrderSubjectFromID 加载入包订单并构造 PaySubject。
func OrderSubjectFromID(db *gorm.DB, orderID, accountID uint64) (PaySubject, error) {
	var o model.Order
	q := query.NotDeleted(db).Where("id = ?", orderID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&o).Error; err != nil {
		return PaySubject{}, err
	}
	return PaySubject{
		Type:      model.PaySubjectOrder,
		ID:        o.ID,
		OrderNo:   o.OrderNo,
		Amount:    o.PayAmount,
		AccountID: o.AccountID,
	}, nil
}

// TakeoutSubjectFromID 加载外卖单并构造 PaySubject。
func TakeoutSubjectFromID(db *gorm.DB, takeoutID, accountID uint64) (PaySubject, error) {
	var to model.TakeoutOrder
	q := query.NotDeleted(db).Where("id = ?", takeoutID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&to).Error; err != nil {
		return PaySubject{}, err
	}
	return PaySubject{
		Type:      model.PaySubjectTakeout,
		ID:        to.ID,
		OrderNo:   to.OrderNo,
		Amount:    to.PayAmount,
		AccountID: to.AccountID,
	}, nil
}

// DeliveryFeeSubjectFromID 加载跑腿配送费单并构造 PaySubject。
func DeliveryFeeSubjectFromID(db *gorm.DB, feeOrderID, accountID uint64) (PaySubject, error) {
	var fee model.DeliveryFeeOrder
	q := query.NotDeleted(db).Where("id = ?", feeOrderID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&fee).Error; err != nil {
		return PaySubject{}, err
	}
	return PaySubject{
		Type:      model.PaySubjectDeliveryFee,
		ID:        fee.ID,
		OrderNo:   fee.OrderNo,
		Amount:    fee.PayAmount,
		AccountID: fee.AccountID,
	}, nil
}

// ErrandSubjectFromID 加载跑腿单并构造 PaySubject。
func ErrandSubjectFromID(db *gorm.DB, errandID, accountID uint64) (PaySubject, error) {
	var eo model.ErrandOrder
	q := query.NotDeleted(db).Where("id = ?", errandID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&eo).Error; err != nil {
		return PaySubject{}, err
	}
	return PaySubject{
		Type:      model.PaySubjectErrand,
		ID:        eo.ID,
		OrderNo:   eo.OrderNo,
		Amount:    eo.PayAmount,
		AccountID: eo.AccountID,
	}, nil
}

// ResolveSubjectByOrderNo 用 out_trade_no 反查支付主体（微信回调/查单）。
func ResolveSubjectByOrderNo(db *gorm.DB, orderNo string) (PaySubject, error) {
	if orderNo == "" {
		return PaySubject{}, ErrInvalidState
	}
	subType := SubjectTypeFromOrderNo(orderNo)
	switch subType {
	case model.PaySubjectErrand:
		// 商品费支付单号为「跑腿订单号 + G」，回查时需按基础单号匹配并按商品费金额校验。
		baseNo := orderNo
		isGoods := IsGoodsOrderNo(orderNo)
		if isGoods {
			baseNo = strings.TrimSuffix(orderNo, GoodsOrderNoSuffix)
		}
		var eo model.ErrandOrder
		if err := query.NotDeleted(db).Where("order_no = ?", baseNo).First(&eo).Error; err != nil {
			return PaySubject{}, err
		}
		amount := eo.PayAmount
		if isGoods {
			amount = eo.GoodsAmount
		}
		return PaySubject{
			Type:      model.PaySubjectErrand,
			ID:        eo.ID,
			OrderNo:   orderNo,
			Amount:    amount,
			AccountID: eo.AccountID,
		}, nil
	case model.PaySubjectTakeout:
		var to model.TakeoutOrder
		if err := query.NotDeleted(db).Where("order_no = ?", orderNo).First(&to).Error; err != nil {
			return PaySubject{}, err
		}
		return PaySubject{
			Type:      model.PaySubjectTakeout,
			ID:        to.ID,
			OrderNo:   to.OrderNo,
			Amount:    to.PayAmount,
			AccountID: to.AccountID,
		}, nil
	case model.PaySubjectDeliveryFee:
		var fee model.DeliveryFeeOrder
		if err := query.NotDeleted(db).Where("order_no = ?", orderNo).First(&fee).Error; err != nil {
			return PaySubject{}, err
		}
		return PaySubject{
			Type:      model.PaySubjectDeliveryFee,
			ID:        fee.ID,
			OrderNo:   fee.OrderNo,
			Amount:    fee.PayAmount,
			AccountID: fee.AccountID,
		}, nil
	default:
		var o model.Order
		if err := query.NotDeleted(db).Where("order_no = ?", orderNo).First(&o).Error; err != nil {
			return PaySubject{}, err
		}
		return PaySubject{
			Type:      model.PaySubjectOrder,
			ID:        o.ID,
			OrderNo:   o.OrderNo,
			Amount:    o.PayAmount,
			AccountID: o.AccountID,
		}, nil
	}
}

// paymentTransactionOrderID 写入 payment_transaction.order_id。
// 入包订单写真实 ID；外卖/配送费写 0（依赖库侧已去掉 fk_pt_order）。
func paymentTransactionOrderID(sub PaySubject) uint64 {
	if sub.Type == model.PaySubjectOrder {
		return sub.ID
	}
	return 0
}

// paidTransactionBySubject scopes a query to a paid transaction for the subject,
// including legacy rows where subject_id=0 but order_id matches (pre-migration orders).
func paidTransactionBySubject(db *gorm.DB, subjectType string, subjectID uint64) *gorm.DB {
	legacyOrderID := uint64(0)
	if subjectType == model.PaySubjectOrder {
		legacyOrderID = subjectID
	}
	return db.Where("status = ?", model.PayTxStatusPaid).
		Where("(subject_type = ? AND subject_id = ?) OR (order_id = ? AND subject_id = 0)",
			subjectType, subjectID, legacyOrderID)
}
