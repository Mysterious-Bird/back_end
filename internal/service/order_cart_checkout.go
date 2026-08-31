package service

import (
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CheckoutCartInput 同店购物车直购合单（一笔订单、一次支付）。
type CheckoutCartInput struct {
	CartItemIDs       []uint64
	MerchantID        uint64
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
	UserCouponID      *uint64
}

type cartCheckoutLine struct {
	Cart     model.CartItem
	Product  model.Product
	UnitPrice float64
	Subtotal  float64
}

// CheckoutCart 将同店、直购购物车行合并为一笔订单。
// 含套餐时不写 package_product_id，以便入背包时入账全部明细（含套餐本体）。
func (s *OrderService) CheckoutCart(accountID uint64, input CheckoutCartInput) (*OrderView, error) {
	if err := AssertAccountHasPhone(s.DB, accountID); err != nil {
		return nil, err
	}
	if len(input.CartItemIDs) == 0 {
		return nil, fmt.Errorf("%w: 请选择购物车商品", ErrInvalidProductArg)
	}
	if input.MerchantID == 0 {
		return nil, fmt.Errorf("%w: 请指定商家", ErrInvalidProductArg)
	}
	if err := assertBagPurchaseAllowed(model.PurchaseTypeSolo, nil); err != nil {
		return nil, err
	}
	if err := assertBagPickupOnly(input.DeliveryType); err != nil {
		return nil, err
	}
	input.DeliveryType = model.DeliveryTypePickup
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}
	coordIn := DeliveryCoordinateInput{
		AddressID: input.AddressID, DeliveryLatitude: input.DeliveryLatitude, DeliveryLongitude: input.DeliveryLongitude,
	}
	if s.ZoneSvc != nil {
		if err := s.ZoneSvc.ValidateDelivery(accountID, input.MerchantID, deliveryType, coordIn); err != nil {
			return nil, err
		}
	}

	var cartItems []model.CartItem
	if err := query.NotDeleted(s.DB).
		Where("account_id = ? AND id IN ?", accountID, input.CartItemIDs).
		Find(&cartItems).Error; err != nil {
		return nil, err
	}
	if len(cartItems) != len(input.CartItemIDs) {
		return nil, ErrCartItemNotFound
	}

	seen := map[uint64]struct{}{}
	lines := make([]cartCheckoutLine, 0, len(cartItems))
	var totalSub float64
	allEnableCoupon := true

	for _, cart := range cartItems {
		if _, ok := seen[cart.ID]; ok {
			return nil, fmt.Errorf("%w: 购物车项重复", ErrInvalidProductArg)
		}
		seen[cart.ID] = struct{}{}
		if cart.PurchaseType == model.PurchaseTypeGroup {
			return nil, fmt.Errorf("%w: 拼团商品请单独结算", ErrInvalidProductArg)
		}
		if cart.Quantity == 0 {
			return nil, fmt.Errorf("%w: 数量无效", ErrInvalidProductArg)
		}

		var product model.Product
		if err := query.NotDeleted(s.DB).
			Where("id = ? AND merchant_id = ? AND status = ?", cart.ProductID, input.MerchantID, model.ProductStatusOn).
			First(&product).Error; err != nil {
			return nil, ErrProductNotFound
		}
		if product.ItemType == model.ProductItemTypePackage {
			if _, err := (&ProductService{DB: s.DB}).LoadPackageGroups(product.ID); err != nil {
				return nil, err
			}
		}
		if product.DealStock < cart.Quantity {
			return nil, ErrInsufficientStock
		}
		if err := assertProductChannelPurchasable(product, productChannelDeal); err != nil {
			return nil, err
		}
		unit := product.Price
		sub := roundMoney(unit * float64(cart.Quantity))
		totalSub += sub
		if product.EnableCoupon != 1 {
			allEnableCoupon = false
		}
		lines = append(lines, cartCheckoutLine{
			Cart: cart, Product: product, UnitPrice: unit, Subtotal: sub,
		})
	}
	totalSub = roundMoney(totalSub)

	couponCtx := OrderCouponContext{
		AccountID: accountID, MerchantID: input.MerchantID,
		Product: lines[0].Product, Subtotal: totalSub, PurchaseType: model.PurchaseTypeSolo,
	}
	var discountAmount float64
	if input.UserCouponID != nil {
		if s.CouponSvc == nil || !allEnableCoupon {
			return nil, ErrCouponNotApplicable
		}
		for _, ln := range lines {
			probe := couponCtx
			probe.Product = ln.Product
			if _, err := s.CouponSvc.EvaluateForOrder(*input.UserCouponID, probe); err != nil {
				return nil, err
			}
		}
		d, err := s.CouponSvc.EvaluateForOrder(*input.UserCouponID, couponCtx)
		if err != nil {
			return nil, err
		}
		discountAmount = d
	}
	payAmount := roundMoney(totalSub - discountAmount)
	now := time.Now()

	productIDs := make([]uint64, 0, len(lines))
	for _, ln := range lines {
		productIDs = append(productIDs, ln.Product.ID)
	}

	var order model.Order
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := assertNoUnpaidBagOrderForProducts(tx, accountID, productIDs); err != nil {
			return err
		}
		// 事务内再锁商品行，避免库存竞态
		for i := range lines {
			var p model.Product
			if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&p, lines[i].Product.ID).Error; err != nil {
				return err
			}
			if p.Status != model.ProductStatusOn || p.MerchantID != input.MerchantID {
				return ErrProductNotFound
			}
			if p.DealStock < lines[i].Cart.Quantity {
				return ErrInsufficientStock
			}
			if err := assertProductChannelPurchasable(p, productChannelDeal); err != nil {
				return err
			}
			lines[i].Product = p
			lines[i].UnitPrice = p.Price
			lines[i].Subtotal = roundMoney(p.Price * float64(lines[i].Cart.Quantity))
		}
		recalc := 0.0
		for _, ln := range lines {
			recalc += ln.Subtotal
		}
		totalSub = roundMoney(recalc)
		if input.UserCouponID != nil {
			couponCtx.Subtotal = totalSub
			d, err := s.CouponSvc.EvaluateForOrder(*input.UserCouponID, couponCtx)
			if err != nil {
				return err
			}
			discountAmount = d
		}
		payAmount = roundMoney(totalSub - discountAmount)

		var addrSnap *model.AddressSnapshot
		if deliveryType == model.DeliveryTypeDelivery && input.AddressID != nil {
			var addr model.UserAddress
			if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
				return ErrAddressRequired
			}
			addrSnap = AddressSnapshotFromUserAddress(&addr)
		}

		expireAt := now.Add(time.Duration(s.payTimeoutMinutes()) * time.Minute)
		order = model.Order{
			OrderNo:             genOrderNo(),
			AccountID:           accountID,
			MerchantID:          input.MerchantID,
			Status:              model.OrderStatusPendingPay,
			MerchantReviewStage: model.MerchantReviewPending,
			DeliveryType:        deliveryType,
			AddressSnapshot:     addrSnap,
			TotalAmount:         totalSub,
			DiscountAmount:      discountAmount,
			UserCouponID:        input.UserCouponID,
			PayAmount:           payAmount,
			PayStatus:           model.PayStatusUnpaid,
			PayExpireAt:         &expireAt,
			Remark:              input.Remark,
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if input.UserCouponID != nil && s.CouponSvc != nil {
			if _, err := s.CouponSvc.ApplyForOrderInTx(tx, *input.UserCouponID, order.ID, couponCtx); err != nil {
				return err
			}
		}

		for _, ln := range lines {
			cover := ln.Product.CoverURL
			item := model.OrderItem{
				OrderID: order.ID, ProductID: ln.Product.ID,
				PurchaseType: model.PurchaseTypeSolo,
				ProductName:  ln.Product.Name, ProductImage: &cover,
				Spec: ln.Cart.Spec, UnitPrice: ln.UnitPrice,
				Quantity: ln.Cart.Quantity, Subtotal: ln.Subtotal,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if err := deductChannelStockInTx(tx, ln.Product.ID, ln.Cart.Quantity, productChannelDeal); err != nil {
				return err
			}
			if err := query.SoftDelete(tx, &model.CartItem{}, "id = ? AND account_id = ?", ln.Cart.ID, accountID).Error; err != nil {
				return err
			}
		}

		// 结算（含 0 元免支付）必须在订单明细写入之后，否则自动审核入包时
		// 查不到 order_item 会静默跳过，出现「审核通过但未入包」。
		if err := s.settlePaymentInTx(tx, order.ID, payAmount, now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, order.ID, nil)
}
