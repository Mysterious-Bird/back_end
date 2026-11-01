package payment

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment/wechatv3"
	"yujixinjiang/backend/internal/query"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WeChatProvider 微信支付 V3 实现。
type WeChatProvider struct {
	DB        *gorm.DB
	AppID     string
	MchID     string
	APIKey    string
	NotifyURL string
	Enabled   bool
	Client    *wechatv3.Client // V3 API 客户端
	// 退款发起失败告警 webhook（可选，钉钉/企业微信/飞书机器人）
	AlertWebhookURL string
	// OnPaidInTx 支付成功后推进入包订单状态（含自动审核入背包）。由 OrderService 注入。
	OnPaidInTx func(tx *gorm.DB, orderID uint64) error
	// OnSubjectPaidInTx 支付成功后推进业务状态（外卖/跑腿等）。由对应 Service 注入。
	OnSubjectPaidInTx func(tx *gorm.DB, sub PaySubject) error
}

func (p *WeChatProvider) Name() string          { return "wechat" }
func (p *WeChatProvider) ImmediateSettle() bool { return false }

// SettlePaidInTx 微信渠道禁止业务层直接"记已付"，必须经回调/查单确认。
func (p *WeChatProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", ErrNotSupported)
}

// SettleSubjectPaidInTx 微信渠道禁止业务层直接"记已付"，必须经回调/查单确认。
func (p *WeChatProvider) SettleSubjectPaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", ErrNotSupported)
}

// CreatePrepay 发起 JSAPI 预支付，返回 wx.requestPayment 所需参数（入包订单包装器）。
func (p *WeChatProvider) CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error) {
	sub, err := OrderSubjectFromID(p.DB, orderID, accountID)
	if err != nil {
		return nil, err
	}
	return p.CreatePrepayForSubject(sub)
}

// CreatePrepayForSubject 为支付主体发起 JSAPI 预支付。
func (p *WeChatProvider) CreatePrepayForSubject(sub PaySubject) (*PrepayResult, error) {
	if !p.Enabled || p.Client == nil {
		return nil, ErrNotConfigured
	}
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
	case model.PaySubjectErrand:
		return p.createErrandPrepay(sub)
	default:
		return nil, ErrInvalidState
	}
}

// createErrandPrepay 跑腿单平台支付：
// 待支付状态下支付跑腿费；已接单/配送中且为代购单时支付商品费（支付单号加 G 后缀）。
func (p *WeChatProvider) createErrandPrepay(sub PaySubject) (*PrepayResult, error) {
	var eo model.ErrandOrder
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", sub.ID, sub.AccountID).
		First(&eo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 跑腿订单不存在", ErrNotSupported)
		}
		return nil, err
	}
	alreadyPaid := &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false, Message: "订单已支付"}
	description := "雨季新江跑腿费"
	if IsGoodsOrderNo(sub.OrderNo) {
		// 代购商品费：仅已接单/配送中可付
		if eo.Status != model.ErrandStatusAccepted && eo.Status != model.ErrandStatusDelivering {
			return nil, ErrInvalidState
		}
		if eo.GoodsAmount <= 0 {
			return nil, ErrInvalidState
		}
		if eo.GoodsPayStatus == model.PayStatusPaid {
			return alreadyPaid, nil
		}
		if eo.GoodsPayStatus != model.PayStatusUnpaid {
			return nil, ErrInvalidState
		}
		description = "雨季新江跑腿代购"
	} else {
		// 跑腿费：仅待支付状态可付
		if eo.Status != model.ErrandStatusPendingPay {
			return nil, ErrInvalidState
		}
		if eo.PayStatus != model.PayStatusUnpaid {
			return alreadyPaid, nil
		}
		if eo.PayExpireAt != nil && time.Now().After(*eo.PayExpireAt) {
			return nil, fmt.Errorf("%w: 支付已超时，请重新下单", ErrInvalidState)
		}
	}
	amount := roundMoney(sub.Amount)
	if amount <= 0 {
		return nil, ErrInvalidState
	}

	var existingTx model.PaymentTransaction
	if err := p.DB.
		Where("subject_type = ? AND subject_id = ? AND order_no = ? AND status IN ?",
			model.PaySubjectErrand, eo.ID, sub.OrderNo,
			[]uint8{model.PayTxStatusPaid, model.PayTxStatusRefunded}).
		First(&existingTx).Error; err == nil {
		return alreadyPaid, nil
	}
	if reused, err := p.reuseOpenPrepay(model.PaySubjectErrand, eo.ID, sub.OrderNo, eo.PayExpireAt); err != nil {
		return nil, err
	} else if reused != nil {
		return reused, nil
	}

	var account model.Account
	if err := query.NotDeleted(p.DB).Select("id", "openid").First(&account, sub.AccountID).Error; err != nil {
		return nil, fmt.Errorf("获取用户 openid 失败: %w", err)
	}
	if account.OpenID == nil || *account.OpenID == "" {
		return nil, fmt.Errorf("用户未绑定微信 openid")
	}

	expireTime := ""
	if eo.PayExpireAt != nil {
		expireTime = eo.PayExpireAt.Format(time.RFC3339)
	}
	prepayResp, err := p.Client.CreateJSAPIPrepay(&wechatv3.CreateJSAPIPrepayRequest{
		AppID:       p.AppID,
		MchID:       p.MchID,
		Description: description,
		OutTradeNo:  sub.OrderNo,
		NotifyURL:   p.NotifyURL,
		Amount: wechatv3.PrepayAmount{
			Total:    wechatv3.YuanToFen(amount),
			Currency: "CNY",
		},
		Payer: wechatv3.PrepayPayer{
			OpenID: *account.OpenID,
		},
		TimeExpire: expireTime,
	})
	if err != nil {
		return nil, fmt.Errorf("微信下单失败: %w", err)
	}

	prepayID := prepayResp.PrepayID
	pt := model.PaymentTransaction{
		SubjectType: model.PaySubjectErrand,
		SubjectID:   eo.ID,
		OrderID:     0,
		OrderNo:     sub.OrderNo,
		PrepayID:    &prepayID,
		PayAmount:   amount,
		Status:      model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		if isDuplicateKey(err) {
			return p.CreatePrepayForSubject(sub)
		}
		return nil, err
	}
	params, err := p.Client.Signer().SignPrepay(p.AppID, prepayID)
	if err != nil {
		return nil, fmt.Errorf("生成支付签名失败: %w", err)
	}
	return &PrepayResult{
		Provider: p.Name(),
		NeedPay:  true,
		Params:   params,
		Message:  "请调起微信支付",
	}, nil
}

func (p *WeChatProvider) createOrderPrepay(sub PaySubject) (*PrepayResult, error) {
	orderID := sub.ID
	accountID := sub.AccountID

	// 1. 查订单，校验归属和状态
	var order model.Order
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", orderID, accountID).
		First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 订单不存在", ErrNotSupported)
		}
		return nil, err
	}
	// 直购待支付 / 拼团待成团且未付均可发起预支付
	switch order.Status {
	case model.OrderStatusPendingPay, model.OrderStatusPendingGroup:
	default:
		return nil, ErrInvalidState
	}
	if order.PayStatus != model.PayStatusUnpaid {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	// 零元单：微信 JSAPI 要求金额 > 0，本地免支付并推进履约（兼容历史未结清的 0 元待支付单）
	if order.PayAmount <= 0.009 {
		at := time.Now()
		err := p.DB.Transaction(func(tx *gorm.DB) error {
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
			if p.OnPaidInTx != nil {
				return p.OnPaidInTx(tx, orderID)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("零元订单免支付失败: %w", err)
		}
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "零元订单已免支付"}, nil
	}

	// 2. 幂等：已有成功流水则直接返回（含迁移前 subject_id=0 的 legacy 行）
	var existingTx model.PaymentTransaction
	err := paidTransactionBySubject(p.DB, model.PaySubjectOrder, orderID).
		First(&existingTx).Error
	if err == nil {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	// 2b. 已有预支付流水则复用（避免重复调微信 / prepay_id 唯一冲突）
	// 支付窗已过则不复用过期 prepay_id，避免客户端拿到失效签名。
	if reused, err := p.reuseOpenPrepay(model.PaySubjectOrder, orderID, order.OrderNo, order.PayExpireAt); err != nil {
		return nil, err
	} else if reused != nil {
		return reused, nil
	}
	if order.PayExpireAt != nil && !order.PayExpireAt.After(time.Now()) {
		return nil, fmt.Errorf("%w: 支付已超时，请重新下单", ErrInvalidState)
	}

	// 3. 获取用户 openid
	var account model.Account
	if err := query.NotDeleted(p.DB).Select("id", "openid").First(&account, accountID).Error; err != nil {
		return nil, fmt.Errorf("获取用户 openid 失败: %w", err)
	}
	if account.OpenID == nil || *account.OpenID == "" {
		return nil, fmt.Errorf("用户未绑定微信 openid")
	}

	// 4. 取首个商品名作为支付描述（限 127 字节）
	desc := "雨季新江商品"
	var item model.OrderItem
	if err := query.NotDeleted(p.DB).Where("order_id = ?", orderID).First(&item).Error; err == nil {
		if len(item.ProductName) > 0 {
			desc = truncateRunes(item.ProductName, 40)
		}
	}

	// 5. 调用微信 JSAPI 统一下单
	expireTime := ""
	if order.PayExpireAt != nil {
		expireTime = order.PayExpireAt.Format(time.RFC3339)
	}
	prepayResp, err := p.Client.CreateJSAPIPrepay(&wechatv3.CreateJSAPIPrepayRequest{
		AppID:       p.AppID,
		MchID:       p.MchID,
		Description: desc,
		OutTradeNo:  order.OrderNo,
		NotifyURL:   p.NotifyURL,
		Amount: wechatv3.PrepayAmount{
			Total:    wechatv3.YuanToFen(order.PayAmount),
			Currency: "CNY",
		},
		Payer: wechatv3.PrepayPayer{
			OpenID: *account.OpenID,
		},
		TimeExpire: expireTime,
	})
	if err != nil {
		return nil, fmt.Errorf("微信下单失败: %w", err)
	}

	// 6. 保存支付流水
	prepayID := prepayResp.PrepayID
	pt := model.PaymentTransaction{
		SubjectType: model.PaySubjectOrder,
		SubjectID:   orderID,
		OrderID:     orderID,
		OrderNo:     order.OrderNo,
		PrepayID:    &prepayID,
		PayAmount:   order.PayAmount,
		Status:      model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		// 唯一索引冲突：同 prepay_id 已存在，复用签名即可
		if isDuplicateKey(err) {
			return p.prepayResultFromID(prepayID)
		}
		return nil, err
	}

	// 7. 更新订单 prepay_id
	_ = p.DB.Model(&order).Update("prepay_id", prepayID).Error

	return p.prepayResultFromID(prepayID)
}

func (p *WeChatProvider) createTakeoutPrepay(sub PaySubject) (*PrepayResult, error) {
	takeoutID := sub.ID
	accountID := sub.AccountID

	var takeout model.TakeoutOrder
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", takeoutID, accountID).
		First(&takeout).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 外卖订单不存在", ErrNotSupported)
		}
		return nil, err
	}
	if takeout.Status != model.TakeoutStatusPendingPay {
		return nil, ErrInvalidState
	}
	if takeout.PayStatus != model.PayStatusUnpaid {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	var existingTx model.PaymentTransaction
	err := paidTransactionBySubject(p.DB, model.PaySubjectTakeout, takeoutID).
		First(&existingTx).Error
	if err == nil {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	var account model.Account
	if err := query.NotDeleted(p.DB).Select("id", "openid").First(&account, accountID).Error; err != nil {
		return nil, fmt.Errorf("获取用户 openid 失败: %w", err)
	}
	if account.OpenID == nil || *account.OpenID == "" {
		return nil, fmt.Errorf("用户未绑定微信 openid")
	}

	desc := "雨季新江外卖"
	var item model.TakeoutOrderItem
	if err := query.NotDeleted(p.DB).Where("takeout_order_id = ?", takeoutID).First(&item).Error; err == nil {
		if len(item.ProductName) > 0 {
			desc = truncateRunes(item.ProductName, 40)
		}
	}

	expireTime := ""
	if takeout.PayExpireAt != nil {
		expireTime = takeout.PayExpireAt.Format(time.RFC3339)
	}
	prepayResp, err := p.Client.CreateJSAPIPrepay(&wechatv3.CreateJSAPIPrepayRequest{
		AppID:       p.AppID,
		MchID:       p.MchID,
		Description: desc,
		OutTradeNo:  takeout.OrderNo,
		NotifyURL:   p.NotifyURL,
		Amount: wechatv3.PrepayAmount{
			Total:    wechatv3.YuanToFen(takeout.PayAmount),
			Currency: "CNY",
		},
		Payer: wechatv3.PrepayPayer{
			OpenID: *account.OpenID,
		},
		TimeExpire: expireTime,
	})
	if err != nil {
		return nil, fmt.Errorf("微信下单失败: %w", err)
	}

	prepayID := prepayResp.PrepayID
	pt := model.PaymentTransaction{
		SubjectType: model.PaySubjectTakeout,
		SubjectID:   takeoutID,
		OrderID:     0,
		OrderNo:     takeout.OrderNo,
		PrepayID:    &prepayID,
		PayAmount:   takeout.PayAmount,
		Status:      model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		if isDuplicateKey(err) {
			return p.CreatePrepayForSubject(sub)
		}
		return nil, err
	}

	params, err := p.Client.Signer().SignPrepay(p.AppID, prepayID)
	if err != nil {
		return nil, fmt.Errorf("生成支付签名失败: %w", err)
	}

	return &PrepayResult{
		Provider: p.Name(),
		NeedPay:  true,
		Params:   params,
		Message:  "请调起微信支付",
	}, nil
}

func (p *WeChatProvider) createDeliveryFeePrepay(sub PaySubject) (*PrepayResult, error) {
	feeOrderID := sub.ID
	accountID := sub.AccountID

	var feeOrder model.DeliveryFeeOrder
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", feeOrderID, accountID).
		First(&feeOrder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 配送费订单不存在", ErrNotSupported)
		}
		return nil, err
	}
	if feeOrder.Status != model.DeliveryFeeStatusPendingPay {
		return nil, ErrInvalidState
	}
	if feeOrder.PayStatus != model.PayStatusUnpaid {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	var existingTx model.PaymentTransaction
	err := paidTransactionBySubject(p.DB, model.PaySubjectDeliveryFee, feeOrderID).
		First(&existingTx).Error
	if err == nil {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	var account model.Account
	if err := query.NotDeleted(p.DB).Select("id", "openid").First(&account, accountID).Error; err != nil {
		return nil, fmt.Errorf("获取用户 openid 失败: %w", err)
	}
	if account.OpenID == nil || *account.OpenID == "" {
		return nil, fmt.Errorf("用户未绑定微信 openid")
	}

	desc := "雨季新江跑腿配送费"
	expireTime := ""
	if feeOrder.PayExpireAt != nil {
		expireTime = feeOrder.PayExpireAt.Format(time.RFC3339)
	}
	prepayResp, err := p.Client.CreateJSAPIPrepay(&wechatv3.CreateJSAPIPrepayRequest{
		AppID:       p.AppID,
		MchID:       p.MchID,
		Description: desc,
		OutTradeNo:  feeOrder.OrderNo,
		NotifyURL:   p.NotifyURL,
		Amount: wechatv3.PrepayAmount{
			Total:    wechatv3.YuanToFen(feeOrder.PayAmount),
			Currency: "CNY",
		},
		Payer: wechatv3.PrepayPayer{
			OpenID: *account.OpenID,
		},
		TimeExpire: expireTime,
	})
	if err != nil {
		return nil, fmt.Errorf("微信下单失败: %w", err)
	}

	prepayID := prepayResp.PrepayID
	pt := model.PaymentTransaction{
		SubjectType: model.PaySubjectDeliveryFee,
		SubjectID:   feeOrderID,
		OrderID:     0,
		OrderNo:     feeOrder.OrderNo,
		PrepayID:    &prepayID,
		PayAmount:   feeOrder.PayAmount,
		Status:      model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		if isDuplicateKey(err) {
			return p.CreatePrepayForSubject(sub)
		}
		return nil, err
	}

	params, err := p.Client.Signer().SignPrepay(p.AppID, prepayID)
	if err != nil {
		return nil, fmt.Errorf("生成支付签名失败: %w", err)
	}

	return &PrepayResult{
		Provider: p.Name(),
		NeedPay:  true,
		Params:   params,
		Message:  "请调起微信支付",
	}, nil
}

// HandleNotify 处理微信支付回调：验签 → 解密 → 按事件类型分流。
func (p *WeChatProvider) HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error) {
	if !p.Enabled || p.Client == nil {
		return nil, ErrNotConfigured
	}

	eventType, plaintext, err := p.Client.ParseAndDecryptNotify(headers, body)
	if err != nil {
		// 验签/解密失败直接拒绝，避免伪造回调触发全量未支付单查询。
		log.Printf("[wechat notify] verify/decrypt failed: %v", err)
		return nil, fmt.Errorf("回调验证失败: %w", err)
	}

	switch eventType {
	case wechatv3.EventPaySuccess:
		return p.handlePaySuccess(plaintext)
	case wechatv3.EventRefundSuccess:
		return p.handleRefundSuccess(plaintext)
	default:
		log.Printf("[wechat notify] unhandled event type: %s", eventType)
		return &NotifyResult{Paid: false, RawAck: `{"code":"SUCCESS"}`}, nil
	}
}

// assertPayNotifyIdentity 校验回调商户号/应用号与本地配置一致。
func (p *WeChatProvider) assertPayNotifyIdentity(mchID, appID string) error {
	if p.MchID != "" && mchID != "" && mchID != p.MchID {
		return fmt.Errorf("回调 mchid 不匹配: got=%s want=%s", mchID, p.MchID)
	}
	if p.AppID != "" && appID != "" && appID != p.AppID {
		return fmt.Errorf("回调 appid 不匹配: got=%s want=%s", appID, p.AppID)
	}
	return nil
}

// handlePaySuccess 处理支付成功回调。
func (p *WeChatProvider) handlePaySuccess(data []byte) (*NotifyResult, error) {
	notify, err := wechatv3.UnmarshalPaySuccess(data)
	if err != nil {
		return nil, err
	}

	if notify.TradeState != "SUCCESS" {
		log.Printf("[wechat notify] trade_state=%s, out_trade_no=%s, skipped", notify.TradeState, notify.OutTradeNo)
		return &NotifyResult{RawAck: `{"code":"SUCCESS"}`}, nil
	}

	txID := notify.TransactionID
	if txID == "" {
		return nil, fmt.Errorf("回调缺少 transaction_id")
	}
	if err := p.assertPayNotifyIdentity(notify.MchID, notify.AppID); err != nil {
		return nil, err
	}

	var result NotifyResult
	var refundJobs []RefundJob
	err = p.DB.Transaction(func(tx *gorm.DB) error {
		AttachRefundCollector(tx, &refundJobs)
		sub, err := ResolveSubjectByOrderNo(tx, notify.OutTradeNo)
		if err != nil {
			return err
		}
		expectedFen := wechatv3.YuanToFen(sub.Amount)
		if notify.Amount.Total != expectedFen {
			return fmt.Errorf("回调金额不匹配: order_no=%s notify=%d expected=%d",
				notify.OutTradeNo, notify.Amount.Total, expectedFen)
		}
		// 入账金额以业务单为准，避免浮点/回调字段偏差
		sub, err = p.upsertPaidTransaction(tx, notify.OutTradeNo, txID, sub.Amount, data)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := p.markSubjectPaidInTx(tx, sub, now); err != nil {
			return err
		}
		if err := p.invokeSubjectPaidCallback(tx, sub); err != nil {
			return err
		}
		result = NotifyResult{
			SubjectType: sub.Type,
			SubjectID:   sub.ID,
			OrderNo:     sub.OrderNo,
			Paid:        true,
			RawAck:      `{"code":"SUCCESS"}`,
		}
		if sub.Type == model.PaySubjectOrder {
			result.OrderID = sub.ID
		}
		return nil
	})
	if err != nil {
		if isDuplicateKey(err) {
			return &NotifyResult{Paid: true, RawAck: `{"code":"SUCCESS"}`}, nil
		}
		return nil, err
	}
	DispatchRefundJobs(refundJobs)

	return &result, nil
}

func (p *WeChatProvider) upsertPaidTransaction(tx *gorm.DB, orderNo, txID string, payAmount float64, raw []byte) (PaySubject, error) {
	var pt model.PaymentTransaction
	dbErr := tx.Where("order_no = ?", orderNo).First(&pt).Error
	if errors.Is(dbErr, gorm.ErrRecordNotFound) {
		sub, err := ResolveSubjectByOrderNo(tx, orderNo)
		if err != nil {
			return PaySubject{}, err
		}
		pt = model.PaymentTransaction{
			SubjectType:   sub.Type,
			SubjectID:     sub.ID,
			OrderID:       paymentTransactionOrderID(sub),
			OrderNo:       orderNo,
			TransactionID: &txID,
			PayAmount:     payAmount,
			Status:        model.PayTxStatusPaid,
		}
		if len(raw) > 0 {
			rawJSON := string(raw)
			pt.WechatRaw = &rawJSON
		}
		if err := tx.Create(&pt).Error; err != nil {
			if isDuplicateKey(err) {
				return sub, nil
			}
			return PaySubject{}, err
		}
		return sub, nil
	}
	if dbErr != nil {
		return PaySubject{}, dbErr
	}
	if pt.Status != model.PayTxStatusPaid {
		updates := map[string]interface{}{
			"transaction_id": txID,
			"status":         model.PayTxStatusPaid,
		}
		if len(raw) > 0 {
			rawJSON := string(raw)
			updates["wechat_raw"] = &rawJSON
		}
		if err := tx.Model(&pt).Updates(updates).Error; err != nil {
			return PaySubject{}, err
		}
	}
	sub := PaySubject{
		Type:      pt.SubjectType,
		ID:        pt.SubjectID,
		OrderNo:   pt.OrderNo,
		Amount:    pt.PayAmount,
	}
	if sub.Type == "" || sub.ID == 0 {
		resolved, err := ResolveSubjectByOrderNo(tx, orderNo)
		if err != nil {
			return PaySubject{}, err
		}
		sub = resolved
		backfill := map[string]interface{}{
			"subject_type": sub.Type,
			"subject_id":   sub.ID,
		}
		if oid := paymentTransactionOrderID(sub); oid > 0 {
			backfill["order_id"] = oid
		}
		if err := tx.Model(&pt).Updates(backfill).Error; err != nil {
			return PaySubject{}, err
		}
	}
	return sub, nil
}

func (p *WeChatProvider) markSubjectPaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	switch sub.Type {
	case model.PaySubjectOrder:
		res := query.NotDeleted(tx.Model(&model.Order{})).
			Where("id = ? AND pay_status = ?", sub.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    at,
				"prepay_id":  nil,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			// 首次支付成功写履约事件（幂等重推不重复写）
			_ = tx.Create(&model.FulfillmentEvent{
				SubjectType: model.FulfillmentSubjectOrder,
				SubjectID:   sub.ID,
				EventCode:   model.EventPaid,
				ActorRole:   model.FulfillmentActorSystem,
				Title:       "支付成功",
				CreatedAt:   time.Now(),
			}).Error
		}
		if res.RowsAffected == 0 {
			var o model.Order
			if err := query.NotDeleted(tx).Select("pay_status").First(&o, sub.ID).Error; err != nil {
				return err
			}
			if o.PayStatus != model.PayStatusPaid {
				return ErrInvalidState
			}
		}
		return nil
	case model.PaySubjectTakeout:
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND pay_status = ?", sub.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    at,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var to model.TakeoutOrder
			if err := query.NotDeleted(tx).Select("pay_status").First(&to, sub.ID).Error; err != nil {
				return err
			}
			if to.PayStatus != model.PayStatusPaid {
				return ErrInvalidState
			}
		}
		return nil
	case model.PaySubjectDeliveryFee:
		res := query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).
			Where("id = ? AND pay_status = ?", sub.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    at,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var fee model.DeliveryFeeOrder
			if err := query.NotDeleted(tx).Select("pay_status").First(&fee, sub.ID).Error; err != nil {
				return err
			}
			if fee.PayStatus != model.PayStatusPaid {
				return ErrInvalidState
			}
		}
		return nil
	case model.PaySubjectErrand:
		if IsGoodsOrderNo(sub.OrderNo) {
			return p.settleErrandGoodsPaidInTx(tx, sub, at)
		}
		return p.settleErrandFeePaidInTx(tx, sub, at)
	default:
		return ErrInvalidState
	}
}

// settleErrandFeePaidInTx 跑腿费支付成功：待支付单解锁为待接单并创建配送单；
// 旧「0 元下单」订单视为送达收款完成；已取消订单自动退款。
func (p *WeChatProvider) settleErrandFeePaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	var eo model.ErrandOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "status", "pay_status", "pay_amount", "delivery_fee", "goods_amount", "delivery_order_id").
		First(&eo, sub.ID).Error; err != nil {
		return err
	}
	if eo.PayStatus == model.PayStatusPaid {
		return nil
	}
	if eo.PayStatus != model.PayStatusUnpaid {
		return ErrInvalidState
	}
	if eo.Status == model.ErrandStatusCancelled {
		// 订单已取消/超时关闭后支付成功：按资金优先自动全额退款
		if err := query.NotDeleted(tx.Model(&model.ErrandOrder{})).
			Where("id = ? AND pay_status = ?", eo.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    at,
			}).Error; err != nil {
			return err
		}
		if err := p.refundErrandAmountInTx(tx, sub, sub.Amount, "订单已取消，支付款自动退回"); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]interface{}{"amount": sub.Amount})
		_ = tx.Create(&model.FulfillmentEvent{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   "auto_refund",
			ActorRole:   model.FulfillmentActorSystem,
			Title:       "订单已取消，支付款自动退款",
			Detail:      detail,
			CreatedAt:   at,
		}).Error
		return nil
	}
	if eo.Status == model.ErrandStatusPendingPay {
		delivery := model.DeliveryOrder{
			ErrandOrderID: &eo.ID,
			Status:        model.DeliveryPendingAccept,
		}
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}
		if err := query.NotDeleted(tx.Model(&model.ErrandOrder{})).
			Where("id = ? AND pay_status = ?", eo.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status":        model.PayStatusPaid,
				"paid_at":           at,
				"status":            model.ErrandStatusPendingAccept,
				"pay_expire_at":     nil,
				"delivery_order_id": delivery.ID,
			}).Error; err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]interface{}{"amount": sub.Amount})
		_ = tx.Create(&model.FulfillmentEvent{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventPaid,
			ActorRole:   model.FulfillmentActorUser,
			Title:       "跑腿费支付成功，等待骑手接单",
			Detail:      detail,
			CreatedAt:   at,
		}).Error
		return nil
	}
	// 旧流程：送达时一次性付清跑腿费 + 商品费
	res := query.NotDeleted(tx.Model(&model.ErrandOrder{})).
		Where("id = ? AND pay_status = ?", eo.ID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"pay_status":    model.PayStatusPaid,
			"paid_at":       at,
			"status":        model.ErrandStatusPendingConfirm,
			"pay_expire_at": nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var cur model.ErrandOrder
		if err := query.NotDeleted(tx).Select("pay_status").First(&cur, eo.ID).Error; err != nil {
			return err
		}
		if cur.PayStatus != model.PayStatusPaid {
			return ErrInvalidState
		}
		return nil
	}
	_ = tx.Model(&model.DeliveryOrder{}).
		Where("errand_order_id = ? AND status IN ?", eo.ID,
			[]int{int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering)}).
		Updates(map[string]interface{}{"status": model.DeliveryDelivered, "delivered_at": at})
	detail, _ := json.Marshal(map[string]interface{}{"amount": sub.Amount})
	_ = tx.Create(&model.FulfillmentEvent{
		SubjectType: model.FulfillmentSubjectErrand,
		SubjectID:   eo.ID,
		EventCode:   model.EventPaid,
		ActorRole:   model.FulfillmentActorUser,
		Title:       "平台支付成功",
		Detail:      detail,
		CreatedAt:   at,
	}).Error
	return nil
}

// settleErrandGoodsPaidInTx 代购商品费支付成功：订单进入待确认，配送单标记送达。
func (p *WeChatProvider) settleErrandGoodsPaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	var eo model.ErrandOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "status", "goods_pay_status", "goods_amount", "delivery_order_id").
		First(&eo, sub.ID).Error; err != nil {
		return err
	}
	if eo.GoodsPayStatus == model.PayStatusPaid {
		return nil
	}
	if eo.GoodsPayStatus != model.PayStatusUnpaid {
		return ErrInvalidState
	}
	res := query.NotDeleted(tx.Model(&model.ErrandOrder{})).
		Where("id = ? AND goods_pay_status = ?", eo.ID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"goods_pay_status": model.PayStatusPaid,
			"pay_mode":         model.ErrandPayModePlatform,
			"status":           model.ErrandStatusPendingConfirm,
			"pay_expire_at":    nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var cur model.ErrandOrder
		if err := query.NotDeleted(tx).Select("goods_pay_status").First(&cur, eo.ID).Error; err != nil {
			return err
		}
		if cur.GoodsPayStatus != model.PayStatusPaid {
			return ErrInvalidState
		}
		return nil
	}
	_ = tx.Model(&model.DeliveryOrder{}).
		Where("errand_order_id = ? AND status IN ?", eo.ID,
			[]int{int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering)}).
		Updates(map[string]interface{}{"status": model.DeliveryDelivered, "delivered_at": at})
	detail, _ := json.Marshal(map[string]interface{}{"amount": sub.Amount})
	_ = tx.Create(&model.FulfillmentEvent{
		SubjectType: model.FulfillmentSubjectErrand,
		SubjectID:   eo.ID,
		EventCode:   model.EventPaid,
		ActorRole:   model.FulfillmentActorUser,
		Title:       "商品费支付成功",
		Detail:      detail,
		CreatedAt:   at,
	}).Error
	return nil
}

func (p *WeChatProvider) invokeSubjectPaidCallback(tx *gorm.DB, sub PaySubject) error {
	if p.OnSubjectPaidInTx != nil {
		return p.OnSubjectPaidInTx(tx, sub)
	}
	if sub.Type == model.PaySubjectOrder {
		if p.OnPaidInTx != nil {
			return p.OnPaidInTx(tx, sub.ID)
		}
		return tx.Model(&model.Order{}).
			Where("id = ? AND status = ?", sub.ID, model.OrderStatusPendingPay).
			Updates(map[string]interface{}{
				"status":                model.OrderStatusPendingFulfill,
				"merchant_review_stage": model.MerchantReviewPending,
				"pay_expire_at":         nil,
			}).Error
	}
	return nil
}

// handleRefundSuccess 处理退款成功回调：确认入账 refunded_amount，释放 pending。
func (p *WeChatProvider) handleRefundSuccess(data []byte) (*NotifyResult, error) {
	notify, err := wechatv3.UnmarshalRefundSuccess(data)
	if err != nil {
		return nil, err
	}
	if notify.RefundStatus != "SUCCESS" {
		return &NotifyResult{RawAck: `{"code":"SUCCESS"}`}, nil
	}
	if err := p.assertPayNotifyIdentity(notify.MchID, ""); err != nil {
		return nil, err
	}
	if notify.RefundID == "" || notify.OutRefundNo == "" {
		return nil, fmt.Errorf("回调缺少 refund_id/out_refund_no")
	}

	refundYuan := wechatv3.FenToYuan(notify.Amount.Refund)
	if refundYuan < 0 {
		refundYuan = 0
	}

	err = p.DB.Transaction(func(tx *gorm.DB) error {
		var pt model.PaymentTransaction
		if err := tx.Where("order_no = ?", notify.OutTradeNo).First(&pt).Error; err != nil {
			return err
		}
		subjectType := pt.SubjectType
		if subjectType == "" {
			subjectType = SubjectTypeFromOrderNo(notify.OutTradeNo)
		}
		var rawJSON *string
		if len(data) > 0 {
			s := string(data)
			rawJSON = &s
		}
		rec := model.PaymentRefund{
			OrderNo:      notify.OutTradeNo,
			OutRefundNo:  notify.OutRefundNo,
			RefundID:     notify.RefundID,
			SubjectType:  subjectType,
			SubjectID:    pt.SubjectID,
			RefundAmount: refundYuan,
			Status:       1,
			WechatRaw:    rawJSON,
		}
		if err := tx.Create(&rec).Error; err != nil {
			if isDuplicateKey(err) {
				// 已处理过的退款通知：幂等成功，不再累加
				return nil
			}
			return err
		}
		switch subjectType {
		case model.PaySubjectTakeout:
			return p.applyTakeoutRefundInTx(tx, pt, refundYuan)
		case model.PaySubjectDeliveryFee:
			return p.applyDeliveryFeeRefundInTx(tx, pt, refundYuan)
		case model.PaySubjectErrand:
			return p.applyErrandRefundInTx(tx, pt, refundYuan)
		default:
			return p.applyOrderRefundInTx(tx, pt, refundYuan)
		}
	})
	if err != nil {
		return nil, err
	}
	return &NotifyResult{
		OrderNo: notify.OutTradeNo,
		Paid:    true,
		RawAck:  `{"code":"SUCCESS"}`,
	}, nil
}

func (p *WeChatProvider) applyOrderRefundInTx(tx *gorm.DB, pt model.PaymentTransaction, refundYuan float64) error {
	var order model.Order
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
		First(&order, pt.OrderID).Error; err != nil {
		return err
	}

	newRefunded := order.RefundedAmount + refundYuan
	if newRefunded > order.PayAmount {
		newRefunded = order.PayAmount
	}
	newPending := order.RefundPendingAmount - refundYuan
	if newPending < 0 {
		newPending = 0
	}
	status := model.PayStatusPartialRefunded
	switch {
	case newRefunded+0.0001 >= order.PayAmount:
		status = model.PayStatusRefunded
		newPending = 0
	case newPending > 0:
		status = model.PayStatusRefunding
	}

	if err := query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", order.ID).Updates(map[string]interface{}{
		"refunded_amount":       newRefunded,
		"refund_pending_amount": newPending,
		"pay_status":            status,
		"refund_failed":         0,
		"refund_fail_reason":    nil,
	}).Error; err != nil {
		return err
	}
	if status == model.PayStatusRefunded {
		_ = query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", order.ID).
			Update("status", model.OrderStatusRefunded).Error
	}
	if status == model.PayStatusRefunded || status == model.PayStatusPartialRefunded {
		title := "部分退款到账"
		if status == model.PayStatusRefunded {
			title = "退款已到账"
		}
		detail, _ := json.Marshal(map[string]interface{}{"amount": refundYuan})
		_ = tx.Create(&model.FulfillmentEvent{
			SubjectType: model.FulfillmentSubjectOrder,
			SubjectID:   order.ID,
			EventCode:   model.EventRefundSucceeded,
			ActorRole:   model.FulfillmentActorSystem,
			Title:       title,
			Detail:      detail,
			CreatedAt:   time.Now(),
		}).Error
	}
	if pt.Status != model.PayTxStatusRefunded && status == model.PayStatusRefunded {
		_ = tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error
	}
	return nil
}

func (p *WeChatProvider) applyTakeoutRefundInTx(tx *gorm.DB, pt model.PaymentTransaction, refundYuan float64) error {
	var takeout model.TakeoutOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount").
		First(&takeout, pt.SubjectID).Error; err != nil {
		return err
	}
	newRefunded := takeout.RefundedAmount + refundYuan
	if newRefunded > takeout.PayAmount {
		newRefunded = takeout.PayAmount
	}
	status := model.PayStatusPartialRefunded
	if newRefunded+0.0001 >= takeout.PayAmount {
		status = model.PayStatusRefunded
	}
	if err := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).Where("id = ?", takeout.ID).Updates(map[string]interface{}{
		"refunded_amount": newRefunded,
		"pay_status":      status,
	}).Error; err != nil {
		return err
	}
	if status == model.PayStatusRefunded {
		_ = query.NotDeleted(tx.Model(&model.TakeoutOrder{})).Where("id = ?", takeout.ID).
			Update("status", model.TakeoutStatusCancelled).Error
		detail, _ := json.Marshal(map[string]interface{}{"amount": refundYuan})
		_ = tx.Create(&model.FulfillmentEvent{
			SubjectType: model.FulfillmentSubjectTakeout,
			SubjectID:   takeout.ID,
			EventCode:   model.EventRefundSucceeded,
			ActorRole:   model.FulfillmentActorSystem,
			Title:       "退款已到账",
			Detail:      detail,
			CreatedAt:   time.Now(),
		}).Error
	}
	if pt.Status != model.PayTxStatusRefunded && status == model.PayStatusRefunded {
		_ = tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error
	}
	return nil
}

func (p *WeChatProvider) applyErrandRefundInTx(tx *gorm.DB, pt model.PaymentTransaction, refundYuan float64) error {
	var eo model.ErrandOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount").
		First(&eo, pt.SubjectID).Error; err != nil {
		return err
	}
	newRefunded := eo.RefundedAmount + refundYuan
	if newRefunded > eo.PayAmount {
		newRefunded = eo.PayAmount
	}
	status := model.PayStatusPartialRefunded
	if newRefunded+0.0001 >= eo.PayAmount {
		status = model.PayStatusRefunded
	}
	if err := query.NotDeleted(tx.Model(&model.ErrandOrder{})).Where("id = ?", eo.ID).Updates(map[string]interface{}{
		"refunded_amount": newRefunded,
		"pay_status":      status,
	}).Error; err != nil {
		return err
	}
	if pt.Status != model.PayTxStatusRefunded && status == model.PayStatusRefunded {
		_ = tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error
	}
	return nil
}

func (p *WeChatProvider) applyDeliveryFeeRefundInTx(tx *gorm.DB, pt model.PaymentTransaction, refundYuan float64) error {
	var fee model.DeliveryFeeOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount").
		First(&fee, pt.SubjectID).Error; err != nil {
		return err
	}
	newRefunded := fee.RefundedAmount + refundYuan
	if newRefunded > fee.PayAmount {
		newRefunded = fee.PayAmount
	}
	status := model.PayStatusPartialRefunded
	if newRefunded+0.0001 >= fee.PayAmount {
		status = model.PayStatusRefunded
	}
	if err := query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).Where("id = ?", fee.ID).Updates(map[string]interface{}{
		"refunded_amount": newRefunded,
		"pay_status":      status,
	}).Error; err != nil {
		return err
	}
	if pt.Status != model.PayTxStatusRefunded && status == model.PayStatusRefunded {
		_ = tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error
	}
	return nil
}

// RefundInTx 在事务内预留全额退款，事务提交后再异步发起微信退款。
func (p *WeChatProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	return p.RefundAmountInTx(tx, orderID, 0, "用户取消")
}

// RefundAmountInTx 按金额预留退款（不提前记入 refunded_amount）。amount<=0 表示退剩余全部。
// 真正的微信 CreateRefund 在事务 Commit 之后执行；失败会释放 pending。
func (p *WeChatProvider) RefundAmountInTx(tx *gorm.DB, orderID uint64, amount float64, reason string) error {
	sub, err := OrderSubjectFromID(tx, orderID, 0)
	if err != nil {
		return err
	}
	return p.RefundSubjectAmountInTx(tx, sub, amount, reason)
}

func (p *WeChatProvider) RefundSubjectAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	if !p.Enabled || p.Client == nil {
		return ErrNotConfigured
	}
	if err := sub.Validate(); err != nil {
		return err
	}
	switch sub.Type {
	case model.PaySubjectOrder:
		return p.refundOrderAmountInTx(tx, sub, amount, reason)
	case model.PaySubjectTakeout:
		return p.refundTakeoutAmountInTx(tx, sub, amount, reason)
	case model.PaySubjectDeliveryFee:
		return p.refundDeliveryFeeAmountInTx(tx, sub, amount, reason)
	case model.PaySubjectErrand:
		return p.refundErrandAmountInTx(tx, sub, amount, reason)
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) refundErrandAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	var eo model.ErrandOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "order_no", "pay_status", "pay_amount", "refunded_amount").
		First(&eo, sub.ID).Error; err != nil {
		return err
	}
	switch eo.PayStatus {
	case model.PayStatusUnpaid, model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := eo.PayAmount - eo.RefundedAmount
		if remain < 0 {
			remain = 0
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
		res := query.NotDeleted(tx.Model(&model.ErrandOrder{})).
			Where("id = ? AND pay_status = ? AND refunded_amount = ?", eo.ID, eo.PayStatus, eo.RefundedAmount).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusRefunding,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: errand %d refund conflict", ErrInvalidState, eo.ID)
		}
		refundReason := reason
		if refundReason == "" {
			refundReason = "跑腿退款"
		}
		orderNo := sub.OrderNo
		if orderNo == "" {
			orderNo = eo.OrderNo
		}
		// 微信退款单的 Total 必须等于该 out_trade_no 的原支付金额：
		// 跑腿费与商品费各自独立支付，按 sub.Amount 传入。
		totalForJob := roundMoney(sub.Amount)
		if totalForJob <= 0 {
			totalForJob = eo.PayAmount
		}
		return enqueueWeChatRefund(tx, RefundJob{
			Provider:    p,
			SubjectType: model.PaySubjectErrand,
			SubjectID:   eo.ID,
			OrderNo:     orderNo,
			OutRefundNo: fmt.Sprintf("RF%s%d", orderNo, time.Now().UnixNano()%1e12),
			PayAmount:   totalForJob,
			RefundAmt:   refund,
			Reason:      refundReason,
		})
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) refundOrderAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	var order model.Order
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "order_no", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
		First(&order, sub.ID).Error; err != nil {
		return err
	}

	switch order.PayStatus {
	case model.PayStatusUnpaid:
		return nil
	case model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := refundableRemain(order)
		// 零元已付单：无微信流水可退，本地记已退款（背包件数由业务层回滚）
		if order.PayAmount <= 0.009 {
			res := optimisticRefundWhere(
				query.NotDeleted(tx.Model(&model.Order{})),
				order.ID, order.PayStatus, order.RefundedAmount, order.RefundPendingAmount,
			).Updates(map[string]interface{}{
				"pay_status":            model.PayStatusRefunded,
				"refunded_amount":       0,
				"refund_pending_amount": 0,
				"status":                model.OrderStatusRefunded,
			})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return fmtRefundConflict(order.ID)
			}
			return nil
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
		newPending := roundMoney(order.RefundPendingAmount + refund)
		res := optimisticRefundWhere(
			query.NotDeleted(tx.Model(&model.Order{})),
			order.ID, order.PayStatus, order.RefundedAmount, order.RefundPendingAmount,
		).Updates(map[string]interface{}{
			"pay_status":            model.PayStatusRefunding,
			"refund_pending_amount": newPending,
			"status":                model.OrderStatusRefunding,
			"refund_failed":         0,
			"refund_fail_reason":    nil,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmtRefundConflict(order.ID)
		}
		refundReason := reason
		if refundReason == "" {
			refundReason = "用户退款"
		}
		if err := enqueueWeChatRefund(tx, RefundJob{
			Provider:    p,
			SubjectType: model.PaySubjectOrder,
			SubjectID:   order.ID,
			OrderID:     order.ID,
			OrderNo:     order.OrderNo,
			OutRefundNo: fmt.Sprintf("RF%s%d", order.OrderNo, time.Now().UnixNano()%1e12),
			PayAmount:   order.PayAmount,
			RefundAmt:   refund,
			Reason:      refundReason,
		}); err != nil {
			return err
		}
		return nil
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) refundTakeoutAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	var takeout model.TakeoutOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "order_no", "pay_status", "pay_amount", "refunded_amount").
		First(&takeout, sub.ID).Error; err != nil {
		return err
	}
	switch takeout.PayStatus {
	case model.PayStatusUnpaid, model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := takeout.PayAmount - takeout.RefundedAmount
		if remain < 0 {
			remain = 0
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
		newPending := roundMoney(refund)
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND pay_status = ? AND refunded_amount = ?", takeout.ID, takeout.PayStatus, takeout.RefundedAmount).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusRefunding,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: takeout %d refund conflict", ErrInvalidState, takeout.ID)
		}
		refundReason := reason
		if refundReason == "" {
			refundReason = "用户退款"
		}
		return enqueueWeChatRefund(tx, RefundJob{
			Provider:    p,
			SubjectType: model.PaySubjectTakeout,
			SubjectID:   takeout.ID,
			OrderNo:     takeout.OrderNo,
			OutRefundNo: fmt.Sprintf("RF%s%d", takeout.OrderNo, time.Now().UnixNano()%1e12),
			PayAmount:   takeout.PayAmount,
			RefundAmt:   newPending,
			Reason:      refundReason,
		})
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) refundDeliveryFeeAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	var fee model.DeliveryFeeOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "order_no", "pay_status", "pay_amount", "refunded_amount").
		First(&fee, sub.ID).Error; err != nil {
		return err
	}
	switch fee.PayStatus {
	case model.PayStatusUnpaid, model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := fee.PayAmount - fee.RefundedAmount
		if remain < 0 {
			remain = 0
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
		res := query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).
			Where("id = ? AND pay_status = ? AND refunded_amount = ?", fee.ID, fee.PayStatus, fee.RefundedAmount).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusRefunding,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: delivery fee %d refund conflict", ErrInvalidState, fee.ID)
		}
		refundReason := reason
		if refundReason == "" {
			refundReason = "配送费退款"
		}
		return enqueueWeChatRefund(tx, RefundJob{
			Provider:    p,
			SubjectType: model.PaySubjectDeliveryFee,
			SubjectID:   fee.ID,
			OrderNo:     fee.OrderNo,
			OutRefundNo: fmt.Sprintf("RF%s%d", fee.OrderNo, time.Now().UnixNano()%1e12),
			PayAmount:   fee.PayAmount,
			RefundAmt:   refund,
			Reason:      refundReason,
		})
	default:
		return ErrInvalidState
	}
}

// --- helper ---

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// isDuplicateKey 判断是否为 MySQL 唯一索引冲突。
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "1062")
}

func (p *WeChatProvider) prepayResultFromID(prepayID string) (*PrepayResult, error) {
	if prepayID == "" {
		return nil, ErrInvalidState
	}
	params, err := p.Client.Signer().SignPrepay(p.AppID, prepayID)
	if err != nil {
		return nil, fmt.Errorf("生成支付签名失败: %w", err)
	}
	return &PrepayResult{
		Provider: p.Name(),
		NeedPay:  true,
		Params:   params,
		Message:  "请调起微信支付",
	}, nil
}

// reuseOpenPrepay 若该主体已有 open 预支付流水，直接复用其 prepay_id。
// payExpireAt 已过期（或距过期不足 30s）时不复用，并作废本地预支付记录 + 关闭微信侧单据。
// 不再回落到订单行上的 prepay_id：流水已失败/缺失时签名会指向失效单。
func (p *WeChatProvider) reuseOpenPrepay(subjectType string, subjectID uint64, orderNo string, payExpireAt *time.Time) (*PrepayResult, error) {
	now := time.Now()
	if payExpireAt != nil && payExpireAt.Sub(now) < 30*time.Second {
		if err := p.invalidateOpenPrepay(subjectType, subjectID, orderNo); err != nil {
			return nil, err
		}
		return nil, nil
	}

	var existing model.PaymentTransaction
	q := p.DB.Where("status = ?", model.PayTxStatusPrepay)
	if subjectType == model.PaySubjectOrder {
		q = q.Where("(subject_type = ? AND subject_id = ?) OR (order_id = ? AND subject_id = 0)",
			subjectType, subjectID, subjectID)
	} else {
		q = q.Where("subject_type = ? AND subject_id = ?", subjectType, subjectID)
		// 跑腿单存在两笔支付（跑腿费/商品费），按支付单号区分
		if subjectType == model.PaySubjectErrand {
			q = q.Where("order_no = ?", orderNo)
		}
	}
	err := q.Order("id DESC").First(&existing).Error
	if err == nil && existing.PrepayID != nil && *existing.PrepayID != "" {
		return p.prepayResultFromID(*existing.PrepayID)
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return nil, nil
}

// invalidateOpenPrepay 作废本地预支付流水并清空订单 prepay_id。
// 会尽力关闭微信侧未支付单；CloseOrder 瞬时失败时仍允许重新拉起（已支付则返回错误）。
func (p *WeChatProvider) invalidateOpenPrepay(subjectType string, subjectID uint64, orderNo string) error {
	q := p.DB.Model(&model.PaymentTransaction{}).Where("status = ?", model.PayTxStatusPrepay)
	if subjectType == model.PaySubjectOrder {
		q = q.Where("(subject_type = ? AND subject_id = ?) OR (order_id = ? AND subject_id = 0)",
			subjectType, subjectID, subjectID)
	} else {
		q = q.Where("subject_type = ? AND subject_id = ?", subjectType, subjectID)
		if subjectType == model.PaySubjectErrand {
			q = q.Where("order_no = ?", orderNo)
		}
	}
	if err := q.Update("status", model.PayTxStatusFailed).Error; err != nil {
		return err
	}
	if subjectType == model.PaySubjectOrder {
		if err := p.DB.Model(&model.Order{}).Where("id = ?", subjectID).
			Update("prepay_id", nil).Error; err != nil {
			return err
		}
	}
	if p.Client != nil && orderNo != "" {
		if err := p.Client.CloseOrder(p.MchID, orderNo); err != nil {
			// 已支付：禁止作废后重新下单，交由查单/回调入账
			if wechatv3.IsOrderPaid(err) {
				return fmt.Errorf("%w: 微信侧订单已支付，请刷新订单状态: %v", ErrInvalidState, err)
			}
			// 网络/瞬时失败：本地预支付已作废，仍尝试重新拉起；若微信拒绝同单号再报错
			log.Printf("[wechat] close order %s failed, continue recreate prepay: %v", orderNo, err)
		}
	}
	return nil
}

// parseEventTypeSafely 从回调 JSON 中提取 event_type（不依赖 resource 解密）。
func parseEventTypeSafely(body []byte) string {
	var cb struct {
		EventType string `json:"event_type"`
	}
	if json.Unmarshal(body, &cb) == nil {
		return cb.EventType
	}
	return ""
}

// retrieveAndSettlePayments 主动查微信支付单状态，处理所有待支付订单。
func (p *WeChatProvider) retrieveAndSettlePayments() (*NotifyResult, error) {
	if p.Client == nil {
		return nil, ErrNotConfigured
	}
	var orders []model.Order
	if err := query.NotDeleted(p.DB).
		Where("status IN ? AND pay_status = ?",
			[]int{int(model.OrderStatusPendingPay), int(model.OrderStatusPendingGroup)},
			model.PayStatusUnpaid).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	for _, o := range orders {
		tradeState, transactionID, err := p.Client.QueryOrderByOutTradeNo(o.OrderNo)
		if err != nil {
			log.Printf("[wechat retrieve] 查询订单 %s 失败: %v", o.OrderNo, err)
			continue
		}
		if tradeState == "SUCCESS" && o.PrepayID != nil {
			_ = p.DB.Transaction(func(tx *gorm.DB) error {
				txID := transactionID
				now := time.Now()
				tx.Model(&model.PaymentTransaction{}).Where("order_id = ?", o.ID).
					Updates(map[string]interface{}{"status": model.PayTxStatusPaid, "transaction_id": txID})
				res := query.NotDeleted(tx.Model(&model.Order{})).
					Where("id = ? AND pay_status = ?", o.ID, model.PayStatusUnpaid).
					Updates(map[string]interface{}{
						"pay_status": model.PayStatusPaid,
						"paid_at":    now,
						"prepay_id":  nil,
					})
				if res.RowsAffected == 0 {
					return nil
				}
				// 推进订单 + 自动审核
				if p.OnPaidInTx != nil {
					return p.OnPaidInTx(tx, o.ID)
				}
				// 仅直购待支付推进；拼团单保持 PendingGroup，等成团逻辑处理
				return tx.Model(&model.Order{}).Where("id = ? AND status = ?", o.ID, model.OrderStatusPendingPay).
					Updates(map[string]interface{}{
						"status":                model.OrderStatusPendingFulfill,
						"merchant_review_stage": model.MerchantReviewPending,
						"pay_expire_at":         nil,
					}).Error
			})
			log.Printf("[wechat retrieve] 订单 %s 支付成功，已处理", o.OrderNo)
		}
	}
	if err := p.retrieveAndSettleTakeoutPayments(); err != nil {
		log.Printf("[wechat retrieve] 外卖单查单结算失败: %v", err)
	}
	if err := p.retrieveAndSettleDeliveryFeePayments(); err != nil {
		log.Printf("[wechat retrieve] 配送费单查单结算失败: %v", err)
	}
	return &NotifyResult{Paid: true, RawAck: `{"code":"SUCCESS"}`}, nil
}

// retrieveAndSettleTakeoutPayments 主动查单，恢复已付但未入账的外卖单。
func (p *WeChatProvider) retrieveAndSettleTakeoutPayments() error {
	if p.Client == nil {
		return ErrNotConfigured
	}
	var takeouts []model.TakeoutOrder
	if err := query.NotDeleted(p.DB).
		Where("status = ? AND pay_status = ?", model.TakeoutStatusPendingPay, model.PayStatusUnpaid).
		Find(&takeouts).Error; err != nil {
		return err
	}
	for _, to := range takeouts {
		tradeState, transactionID, err := p.Client.QueryOrderByOutTradeNo(to.OrderNo)
		if err != nil {
			log.Printf("[wechat retrieve] 查询外卖单 %s 失败: %v", to.OrderNo, err)
			continue
		}
		if tradeState != "SUCCESS" {
			continue
		}
		if err := p.DB.Transaction(func(tx *gorm.DB) error {
			sub, err := p.upsertPaidTransaction(tx, to.OrderNo, transactionID, to.PayAmount, nil)
			if err != nil {
				return err
			}
			now := time.Now()
			if err := p.markSubjectPaidInTx(tx, sub, now); err != nil {
				return err
			}
			return p.invokeSubjectPaidCallback(tx, sub)
		}); err != nil {
			log.Printf("[wechat retrieve] 外卖单 %s 支付成功处理失败: %v", to.OrderNo, err)
			continue
		}
		log.Printf("[wechat retrieve] 外卖单 %s 支付成功，已处理", to.OrderNo)
	}
	return nil
}

// retrieveAndSettleDeliveryFeePayments 主动查单，恢复已付但未履约的配送费单。
func (p *WeChatProvider) retrieveAndSettleDeliveryFeePayments() error {
	if p.Client == nil {
		return ErrNotConfigured
	}
	var fees []model.DeliveryFeeOrder
	if err := query.NotDeleted(p.DB).
		Where("status = ? AND pay_status = ?", model.DeliveryFeeStatusPendingPay, model.PayStatusUnpaid).
		Find(&fees).Error; err != nil {
		return err
	}
	for _, fee := range fees {
		tradeState, transactionID, err := p.Client.QueryOrderByOutTradeNo(fee.OrderNo)
		if err != nil {
			log.Printf("[wechat retrieve] 查询配送费单 %s 失败: %v", fee.OrderNo, err)
			continue
		}
		if tradeState != "SUCCESS" {
			continue
		}
		if err := p.DB.Transaction(func(tx *gorm.DB) error {
			sub, err := p.upsertPaidTransaction(tx, fee.OrderNo, transactionID, fee.PayAmount, nil)
			if err != nil {
				return err
			}
			now := time.Now()
			if err := p.markSubjectPaidInTx(tx, sub, now); err != nil {
				return err
			}
			return p.invokeSubjectPaidCallback(tx, sub)
		}); err != nil {
			log.Printf("[wechat retrieve] 配送费单 %s 支付成功处理失败: %v", fee.OrderNo, err)
			continue
		}
		log.Printf("[wechat retrieve] 配送费单 %s 支付成功，已处理", fee.OrderNo)
	}
	return nil
}
