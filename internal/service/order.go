package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrOrderNotFound         = errors.New("order not found")
	ErrOrderForbidden        = errors.New("order forbidden")
	ErrOrderStatusInvalid    = errors.New("order status invalid")
	ErrInsufficientStock     = errors.New("insufficient stock")
	ErrGroupBuyInvalid       = errors.New("group buy invalid")
	ErrGroupBuyAlreadyJoined = errors.New("group buy already joined")
	ErrAddressRequired       = errors.New("address required")
	ErrInvalidDeliveryType   = errors.New("invalid delivery type")
	ErrSoloPurchaseDisabled  = errors.New("solo purchase disabled")
)

type OrderService struct {
	DB                *gorm.DB
	InventorySvc      *InventoryService
	CouponSvc         *CouponService
	ActivitySvc       *ActivityService
	ZoneSvc           *DeliveryZoneService
	Payment           payment.Provider
	PayTimeoutMinutes int // 待支付订单超时分钟数，0 时由 worker 用默认值
	AvatarPublicBase  string
}

type CreateOrderInput struct {
	ProductID         uint64
	MerchantID        uint64
	Quantity          uint32
	PurchaseType      uint8
	GroupBuyID        *uint64
	GroupBuyTeamID    *uint64
	StartNewTeam      bool
	ActivityProductID *uint64
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
	CartItemID        *uint64
	UserCouponID      *uint64
}

type RequestUseInput struct {
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
}

type BuyerBrief struct {
	AccountID uint64  `json:"account_id"`
	Nickname  *string `json:"nickname,omitempty"`
	Phone     *string `json:"phone,omitempty"`
}

type OrderView struct {
	model.Order
	StatusText       string            `json:"status_text"`
	StatusCode       string            `json:"status_code"`
	VerifyCode       *string           `json:"verify_code,omitempty"`
	PickupCode       string            `json:"pickup_code,omitempty"`
	DeliveryOrderID  *uint64           `json:"delivery_order_id,omitempty"`
	Buyer            *BuyerBrief       `json:"buyer,omitempty"`
	GroupBuyProgress *GroupBuyProgress `json:"group_buy_progress,omitempty"`
}

type GroupBuyProgress struct {
	TeamID              uint64               `json:"team_id"`
	TargetCount         uint32               `json:"target_count"`
	CurrentCount        uint32               `json:"current_count"`
	RemainingCount      uint32               `json:"remaining_count"`
	Status              uint8                `json:"status"`
	StatusText          string               `json:"status_text"`
	ExpireAt            string               `json:"expire_at"`
	GroupPrice          float64              `json:"group_price"`
	AllowRepeatJoin     uint8                `json:"allow_repeat_join"`
	UserJoined          bool                 `json:"user_joined"`
	UserJoinCount       uint32               `json:"user_join_count"`
	IsLeader            bool                 `json:"is_leader"`
	CanStartNewTeam     bool                 `json:"can_start_new_team"`
	MaxConcurrentTeams  uint32               `json:"max_concurrent_teams"`
	ConcurrentTeamCount uint32               `json:"concurrent_team_count"`
	Members             []GroupBuyMemberView `json:"members,omitempty"`
}

// GroupBuyMemberView 拼团页成员展示（昵称/头像）。
type GroupBuyMemberView struct {
	AccountID uint64 `json:"account_id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url,omitempty"`
	IsLeader  bool   `json:"is_leader"`
	IsMe      bool   `json:"is_me"`
}

type JoinableGroupTeamView struct {
	TeamID       uint64 `json:"team_id"`
	CurrentCount uint32 `json:"current_count"`
	TargetCount  uint32 `json:"target_count"`
	Remain       uint32 `json:"remain"`
	ExpireAt     string `json:"expire_at"`
	LeaderName   string `json:"leader_name,omitempty"`
}

// JoinableTeamsResult 可参团列表 + 是否允许开新团。
type JoinableTeamsResult struct {
	Teams               []JoinableGroupTeamView `json:"teams"`
	CanStartNewTeam     bool                    `json:"can_start_new_team"`
	MaxConcurrentTeams  uint32                  `json:"max_concurrent_teams"`
	ConcurrentTeamCount uint32                  `json:"concurrent_team_count"`
}

func (s *OrderService) Create(accountID uint64, input CreateOrderInput) (*OrderView, error) {
	if err := AssertAccountHasPhone(s.DB, accountID); err != nil {
		return nil, err
	}
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	if input.PurchaseType == 0 {
		input.PurchaseType = model.PurchaseTypeSolo
	}
	if err := assertBagPurchaseAllowed(input.PurchaseType, input.ActivityProductID); err != nil {
		return nil, err
	}
	if err := assertBagPickupOnly(input.DeliveryType); err != nil {
		return nil, err
	}
	input.DeliveryType = model.DeliveryTypePickup
	if _, err := normalizeDeliveryType(input.DeliveryType); err != nil {
		return nil, err
	}
	if input.DeliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}

	var product model.Product
	var unitPrice float64
	var actCtx *ActivityOrderContext
	var activityID *uint64
	var activityProductID *uint64
	var actGB *ActivityGroupBuyConfig

	if input.ActivityProductID != nil && s.ActivitySvc != nil {
		ctx, err := s.ActivitySvc.ResolveForOrder(accountID, *input.ActivityProductID, input.MerchantID, input.Quantity, input.PurchaseType)
		if err != nil {
			return nil, err
		}
		actCtx = ctx
		product = ctx.Product
		unitPrice = ctx.UnitPrice
		activityID = &ctx.Activity.ID
		activityProductID = &ctx.ActivityProduct.ID
		actGB = ctx.GroupBuyConfig
		input.ProductID = product.ID
		input.MerchantID = product.MerchantID
		if !ctx.EnableCoupon {
			if input.UserCouponID != nil {
				return nil, ErrCouponNotApplicable
			}
		}
	} else {
		productSvc := &ProductService{DB: s.DB}
		p, err := productSvc.GetOnShelf(input.ProductID, input.MerchantID)
		if err != nil {
			return nil, err
		}
		product = *p
		input.MerchantID = product.MerchantID
		if product.ItemType == model.ProductItemTypePackage {
			return nil, fmt.Errorf("%w: 套餐请使用套餐下单接口", ErrInvalidProductArg)
		}
		ch := purchaseTypeToChannel(input.PurchaseType)
		if err := assertProductChannelPurchasable(product, ch); err != nil {
			return nil, err
		}
		if productChannelStock(product, ch) < input.Quantity {
			return nil, ErrInsufficientStock
		}
		unitPrice = product.Price
		if input.PurchaseType == model.PurchaseTypeGroup {
			if product.GroupBuyPrice == nil {
				return nil, ErrGroupBuyInvalid
			}
			unitPrice = *product.GroupBuyPrice
		} else {
			input.GroupBuyTeamID = nil
		}
	}

	coordIn := DeliveryCoordinateInput{
		AddressID: input.AddressID, DeliveryLatitude: input.DeliveryLatitude, DeliveryLongitude: input.DeliveryLongitude,
	}
	if s.ZoneSvc != nil {
		if err := s.ZoneSvc.ValidateDelivery(accountID, product.MerchantID, input.DeliveryType, coordIn); err != nil {
			return nil, err
		}
	}

	if input.PurchaseType == model.PurchaseTypeGroup {
		if actGB == nil {
			if product.EnableGroupBuy != 1 || product.GroupBuyPrice == nil {
				return nil, ErrGroupBuyInvalid
			}
		} else if actGB.EnableGroupBuy != 1 {
			return nil, ErrGroupBuyInvalid
		}
	} else {
		input.GroupBuyTeamID = nil
		input.StartNewTeam = false
	}

	if err := assertActivityGroupBuyOnly(input.PurchaseType, actCtx); err != nil {
		return nil, err
	}
	if input.PurchaseType == model.PurchaseTypeGroup {
		if err := validateGroupBuyOrderInput(input.Quantity, input.GroupBuyTeamID, input.StartNewTeam); err != nil {
			return nil, err
		}
	}

	if actCtx == nil {
		ch := purchaseTypeToChannel(input.PurchaseType)
		if productChannelStock(product, ch) < input.Quantity {
			return nil, ErrInsufficientStock
		}
	}

	var groupBuyID *uint64
	var groupBuyTeamID *uint64

	subtotal := unitPrice * float64(input.Quantity)
	couponCtx := OrderCouponContext{
		AccountID: accountID, MerchantID: input.MerchantID, Product: product,
		Subtotal: subtotal, PurchaseType: input.PurchaseType,
	}
	var discountAmount float64
	if input.UserCouponID != nil {
		if s.CouponSvc == nil {
			return nil, ErrCouponNotApplicable
		}
		d, err := s.CouponSvc.EvaluateForOrder(*input.UserCouponID, couponCtx)
		if err != nil {
			return nil, err
		}
		discountAmount = d
	}
	subtotal = roundMoney(subtotal)
	payAmount := roundMoney(subtotal - discountAmount)

	now := time.Now()
	orderNo := genOrderNo()

	var gb model.GroupBuy
	if input.PurchaseType == model.PurchaseTypeGroup {
		ensured, err := s.ensureActiveGroupBuy(product, actGB)
		if err != nil {
			return nil, err
		}
		gb = *ensured
		resolved, err := resolveClientGroupBuy(s.DB, product.ID, gb, input.GroupBuyID)
		if err != nil {
			return nil, err
		}
		gb = resolved
		groupBuyID = &gb.ID
		if input.StartNewTeam {
			if err := assertCanStartNewTeam(s.DB, product, gb, actGB, activityID, activityProductID); err != nil {
				return nil, err
			}
		}
	}

	var order model.Order
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := assertNoUnpaidBagOrderForProducts(tx, accountID, []uint64{product.ID}); err != nil {
			return err
		}
		// Re-check activity purchase limits inside the tx (ResolveForOrder is a fast-fail
		// pre-check only). Lock activity_product first so concurrent creates serialize
		// before counting — same TOCTOU class CreditSoldInTx already closes for stock.
		if s.ActivitySvc != nil && activityProductID != nil && actCtx != nil && actCtx.ActivityProduct != nil {
			var apLock model.ActivityProduct
			if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&apLock, *activityProductID).Error; err != nil {
				return err
			}
			if _, err := ensurePlatformDailyBucketLocked(tx, &apLock, now); err != nil {
				return err
			}
			if err := s.ActivitySvc.checkUserLimits(tx, accountID, &apLock, input.Quantity); err != nil {
				return err
			}
		}

		status := model.OrderStatusPendingPay
		reviewStage := model.MerchantReviewPending
		if input.PurchaseType == model.PurchaseTypeGroup {
			status = model.OrderStatusPendingGroup
			reviewStage = model.MerchantReviewNone
		}

		var addrSnap *model.AddressSnapshot
		if input.DeliveryType == model.DeliveryTypeDelivery && input.AddressID != nil {
			var addr model.UserAddress
			if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
				return ErrAddressRequired
			}
			addrSnap = AddressSnapshotFromUserAddress(&addr)
		}

		// 直购与拼团均需先支付：记录支付超时。成团推进只计已支付订单。
		expireAt := now.Add(time.Duration(s.payTimeoutMinutes()) * time.Minute)
		payExpireAt := &expireAt

		order = model.Order{
			OrderNo:             orderNo,
			AccountID:           accountID,
			MerchantID:          input.MerchantID,
			ActivityID:          activityID,
			Status:              status,
			MerchantReviewStage: reviewStage,
			DeliveryType:        input.DeliveryType,
			AddressSnapshot:     addrSnap,
			TotalAmount:         subtotal,
			DiscountAmount:      discountAmount,
			UserCouponID:        input.UserCouponID,
			PayAmount:           payAmount,
			PayStatus:           model.PayStatusUnpaid,
			PayExpireAt:         payExpireAt,
			Remark:              input.Remark,
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if err := s.settlePaymentInTx(tx, order.ID, payAmount, now); err != nil {
			return err
		}

		if input.UserCouponID != nil && s.CouponSvc != nil {
			if _, err := s.CouponSvc.ApplyForOrderInTx(tx, *input.UserCouponID, order.ID, couponCtx); err != nil {
				return err
			}
		}

		spec := (*string)(nil)
		if input.CartItemID != nil {
			var cart model.CartItem
			if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *input.CartItemID, accountID).First(&cart).Error; err == nil {
				spec = cart.Spec
			}
		}

		item := model.OrderItem{
			OrderID: order.ID, ProductID: product.ID,
			ActivityID: activityID, ActivityProductID: activityProductID,
			PurchaseType: input.PurchaseType,
			GroupBuyID:   groupBuyID, ProductName: product.Name, ProductImage: &product.CoverURL,
			Spec: spec, UnitPrice: unitPrice, Quantity: input.Quantity, Subtotal: subtotal,
		}

		if input.PurchaseType == model.PurchaseTypeGroup {
			// 未付款不入团：仅校验并记录意向团；付款成功后再 joinOrCreateTeam。
			teamID, err := s.resolveUnpaidGroupTeamID(tx, accountID, product, gb, input.GroupBuyTeamID, input.StartNewTeam, actGB, activityID)
			if err != nil {
				return err
			}
			groupBuyTeamID = teamID
			item.GroupBuyTeamID = groupBuyTeamID
		}

		if err := tx.Create(&item).Error; err != nil {
			return err
		}

		stockCh := stockChannelForOrder(product, input.PurchaseType, activityProductID != nil)
		if err := deductChannelStockInTx(tx, product.ID, input.Quantity, stockCh); err != nil {
			return err
		}

		if s.ActivitySvc != nil && activityProductID != nil {
			bucket, err := s.ActivitySvc.CreditSoldInTx(tx, *activityProductID, input.Quantity)
			if err != nil {
				return err
			}
			if bucket != nil {
				if err := tx.Model(&item).Update("platform_daily_bucket", *bucket).Error; err != nil {
					return err
				}
			}
		}

		if input.CartItemID != nil {
			_ = query.SoftDelete(tx, &model.CartItem{}, "id = ? AND account_id = ?", *input.CartItemID, accountID).Error
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, order.ID, nil)
}

// resolveUnpaidGroupTeamID 下单未付款时只选定意向团（不占名额、不写 member）。
func (s *OrderService) resolveUnpaidGroupTeamID(tx *gorm.DB, accountID uint64, product model.Product, gb model.GroupBuy, teamID *uint64, startNewTeam bool, actGB *ActivityGroupBuyConfig, activityID *uint64) (*uint64, error) {
	if startNewTeam {
		return nil, nil
	}
	if teamID == nil || *teamID == 0 {
		return nil, nil
	}
	var team model.GroupBuyTeam
	if err := query.NotDeleted(tx).
		Where("id = ? AND group_buy_id = ? AND status = ?", *teamID, gb.ID, model.GroupBuyTeamPending).
		First(&team).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}
	joinCount, err := countUserTeamJoins(tx, accountID, team.ID, activityID, 0)
	if err != nil {
		return nil, err
	}
	allowRepeat, maxJoins := resolveGroupBuyRepeat(product, actGB)
	if err := validateTeamJoinLimit(joinCount, allowRepeat, maxJoins); err != nil {
		return nil, err
	}
	paidCount, err := countPaidPendingGroupOnTeam(tx, team.ID, 0)
	if err != nil {
		return nil, err
	}
	if paidCount >= team.TargetCount {
		return nil, ErrGroupBuyInvalid
	}
	id := team.ID
	return &id, nil
}

func (s *OrderService) joinOrCreateTeam(tx *gorm.DB, accountID, orderID uint64, product model.Product, gb model.GroupBuy, teamID *uint64, startNewTeam bool, actGB *ActivityGroupBuyConfig, activityID *uint64, activityProductID *uint64, now time.Time) (uint64, error) {
	target := uint32(2)
	if actGB != nil {
		if actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
	} else if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
		target = *product.GroupBuyTargetCount
	} else if gb.TargetCount >= 2 {
		target = gb.TargetCount
	}

	var resolveTeamID *uint64
	if startNewTeam {
		resolveTeamID = nil
	} else if teamID != nil && *teamID > 0 {
		resolveTeamID = teamID
	}

	if resolveTeamID != nil {
		var team model.GroupBuyTeam
		// FOR UPDATE 行锁，串行化并发 join，避免 current_count 自增与成团判断竞态
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND group_buy_id = ? AND status = ?", *resolveTeamID, gb.ID, model.GroupBuyTeamPending).
			First(&team).Error; err != nil {
			return 0, ErrGroupBuyInvalid
		}
		// 本单已入团则幂等返回（支付回调重试）。用 Count 避免 First 未命中污染 tx.Error。
		var existingCnt int64
		if err := query.NotDeleted(tx.Model(&model.GroupBuyMember{})).
			Where("team_id = ? AND order_id = ?", team.ID, orderID).
			Count(&existingCnt).Error; err != nil {
			return 0, err
		}
		if existingCnt > 0 {
			return team.ID, nil
		}
		joinCount, err := countUserTeamJoins(tx, accountID, team.ID, activityID, orderID)
		if err != nil {
			return 0, err
		}
		if err := assertTeamMatchesActivityProduct(tx, team.ID, activityProductID); err != nil {
			return 0, err
		}
		allowRepeat, maxJoins := resolveGroupBuyRepeat(product, actGB)
		if err := validateTeamJoinLimit(joinCount, allowRepeat, maxJoins); err != nil {
			return 0, err
		}
		paidCount, err := countPaidPendingGroupOnTeam(tx, team.ID, orderID)
		if err != nil {
			return 0, err
		}
		if paidCount >= team.TargetCount {
			return 0, ErrGroupBuyInvalid
		}
		if err := tx.Model(&team).Update("current_count", gorm.Expr("current_count + 1")).Error; err != nil {
			return 0, err
		}
		if err := ensureGroupBuyMember(tx, team.ID, orderID, accountID, false); err != nil {
			return 0, err
		}
		return team.ID, nil
	}

	var ap *model.ActivityProduct
	if activityProductID != nil {
		var loaded model.ActivityProduct
		if err := query.NotDeleted(tx).First(&loaded, *activityProductID).Error; err == nil {
			ap = &loaded
		}
	}
	expire := computeGroupExpireAt(now, ap)

	if err := assertCanStartNewTeam(tx, product, gb, actGB, activityID, activityProductID); err != nil {
		return 0, err
	}

	team := model.GroupBuyTeam{
		GroupBuyID: gb.ID, LeaderID: accountID, TargetCount: target,
		CurrentCount: 1, Status: model.GroupBuyTeamPending, ExpireAt: expire,
	}
	if err := tx.Create(&team).Error; err != nil {
		return 0, err
	}
	if err := ensureGroupBuyMember(tx, team.ID, orderID, accountID, true); err != nil {
		return 0, err
	}
	return team.ID, nil
}

// resolveClientGroupBuy 接受客户端 group_buy_id 时必须属于当前商品且启用中，防止跨品拼团劫持。
func resolveClientGroupBuy(db *gorm.DB, productID uint64, ensured model.GroupBuy, clientID *uint64) (model.GroupBuy, error) {
	if clientID == nil || *clientID == 0 || *clientID == ensured.ID {
		return ensured, nil
	}
	var byID model.GroupBuy
	if err := query.NotDeleted(db).First(&byID, *clientID).Error; err != nil {
		return model.GroupBuy{}, ErrGroupBuyInvalid
	}
	if byID.ProductID != productID || byID.Status != 1 {
		return model.GroupBuy{}, ErrGroupBuyInvalid
	}
	return byID, nil
}

// ensureActiveGroupBuy 保证商品有可用的 group_buy 行（活动拼团也可能未同步商品拼团配置）。
func (s *OrderService) ensureActiveGroupBuy(product model.Product, actGB *ActivityGroupBuyConfig) (*model.GroupBuy, error) {
	target := uint32(2)
	price := 0.0
	if actGB != nil && actGB.EnableGroupBuy == 1 {
		if actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
		price = actGB.GroupBuyPrice
	} else if product.EnableGroupBuy == 1 && product.GroupBuyPrice != nil {
		price = *product.GroupBuyPrice
		if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
			target = *product.GroupBuyTargetCount
		}
	} else {
		return nil, ErrGroupBuyInvalid
	}
	if price <= 0 {
		return nil, ErrGroupBuyInvalid
	}

	now := time.Now()
	endAt := now.AddDate(10, 0, 0)
	var gb model.GroupBuy
	err := query.NotDeleted(s.DB).Where("product_id = ?", product.ID).First(&gb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		gb = model.GroupBuy{
			ProductID: product.ID, TargetCount: target, GroupPrice: price,
			StartAt: now, EndAt: endAt, Status: 1,
		}
		if err := s.DB.Create(&gb).Error; err != nil {
			return nil, err
		}
		return &gb, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.DB.Model(&gb).Updates(map[string]interface{}{
		"target_count": target,
		"group_price":  price,
		"status":       1,
		"end_at":       endAt,
	}).Error; err != nil {
		return nil, err
	}
	gb.TargetCount = target
	gb.GroupPrice = price
	gb.Status = 1
	return &gb, nil
}

func ensureGroupBuyMember(tx *gorm.DB, teamID, orderID, accountID uint64, isLeader bool) error {
	// Count 代替 First：避免 record not found 把 tx.Error 污染后导致后续 Create 变成 no-op。
	var n int64
	if err := query.NotDeleted(tx.Model(&model.GroupBuyMember{})).
		Where("team_id = ? AND order_id = ?", teamID, orderID).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	leader := uint8(0)
	if isLeader {
		leader = 1
	}
	m := model.GroupBuyMember{
		TeamID: teamID, OrderID: orderID, AccountID: accountID,
		IsLeader: leader, JoinedAt: time.Now(),
	}
	if err := tx.Create(&m).Error; err != nil {
		// 并发下可能撞 uk_team_order，再确认一次即可
		if isMySQLDuplicateKey(err) {
			return nil
		}
		return err
	}
	return nil
}

func resolveGroupBuyRepeat(product model.Product, actGB *ActivityGroupBuyConfig) (uint8, uint32) {
	if actGB != nil {
		return actGB.GroupBuyAllowRepeat, actGB.GroupBuyMaxJoinsPerUser
	}
	return product.GroupBuyAllowRepeat, 0 // product 无 max_joins 列：开重复时 0=不限
}

func groupCompleteReady(currentCount, targetCount, distinctAccounts uint32, allowRepeat uint8) bool {
	if currentCount < targetCount {
		return false
	}
	if allowRepeat == 1 {
		return true
	}
	return distinctAccounts >= targetCount
}

func validateTeamJoinLimit(existingJoins int64, allowRepeat uint8, maxJoins uint32) error {
	if allowRepeat != 1 {
		if existingJoins > 0 {
			return ErrGroupBuyAlreadyJoined
		}
		return nil
	}
	if maxJoins > 0 && uint32(existingJoins) >= maxJoins {
		return ErrGroupBuyAlreadyJoined
	}
	return nil
}

func (s *OrderService) tryCompleteGroup(tx *gorm.DB, teamID *uint64, product model.Product, actGB *ActivityGroupBuyConfig) error {
	if teamID == nil {
		return nil
	}
	var team model.GroupBuyTeam
	// FOR UPDATE 行锁，让成团判断与状态更新串行化，避免并发重复成团
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&team, *teamID).Error; err != nil {
		return err
	}
	if team.Status != model.GroupBuyTeamPending {
		// 已被并发置为 Success/Failed，直接返回
		return nil
	}

	// 成团门槛只计「已支付」的待成团单，避免未付款参团直接入包
	paidCount, err := countPaidPendingGroupOnTeam(tx, team.ID, 0)
	if err != nil {
		return err
	}
	allowRepeat, _ := resolveGroupBuyRepeat(product, actGB)
	var distinct uint32
	if allowRepeat != 1 {
		d, err := countDistinctPaidPendingGroupOnTeam(tx, team.ID)
		if err != nil {
			return err
		}
		distinct = d
	}
	if !groupCompleteReady(paidCount, team.TargetCount, distinct, allowRepeat) {
		return nil
	}

	now := time.Now()
	// 加 status=Pending 守卫，幂等兜底防重复成团
	res := tx.Model(&team).Where("status = ?", model.GroupBuyTeamPending).
		Updates(map[string]interface{}{
			"status":     model.GroupBuyTeamSuccess,
			"success_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 已被并发成团，无需重复推进订单
		return nil
	}
	// 只推进已支付的待成团单；未支付的仍留在 PendingGroup，等付款后再走支付回调成团推进
	if err := tx.Model(&model.Order{}).
		Where("status = ? AND pay_status = ? AND id IN (SELECT order_id FROM order_item WHERE group_buy_team_id = ? AND is_deleted = 0)",
			model.OrderStatusPendingGroup, model.PayStatusPaid, team.ID).
		Updates(map[string]interface{}{
			"status":                model.OrderStatusPendingFulfill,
			"merchant_review_stage": model.MerchantReviewPending,
		}).Error; err != nil {
		return err
	}
	// 成团后对开启自动审核的商家逐单入背包（仅已推进的已付单）
	var orderIDs []uint64
	if err := tx.Raw(`
		SELECT DISTINCT oi.order_id
		FROM order_item oi
		INNER JOIN `+"`order`"+` o ON o.id = oi.order_id AND o.is_deleted = 0
		WHERE oi.group_buy_team_id = ? AND oi.is_deleted = 0
		  AND o.pay_status = ? AND o.status = ?
	`, team.ID, model.PayStatusPaid, model.OrderStatusPendingFulfill).Scan(&orderIDs).Error; err != nil {
		return err
	}
	for _, oid := range orderIDs {
		if err := s.maybeAutoApproveInTx(tx, oid); err != nil {
			return err
		}
	}
	return nil
}

// tryCompleteGroupForOrderInTx 拼团单支付成功后：先正式入团，再尝试成团。
func (s *OrderService) tryCompleteGroupForOrderInTx(tx *gorm.DB, orderID uint64) error {
	var order model.Order
	if err := query.NotDeleted(tx).First(&order, orderID).Error; err != nil {
		return err
	}
	if order.Status != model.OrderStatusPendingGroup || order.PayStatus != model.PayStatusPaid {
		return nil
	}
	var item model.OrderItem
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND purchase_type = ?", orderID, model.PurchaseTypeGroup).
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var product model.Product
	if err := query.NotDeleted(tx).First(&product, item.ProductID).Error; err != nil {
		return err
	}
	var actGB *ActivityGroupBuyConfig
	if item.ActivityProductID != nil {
		var ap model.ActivityProduct
		if err := query.NotDeleted(tx).First(&ap, *item.ActivityProductID).Error; err == nil && ap.EnableGroupBuy == 1 {
			target := uint32(2)
			if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
				target = *ap.GroupBuyTargetCount
			}
			price := ap.ActivityPrice
			if ap.GroupBuyPrice != nil {
				price = *ap.GroupBuyPrice
			}
			actGB = &ActivityGroupBuyConfig{
				EnableGroupBuy:             1,
				GroupBuyPrice:              price,
				GroupBuyTargetCount:        target,
				GroupBuyAllowRepeat:        ap.GroupBuyAllowRepeat,
				GroupBuyMaxJoinsPerUser:    ap.GroupBuyMaxJoinsPerUser,
				GroupBuyMaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
			}
		}
	}

	gbID := item.GroupBuyID
	var gb model.GroupBuy
	if gbID != nil && *gbID > 0 {
		if err := query.NotDeleted(tx).First(&gb, *gbID).Error; err != nil {
			return err
		}
	} else {
		ensured, err := s.ensureActiveGroupBuy(product, actGB)
		if err != nil {
			return err
		}
		gb = *ensured
		gbID = &gb.ID
		if err := tx.Model(&item).Update("group_buy_id", gb.ID).Error; err != nil {
			return err
		}
	}

	// 付款后才占名额 / 写 member；若意向团已满则新开一团
	startNew := item.GroupBuyTeamID == nil || *item.GroupBuyTeamID == 0
	intended := item.GroupBuyTeamID
	teamID, err := s.joinOrCreateTeam(tx, order.AccountID, orderID, product, gb, intended, startNew, actGB, item.ActivityID, item.ActivityProductID, time.Now())
	if err != nil {
		if !startNew {
			// 原团不可入：降级为新开一团，避免已付款无法入团
			teamID, err = s.joinOrCreateTeam(tx, order.AccountID, orderID, product, gb, nil, true, actGB, item.ActivityID, item.ActivityProductID, time.Now())
		}
		if err != nil {
			return err
		}
	}
	if item.GroupBuyTeamID == nil || *item.GroupBuyTeamID != teamID {
		if err := tx.Model(&item).Update("group_buy_team_id", teamID).Error; err != nil {
			return err
		}
	}
	return s.tryCompleteGroup(tx, &teamID, product, actGB)
}

func (s *OrderService) Cancel(accountID, orderID uint64) error {
	order, err := s.getUserOrder(accountID, orderID)
	if err != nil {
		return err
	}
	isPackageParent := order.PackageProductID != nil && order.ParentOrderID == nil && order.MerchantID == 0
	if order.Status != model.OrderStatusPendingPay && order.Status != model.OrderStatusPendingGroup {
		if order.Status == model.OrderStatusPendingFulfill &&
			(order.MerchantReviewStage == model.MerchantReviewPending ||
				(isPackageParent && order.MerchantReviewStage == model.MerchantReviewNone)) {
			// allow cancel before merchant review / 套餐父单
		} else {
			return ErrOrderStatusInvalid
		}
	}
	// 子单不可单独取消，须取消父单级联
	if order.ParentOrderID != nil {
		return fmt.Errorf("%w: 请取消套餐父订单", ErrOrderStatusInvalid)
	}
	return s.runTx(func(tx *gorm.DB) error {
		// 成团成功后禁止用户取消（只能商家拒单），避免团状态被打回
		okGroup, err := orderHasSuccessfulGroup(tx, orderID)
		if err != nil {
			return err
		}
		if okGroup {
			return fmt.Errorf("%w: 已成团订单不可取消，请联系商家", ErrOrderStatusInvalid)
		}
		if err := rollbackGroupTeamForOrder(tx, orderID); err != nil {
			return err
		}
		if s.CouponSvc != nil {
			if err := s.CouponSvc.ReleaseByOrderInTx(tx, order); err != nil {
				return err
			}
		}
		if s.InventorySvc != nil {
			if err := s.InventorySvc.RollbackOrderCredit(tx, orderID); err != nil {
				if errors.Is(err, ErrInventoryRollback) {
					return fmt.Errorf("%w: 商品已使用，无法取消", ErrInventoryRollback)
				}
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
		if err := s.refundPaymentInTx(tx, orderID); err != nil {
			return err
		}
		return tx.Model(order).Update("status", model.OrderStatusCancelled).Error
	})
}

func (s *OrderService) RequestUse(accountID, orderID uint64, input RequestUseInput) (*OrderView, error) {
	order, err := s.getUserOrder(accountID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewApproved {
		return nil, ErrOrderStatusInvalid
	}
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
		if err := s.ZoneSvc.ValidateDelivery(accountID, order.MerchantID, deliveryType, coordIn); err != nil {
			return nil, err
		}
	}

	updates := map[string]interface{}{
		"delivery_type":         deliveryType,
		"merchant_review_stage": model.MerchantReviewPendingUse,
	}
	if deliveryType == model.DeliveryTypeDelivery {
		if input.AddressID == nil {
			return nil, ErrAddressRequired
		}
		var addr model.UserAddress
		if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
			return nil, ErrAddressRequired
		}
		updates["address_snapshot"] = AddressSnapshotFromUserAddress(&addr)
	}
	if input.Remark != nil {
		updates["remark"] = *input.Remark
	}
	if err := s.DB.Model(order).Updates(updates).Error; err != nil {
		return nil, err
	}
	// 自取/外卖均自动通过审核：自取直接生成核销码，外卖进备餐出餐流程
	return s.MerchantUseReview(order.MerchantID, orderID, true)
}

func (s *OrderService) ConfirmPickup(accountID, orderID uint64) (*OrderView, error) {
	order, err := s.getUserOrder(accountID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingVerify {
		return nil, ErrOrderStatusInvalid
	}
	if err := s.DB.Model(order).Update("status", model.OrderStatusCompleted).Error; err != nil {
		return nil, err
	}
	return s.GetView(accountID, orderID, nil)
}

func (s *OrderService) GetView(accountID, orderID uint64, merchantID *uint64) (*OrderView, error) {
	order, err := s.getOrderScoped(accountID, orderID, merchantID)
	if err != nil {
		return nil, err
	}
	if err := query.NotDeleted(s.DB).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		Preload("Children", "is_deleted = ?", model.NotDeleted).
		Preload("Children.Items", "is_deleted = ?", model.NotDeleted).
		First(order, order.ID).Error; err != nil {
		return nil, err
	}
	view := toOrderView(order)
	s.attachVerifyCode(&view)
	s.attachPickupCode(&view)
	s.attachItemRefundQuantities(&view)
	s.attachGroupBuyProgress(&view, accountID)
	if merchantID != nil || accountID == 0 {
		s.enrichBuyer(&view)
	}
	return &view, nil
}

func (s *OrderService) List(accountID uint64, merchantID *uint64, page, pageSize int, status *uint8, statusCode string, buyerAccountID *uint64, accountIDs []uint64, startDate, endDate *time.Time) ([]OrderView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	// 商家查看待审列表时：若已开自动审核，顺带修复卡在待审的脏数据（入背包并标已通过）
	if merchantID != nil && statusCode == "pending_merchant" {
		_, _ = s.AutoApprovePendingForMerchant(*merchantID)
	}

	q := query.NotDeleted(s.DB.Model(&model.Order{}))
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	} else if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
		// 用户端只展示顶层订单（套餐父单 / 普通单），子单挂在父单 children
		q = q.Where("parent_order_id IS NULL")
	}
	if len(accountIDs) > 0 {
		q = q.Where("account_id IN ?", accountIDs)
	} else if buyerAccountID != nil {
		q = q.Where("account_id = ?", *buyerAccountID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if startDate != nil {
		q = q.Where("created_at >= ?", *startDate)
	}
	if endDate != nil {
		q = q.Where("created_at < ?", *endDate)
	}
	applyStatusCodeFilter(q, statusCode)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []model.Order
	if err := q.Preload("Items", "is_deleted = ?", model.NotDeleted).
		Preload("Children", "is_deleted = ?", model.NotDeleted).
		Preload("Children.Items", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	views := make([]OrderView, 0, len(orders))
	for i := range orders {
		view := toOrderView(&orders[i])
		s.attachVerifyCode(&view)
		s.attachPickupCode(&view)
		s.attachItemRefundQuantities(&view)
		s.attachGroupBuyProgress(&view, accountID)
		views = append(views, view)
	}
	if merchantID != nil || accountID == 0 {
		s.enrichBuyers(views)
	}
	return views, total, nil
}

func (s *OrderService) enrichBuyer(view *OrderView) {
	var acc model.Account
	if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").
		First(&acc, view.AccountID).Error; err != nil {
		return
	}
	view.Buyer = &BuyerBrief{AccountID: acc.ID, Nickname: acc.Nickname, Phone: acc.Phone}
}

func (s *OrderService) enrichBuyers(views []OrderView) {
	if len(views) == 0 {
		return
	}
	ids := make([]uint64, 0, len(views))
	seen := make(map[uint64]struct{}, len(views))
	for i := range views {
		id := views[i].AccountID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	var accounts []model.Account
	if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").
		Where("id IN ?", ids).Find(&accounts).Error; err != nil {
		return
	}
	byID := make(map[uint64]BuyerBrief, len(accounts))
	for _, acc := range accounts {
		byID[acc.ID] = BuyerBrief{AccountID: acc.ID, Nickname: acc.Nickname, Phone: acc.Phone}
	}
	for i := range views {
		if b, ok := byID[views[i].AccountID]; ok {
			bCopy := b
			views[i].Buyer = &bCopy
		}
	}
}

func applyStatusCodeFilter(q *gorm.DB, code string) {
	switch code {
	case "pending_pay":
		q.Where("status = ?", model.OrderStatusPendingPay)
	case "pending_group":
		q.Where("status = ?", model.OrderStatusPendingGroup)
	case "pending_merchant":
		q.Where("status = ? AND merchant_review_stage = ?", model.OrderStatusPendingFulfill, model.MerchantReviewPending)
	case "approved":
		q.Where("status = ? AND merchant_review_stage = ?", model.OrderStatusPendingFulfill, model.MerchantReviewApproved)
	case "pending_use_merchant":
		q.Where("status = ? AND merchant_review_stage = ?", model.OrderStatusPendingFulfill, model.MerchantReviewPendingUse)
	case "ready_pickup":
		q.Where("status = ?", model.OrderStatusPendingVerify)
	case "pending_rider":
		q.Where("status = ?", model.OrderStatusPendingShip)
	case "delivering":
		q.Where("status = ?", model.OrderStatusShipping)
	case "completed":
		q.Where("status = ?", model.OrderStatusCompleted)
	case "cancelled":
		q.Where("status = ?", model.OrderStatusCancelled)
	case "closed":
		q.Where("status = ?", model.OrderStatusClosed)
	case "preparing":
		q.Where("status = ?", model.OrderStatusPreparing)
	}
}

func (s *OrderService) MerchantReview(merchantID, orderID uint64, approve bool, rejectReason *string) (*OrderView, error) {
	order, err := s.getOrderScoped(0, orderID, &merchantID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewPending {
		return nil, ErrOrderStatusInvalid
	}
	if !approve {
		err := s.runTx(func(tx *gorm.DB) error {
			if s.CouponSvc != nil {
				if err := s.CouponSvc.ReleaseByOrderInTx(tx, order); err != nil {
					return err
				}
			}
			if s.InventorySvc != nil {
				if err := s.InventorySvc.RollbackOrderCredit(tx, orderID); err != nil {
					if errors.Is(err, ErrInventoryRollback) {
						return fmt.Errorf("%w: 商品已使用无法拒绝退款；若只是卡在待审请点「通过」", ErrInventoryRollback)
					}
					return err
				}
			}
			if err := restoreProductStockForOrder(tx, orderID); err != nil {
				return err
			}
			if s.ActivitySvc != nil {
				if err := s.ActivitySvc.RollbackSoldInTx(tx, orderID); err != nil {
					return err
				}
			}
			if err := s.refundPaymentInTx(tx, orderID); err != nil {
				return err
			}
			reasonText := "商家拒单"
			if rejectReason != nil {
				r := strings.TrimSpace(*rejectReason)
				if r != "" {
					if strings.HasPrefix(r, "商家拒单") {
						reasonText = r
					} else {
						reasonText = "商家拒单：" + r
					}
				}
			}
			if err := tx.Model(order).Updates(map[string]interface{}{
				"merchant_review_stage": model.MerchantReviewRejected,
				"status":                model.OrderStatusCancelled,
				"remark":                reasonText,
			}).Error; err != nil {
				return err
			}
			mid := merchantID
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectOrder,
				SubjectID:   orderID,
				EventCode:   model.EventMerchantRejected,
				ActorRole:   model.FulfillmentActorMerchant,
				ActorID:     &mid,
				Title:       "商家已拒单",
				Detail:      map[string]interface{}{"reason": reasonText},
			})
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectOrder,
				SubjectID:   orderID,
				EventCode:   model.EventRefundRequested,
				ActorRole:   model.FulfillmentActorSystem,
				Title:       "退款已发起",
				Detail:      map[string]interface{}{"reason": reasonText},
			})
			return nil
		})
		if err != nil {
			// 自动审核店铺的脏待审单：拒绝失败时尝试直接标为已通过，移出审核队列
			if errors.Is(err, ErrInventoryRollback) {
				if healErr := s.healPendingIfAutoApprove(merchantID, orderID); healErr == nil {
					return s.GetView(0, orderID, &merchantID)
				}
			}
			return nil, err
		}
		return s.GetView(0, orderID, &merchantID)
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var items []model.OrderItem
		if err := query.NotDeleted(tx).Where("order_id = ?", orderID).Find(&items).Error; err != nil {
			return err
		}
		if err := s.creditOrderInventory(tx, order.AccountID, orderID, items); err != nil {
			return err
		}
		if s.InventorySvc != nil {
			if err := s.InventorySvc.AutoPickupAfterCredit(tx, order.AccountID, orderID, resolveUsageMerchantID(order)); err != nil {
				return err
			}
		}
		if err := tx.Model(order).Update("merchant_review_stage", model.MerchantReviewApproved).Error; err != nil {
			return err
		}
		mid := merchantID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectOrder,
			SubjectID:   orderID,
			EventCode:   model.EventMerchantApproved,
			ActorRole:   model.FulfillmentActorMerchant,
			ActorID:     &mid,
			Title:       "商家已通过，已生成待核销",
		})
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectOrder,
			SubjectID:   orderID,
			EventCode:   model.EventInventoryCredited,
			ActorRole:   model.FulfillmentActorSystem,
			Title:       "商品已入待核销",
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(0, orderID, &merchantID)
}

func (s *OrderService) MerchantUseReview(merchantID, orderID uint64, approve bool) (*OrderView, error) {
	order, err := s.getOrderScoped(0, orderID, &merchantID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewPendingUse {
		return nil, ErrOrderStatusInvalid
	}
	if !approve {
		return nil, s.DB.Model(order).Update("merchant_review_stage", model.MerchantReviewApproved).Error
	}

	var items []model.OrderItem
	query.NotDeleted(s.DB).Where("order_id = ?", orderID).Find(&items)

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.creditOrderInventory(tx, order.AccountID, orderID, items); err != nil {
			return err
		}

		var product model.Product
		if len(items) > 0 {
			if err := query.NotDeleted(tx).First(&product, items[0].ProductID).Error; err != nil {
				return ErrProductNotFound
			}
		}

		deliveryType, err := normalizeDeliveryType(order.DeliveryType)
		if err != nil {
			return err
		}
		if deliveryType != order.DeliveryType {
			if err := tx.Model(order).Update("delivery_type", deliveryType).Error; err != nil {
				return err
			}
		}

		if deliveryType == model.DeliveryTypeDelivery && product.ItemType == model.ProductItemTypePhysical {
			if s.ZoneSvc != nil {
				if err := s.ZoneSvc.ValidateDelivery(order.AccountID, order.MerchantID, deliveryType, DeliveryCoordinateInput{
					AddressSnapshot: order.AddressSnapshot,
				}); err != nil {
					return err
				}
			}
		}

		// 购买时已入背包，此处仅扣减商家库存并完结购买订单；自提/配送核销走背包使用单
		nextStatus := model.OrderStatusCompleted
		if deliveryType == model.DeliveryTypeDelivery && product.ItemType == model.ProductItemTypePhysical {
			nextStatus = model.OrderStatusPreparing // 备餐中，商家确认出餐后推进到 PendingShip
			// 配送费/骑手收益从商家配置读取快照（与背包使用链路 inventory.go 一致），
			// 不能取 order.DeliveryFee/RiderEarnings--下单时未写入，恒为 0
			var merchant model.MerchantProfile
			if err := query.NotDeleted(tx).First(&merchant, order.MerchantID).Error; err != nil {
				return ErrMerchantNotFound
			}
			orderID := order.ID
			d := model.DeliveryOrder{
				OrderID:          &orderID,
				Status:           model.DeliveryPendingAccept,
				MerchantPrepared: 0, // 备餐中，商家确认出餐后置 1，骑手才可见
				PickupCode:       genPickupCode(tx, order.MerchantID),
				DeliveryFee:      merchant.DeliveryFee,
				RiderEarnings:    merchant.RiderEarnings,
			}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
		}

		return tx.Model(order).Updates(map[string]interface{}{
			"status":                nextStatus,
			"merchant_review_stage": model.MerchantReviewUseApproved,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(0, orderID, &merchantID)
}

func (s *OrderService) GetGroupProgress(accountID, productID uint64, teamID *uint64) (*GroupBuyProgress, error) {
	var product model.Product
	if err := query.NotDeleted(s.DB).First(&product, productID).Error; err != nil {
		return nil, ErrProductNotFound
	}

	var gb model.GroupBuy
	if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", productID).First(&gb).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	resolvedTeamID := teamID
	if resolvedTeamID == nil && accountID > 0 {
		if userTeamID, err := s.findUserPendingTeamID(accountID, productID); err != nil {
			return nil, err
		} else if userTeamID != nil {
			resolvedTeamID = userTeamID
		}
	}

	var team model.GroupBuyTeam
	q := query.NotDeleted(s.DB).Where("group_buy_id = ?", gb.ID)
	if resolvedTeamID != nil {
		q = q.Where("id = ?", *resolvedTeamID)
	} else {
		q = q.Where("status = ?", model.GroupBuyTeamPending)
	}
	if err := q.Order("id DESC").First(&team).Error; err != nil {
		// 商品已启用拼团但暂无待成团 team（全新商品或所有团均已结束）：
		// 不报错，返回未开团的空进度，让前端可正常展示拼团价/目标人数。
		return s.buildEmptyGroupBuyProgress(&product, &gb, nil), nil
	}

	return s.buildGroupBuyProgress(&product, &gb, &team, accountID, nil)
}

func (s *OrderService) GetActivityGroupProgress(accountID, activityID, activityProductID uint64, teamID *uint64) (*GroupBuyProgress, error) {
	if s.ActivitySvc == nil {
		return nil, ErrGroupBuyInvalid
	}
	view, err := s.ActivitySvc.GetStoreProduct(activityID, activityProductID)
	if err != nil {
		return nil, err
	}
	if !view.CanGroupBuy {
		return nil, ErrGroupBuyInvalid
	}
	ap := view.ActivityProduct
	var prod model.Product
	if err := query.NotDeleted(s.DB).First(&prod, ap.ProductID).Error; err != nil {
		return nil, ErrProductNotFound
	}
	var gb model.GroupBuy
	if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", ap.ProductID).First(&gb).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	target := uint32(2)
	if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
		target = *ap.GroupBuyTargetCount
	}
	maxJoins := ap.GroupBuyMaxJoinsPerUser
	groupPrice := ap.ActivityPrice
	if ap.GroupBuyPrice != nil {
		groupPrice = *ap.GroupBuyPrice
	}
	actGB := &ActivityGroupBuyConfig{
		EnableGroupBuy:             1,
		GroupBuyPrice:              groupPrice,
		GroupBuyTargetCount:        target,
		GroupBuyAllowRepeat:        ap.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser:    maxJoins,
		GroupBuyMaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
	}

	resolvedTeamID := teamID
	if resolvedTeamID == nil && accountID > 0 {
		if userTeamID, err := findUserPendingTeamInGroupBuy(s.DB, accountID, gb.ID, &activityID, &activityProductID); err != nil {
			return nil, err
		} else if userTeamID != nil {
			resolvedTeamID = userTeamID
		}
	}
	if resolvedTeamID == nil {
		if latestTeamID, err := findLatestActivityPendingTeam(s.DB, gb.ID, activityID, activityProductID); err != nil {
			return nil, err
		} else if latestTeamID != nil {
			resolvedTeamID = latestTeamID
		}
	}

	if resolvedTeamID == nil {
		return s.buildEmptyGroupBuyProgress(&prod, &gb, actGB), nil
	}

	var team model.GroupBuyTeam
	if err := query.NotDeleted(s.DB).Where("group_buy_id = ? AND id = ?", gb.ID, *resolvedTeamID).First(&team).Error; err != nil {
		return s.buildEmptyGroupBuyProgress(&prod, &gb, actGB), nil
	}

	progress, err := s.buildGroupBuyProgress(&prod, &gb, &team, accountID, actGB)
	if err != nil {
		return nil, err
	}
	if accountID > 0 {
		count, err := countUserTeamJoins(s.DB, accountID, team.ID, &activityID, 0)
		if err != nil {
			return nil, err
		}
		progress.UserJoinCount = uint32(count)
		progress.UserJoined = count > 0
	}
	return progress, nil
}

func (s *OrderService) ListJoinableTeams(productID uint64, activityID, activityProductID *uint64, limit int) (*JoinableTeamsResult, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	var actGB *ActivityGroupBuyConfig
	if activityID != nil && activityProductID != nil {
		if s.ActivitySvc == nil {
			return nil, ErrGroupBuyInvalid
		}
		view, err := s.ActivitySvc.GetStoreProduct(*activityID, *activityProductID)
		if err != nil {
			return nil, err
		}
		if !view.CanGroupBuy {
			return nil, ErrGroupBuyInvalid
		}
		if view.ActivityProduct.ProductID != productID {
			return nil, ErrGroupBuyInvalid
		}
		ap := view.ActivityProduct
		target := uint32(2)
		if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
			target = *ap.GroupBuyTargetCount
		}
		groupPrice := ap.ActivityPrice
		if ap.GroupBuyPrice != nil {
			groupPrice = *ap.GroupBuyPrice
		}
		actGB = &ActivityGroupBuyConfig{
			EnableGroupBuy:             1,
			GroupBuyPrice:              groupPrice,
			GroupBuyTargetCount:        target,
			GroupBuyAllowRepeat:        ap.GroupBuyAllowRepeat,
			GroupBuyMaxJoinsPerUser:    ap.GroupBuyMaxJoinsPerUser,
			GroupBuyMaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
		}
	} else if activityID != nil || activityProductID != nil {
		return nil, ErrGroupBuyInvalid
	}

	var product model.Product
	if err := query.NotDeleted(s.DB).First(&product, productID).Error; err != nil {
		return nil, ErrProductNotFound
	}

	var gb model.GroupBuy
	if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", productID).First(&gb).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	now := time.Now()
	type teamRow struct {
		ID           uint64
		CurrentCount uint32
		TargetCount  uint32
		ExpireAt     time.Time
		LeaderID     uint64
	}

	var rows []teamRow
	// 可参团列表按「已支付待成团」人数展示，未付款不占名额
	paidCountExpr := `(
		SELECT COUNT(*) FROM order_item oi2
		INNER JOIN ` + "`order`" + ` o2 ON o2.id = oi2.order_id AND o2.is_deleted = 0
		WHERE oi2.group_buy_team_id = t.id AND oi2.is_deleted = 0
		  AND o2.status = ? AND o2.pay_status = ?
	)`
	q := s.DB.Table("group_buy_team t").
		Select("t.id, "+paidCountExpr+" AS current_count, t.target_count, t.expire_at, t.leader_id",
			model.OrderStatusPendingGroup, model.PayStatusPaid).
		Where("t.is_deleted = ? AND t.group_buy_id = ? AND t.status = ?", model.NotDeleted, gb.ID, model.GroupBuyTeamPending).
		Where(paidCountExpr+" < t.target_count", model.OrderStatusPendingGroup, model.PayStatusPaid).
		Where("t.expire_at > ?", now)

	if activityID != nil {
		q = q.
			Joins("JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
			Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
			Where("o.status = ?", model.OrderStatusPendingGroup)
		q = applyActivityTeamScope(q, activityID, activityProductID)
	} else {
		q = q.
			Joins("JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
			Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
			Where("oi.activity_id IS NULL AND o.status = ?", model.OrderStatusPendingGroup)
	}

	if err := q.Group("t.id").
		Order("current_count DESC, t.expire_at ASC, t.id ASC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	leaderIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		leaderIDs = append(leaderIDs, r.LeaderID)
	}
	leaderNames := loadAccountNicknames(s.DB, leaderIDs)

	out := make([]JoinableGroupTeamView, 0, len(rows))
	for _, r := range rows {
		view := JoinableGroupTeamView{
			TeamID:       r.ID,
			CurrentCount: r.CurrentCount,
			TargetCount:  r.TargetCount,
			Remain:       r.TargetCount - r.CurrentCount,
			ExpireAt:     r.ExpireAt.Format(time.RFC3339),
		}
		if name := leaderNames[r.LeaderID]; name != "" {
			view.LeaderName = name
		}
		out = append(out, view)
	}

	canStart, maxTeams, concurrentCount, err := buildConcurrentTeamMeta(s.DB, product, gb, actGB, activityID, activityProductID)
	if err != nil {
		return nil, err
	}
	return &JoinableTeamsResult{
		Teams:               out,
		CanStartNewTeam:     canStart,
		MaxConcurrentTeams:  maxTeams,
		ConcurrentTeamCount: concurrentCount,
	}, nil
}

func (s *OrderService) attachGroupBuyProgress(view *OrderView, accountID uint64) {
	if view.Status != model.OrderStatusPendingGroup {
		return
	}
	var item model.OrderItem
	for i := range view.Items {
		if view.Items[i].GroupBuyTeamID != nil {
			item = view.Items[i]
			break
		}
	}
	if item.GroupBuyTeamID == nil {
		return
	}

	var product model.Product
	if err := query.NotDeleted(s.DB).First(&product, item.ProductID).Error; err != nil {
		return
	}
	var gb model.GroupBuy
	if item.GroupBuyID != nil {
		if err := query.NotDeleted(s.DB).First(&gb, *item.GroupBuyID).Error; err != nil {
			return
		}
	} else if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", item.ProductID).First(&gb).Error; err != nil {
		return
	}
	var team model.GroupBuyTeam
	if err := query.NotDeleted(s.DB).First(&team, *item.GroupBuyTeamID).Error; err != nil {
		return
	}
	var actGB *ActivityGroupBuyConfig
	if item.ActivityProductID != nil && s.ActivitySvc != nil {
		if ap, err := s.ActivitySvc.GetActivityProduct(*item.ActivityProductID, nil); err == nil && ap.EnableGroupBuy == 1 && ap.GroupBuyPrice != nil {
			target := uint32(2)
			if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
				target = *ap.GroupBuyTargetCount
			}
			maxJoins := ap.GroupBuyMaxJoinsPerUser
			actGB = &ActivityGroupBuyConfig{
				EnableGroupBuy:             1,
				GroupBuyPrice:              *ap.GroupBuyPrice,
				GroupBuyTargetCount:        target,
				GroupBuyAllowRepeat:        ap.GroupBuyAllowRepeat,
				GroupBuyMaxJoinsPerUser:    maxJoins,
				GroupBuyMaxConcurrentTeams: ap.GroupBuyMaxConcurrentTeams,
			}
		}
	}
	progress, err := s.buildGroupBuyProgress(&product, &gb, &team, accountID, actGB)
	if err != nil {
		return
	}
	view.GroupBuyProgress = progress
}

func (s *OrderService) buildGroupBuyProgress(product *model.Product, gb *model.GroupBuy, team *model.GroupBuyTeam, accountID uint64, actGB *ActivityGroupBuyConfig) (*GroupBuyProgress, error) {
	text := "拼团中"
	switch team.Status {
	case model.GroupBuyTeamSuccess:
		text = "已成团"
	case model.GroupBuyTeamFailed:
		text = "已失败"
	}

	allowRepeat := product.GroupBuyAllowRepeat
	groupPrice := gb.GroupPrice
	target := team.TargetCount
	if actGB != nil {
		allowRepeat = actGB.GroupBuyAllowRepeat
		groupPrice = actGB.GroupBuyPrice
		if target == 0 && actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
	}
	if target == 0 {
		if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
			target = *product.GroupBuyTargetCount
		} else if gb.TargetCount >= 2 {
			target = gb.TargetCount
		} else {
			target = 2
		}
	}

	current := team.CurrentCount
	if allowRepeat != 1 {
		distinct, err := countDistinctTeamParticipants(s.DB, team.ID)
		if err != nil {
			return nil, err
		}
		current = distinct
	}

	remaining := uint32(0)
	if current < target {
		remaining = target - current
	}

	progress := &GroupBuyProgress{
		TeamID:          team.ID,
		TargetCount:     target,
		CurrentCount:    current,
		RemainingCount:  remaining,
		Status:          team.Status,
		StatusText:      text,
		ExpireAt:        team.ExpireAt.Format(time.RFC3339),
		GroupPrice:      groupPrice,
		AllowRepeatJoin: allowRepeat,
	}
	if accountID > 0 {
		count, err := countUserTeamOrders(s.DB, accountID, team.ID)
		if err != nil {
			return nil, err
		}
		progress.UserJoinCount = uint32(count)
		progress.UserJoined = count > 0
		// 仅已支付入团才算团长身份展示
		progress.IsLeader = progress.UserJoined && team.LeaderID == accountID
	}
	// 展示进度与成团一致：以已支付人数为准（current_count 仅在付款入团时增加）
	if allowRepeat == 1 {
		if paid, err := countPaidPendingGroupOnTeam(s.DB, team.ID, 0); err == nil {
			progress.CurrentCount = paid
			if paid < target {
				progress.RemainingCount = target - paid
			} else {
				progress.RemainingCount = 0
			}
		}
	}
	members, err := loadGroupBuyMemberViews(s.DB, team.ID, accountID, s.AvatarPublicBase)
	if err != nil {
		return nil, err
	}
	progress.Members = members
	return progress, nil
}

// buildEmptyGroupBuyProgress 构造"未开团"的空进度。
// 用于商品已启用拼团但暂无任何待成团 team 的场景，避免前端因拿不到进度而展示假数据。
func (s *OrderService) buildEmptyGroupBuyProgress(product *model.Product, gb *model.GroupBuy, actGB *ActivityGroupBuyConfig) *GroupBuyProgress {
	allowRepeat := product.GroupBuyAllowRepeat
	groupPrice := gb.GroupPrice
	target := gb.TargetCount
	if actGB != nil {
		allowRepeat = actGB.GroupBuyAllowRepeat
		groupPrice = actGB.GroupBuyPrice
		if target == 0 && actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
	}
	if target == 0 {
		if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
			target = *product.GroupBuyTargetCount
		} else if gb.TargetCount >= 2 {
			target = gb.TargetCount
		} else {
			target = 2
		}
	}
	return &GroupBuyProgress{
		TeamID:          0,
		TargetCount:     target,
		CurrentCount:    0,
		RemainingCount:  target,
		Status:          model.GroupBuyTeamPending,
		StatusText:      "未开团",
		GroupPrice:      groupPrice,
		AllowRepeatJoin: allowRepeat,
	}
}

func (s *OrderService) findUserPendingTeamID(accountID, productID uint64) (*uint64, error) {
	var teamID uint64
	err := s.DB.
		Table("order_item oi").
		Select("oi.group_buy_team_id").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Joins("JOIN group_buy_team t ON t.id = oi.group_buy_team_id AND t.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.product_id = ? AND oi.group_buy_team_id IS NOT NULL AND oi.is_deleted = ?", accountID, productID, model.NotDeleted).
		Where("o.status = ? AND t.status = ?", model.OrderStatusPendingGroup, model.GroupBuyTeamPending).
		Order("o.id DESC").
		Limit(1).
		Scan(&teamID).Error
	if err != nil {
		return nil, err
	}
	if teamID == 0 {
		return nil, nil
	}
	return &teamID, nil
}

func countUserTeamJoins(db *gorm.DB, accountID, teamID uint64, activityID *uint64, excludeOrderID uint64) (int64, error) {
	// 仅计已支付参团，未付款不算「已加入」。
	// excludeOrderID：支付成功回调里本单已标已付，校验「是否已参过团」时需排除本单，否则会误判已加入。
	q := db.
		Table("order_item oi").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.is_deleted = ?", accountID, model.NotDeleted).
		Where("o.pay_status = ?", model.PayStatusPaid).
		Where("o.status <> ?", model.OrderStatusCancelled).
		Where("o.status <> ?", model.OrderStatusClosed)
	if teamID > 0 {
		q = q.Where("oi.group_buy_team_id = ?", teamID)
	}
	if excludeOrderID > 0 {
		q = q.Where("oi.order_id <> ?", excludeOrderID)
	}
	if activityID != nil {
		q = q.Where("oi.activity_id = ?", *activityID)
	} else {
		q = q.Where("oi.activity_id IS NULL")
	}
	var count int64
	return count, q.Count(&count).Error
}

func countDistinctTeamParticipants(db *gorm.DB, teamID uint64) (uint32, error) {
	var distinct int64
	err := db.Raw(`
		SELECT COUNT(DISTINCT o.account_id)
		FROM order_item oi
		INNER JOIN `+"`order`"+` o ON o.id = oi.order_id AND o.is_deleted = 0
		WHERE oi.group_buy_team_id = ? AND oi.is_deleted = 0
		  AND o.status = ? AND o.pay_status = ?
	`, teamID, model.OrderStatusPendingGroup, model.PayStatusPaid).Scan(&distinct).Error
	return uint32(distinct), err
}

// countPaidPendingGroupOnTeam 已支付且仍待成团的参团笔数（同账号多笔各计一次）。
// excludeOrderID：本单刚标已付时排除自己，避免「满员」判断把本单算进去导致无法入团。
func countPaidPendingGroupOnTeam(db *gorm.DB, teamID, excludeOrderID uint64) (uint32, error) {
	var n int64
	q := db.Table("order_item oi").
		Joins("INNER JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = 0").
		Where("oi.group_buy_team_id = ? AND oi.is_deleted = 0", teamID).
		Where("o.status = ? AND o.pay_status = ?", model.OrderStatusPendingGroup, model.PayStatusPaid)
	if excludeOrderID > 0 {
		q = q.Where("oi.order_id <> ?", excludeOrderID)
	}
	err := q.Count(&n).Error
	return uint32(n), err
}

func countDistinctPaidPendingGroupOnTeam(db *gorm.DB, teamID uint64) (uint32, error) {
	var distinct int64
	err := db.Raw(`
		SELECT COUNT(DISTINCT o.account_id)
		FROM order_item oi
		INNER JOIN `+"`order`"+` o ON o.id = oi.order_id AND o.is_deleted = 0
		WHERE oi.group_buy_team_id = ? AND oi.is_deleted = 0
		  AND o.status = ? AND o.pay_status = ?
	`, teamID, model.OrderStatusPendingGroup, model.PayStatusPaid).Scan(&distinct).Error
	return uint32(distinct), err
}

func loadAccountNicknames(db *gorm.DB, accountIDs []uint64) map[uint64]string {
	result := make(map[uint64]string)
	if len(accountIDs) == 0 {
		return result
	}
	type row struct {
		ID       uint64
		Nickname string
	}
	var rows []row
	_ = db.Table("account").
		Select("id, COALESCE(NULLIF(nickname,''), '拼友') AS nickname").
		Where("id IN ?", accountIDs).
		Scan(&rows).Error
	for _, r := range rows {
		if r.Nickname != "" {
			result[r.ID] = r.Nickname
		}
	}
	return result
}

func loadGroupBuyMemberViews(db *gorm.DB, teamID, viewerAccountID uint64, avatarBase string) ([]GroupBuyMemberView, error) {
	var members []model.GroupBuyMember
	if err := query.NotDeleted(db).
		Where("team_id = ?", teamID).
		Order("is_leader DESC, joined_at ASC, id ASC").
		Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return []GroupBuyMemberView{}, nil
	}
	ids := make([]uint64, 0, len(members))
	seen := make(map[uint64]struct{}, len(members))
	for _, m := range members {
		if _, ok := seen[m.AccountID]; ok {
			continue
		}
		seen[m.AccountID] = struct{}{}
		ids = append(ids, m.AccountID)
	}
	type accRow struct {
		ID        uint64
		Nickname  string
		AvatarURL *string
	}
	var rows []accRow
	_ = db.Table("account").
		Select("id, COALESCE(NULLIF(nickname,''), '拼友') AS nickname, avatar_url").
		Where("id IN ?", ids).
		Scan(&rows).Error
	byID := make(map[uint64]accRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]GroupBuyMemberView, 0, len(members))
	for _, m := range members {
		acc := byID[m.AccountID]
		nick := acc.Nickname
		if nick == "" {
			nick = "拼友"
		}
		avatar := ""
		if acc.AvatarURL != nil && *acc.AvatarURL != "" {
			avatar = config.ExpandPublicURL(avatarBase, *acc.AvatarURL)
		}
		out = append(out, GroupBuyMemberView{
			AccountID: m.AccountID,
			Nickname:  nick,
			AvatarURL: avatar,
			IsLeader:  m.IsLeader == 1,
			IsMe:      viewerAccountID > 0 && m.AccountID == viewerAccountID,
		})
	}
	return out, nil
}

func findLatestActivityPendingTeam(db *gorm.DB, groupBuyID, activityID, activityProductID uint64) (*uint64, error) {
	var teamID uint64
	q := db.
		Table("group_buy_team t").
		Select("t.id").
		Joins("JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("t.is_deleted = ? AND t.group_buy_id = ? AND t.status = ?", model.NotDeleted, groupBuyID, model.GroupBuyTeamPending).
		Where("o.status = ?", model.OrderStatusPendingGroup)
	q = applyActivityTeamScope(q, &activityID, &activityProductID)
	err := q.Order("t.id DESC").Limit(1).Scan(&teamID).Error
	if err != nil {
		return nil, err
	}
	if teamID == 0 {
		return nil, nil
	}
	return &teamID, nil
}

func findUserPendingTeamInGroupBuy(tx *gorm.DB, accountID, groupBuyID uint64, activityID, activityProductID *uint64) (*uint64, error) {
	q := tx.
		Table("order_item oi").
		Select("oi.group_buy_team_id").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Joins("JOIN group_buy_team t ON t.id = oi.group_buy_team_id AND t.is_deleted = ? AND t.status = ?", model.NotDeleted, model.GroupBuyTeamPending).
		Where("o.account_id = ? AND oi.group_buy_id = ? AND oi.is_deleted = ?", accountID, groupBuyID, model.NotDeleted).
		Where("o.status = ?", model.OrderStatusPendingGroup)
	q = applyActivityTeamScope(q, activityID, activityProductID)
	var teamID uint64
	if err := q.Order("o.id DESC").Limit(1).Scan(&teamID).Error; err != nil {
		return nil, err
	}
	if teamID == 0 {
		return nil, nil
	}
	return &teamID, nil
}

func assertTeamMatchesActivityProduct(tx *gorm.DB, teamID uint64, activityProductID *uint64) error {
	if activityProductID == nil {
		return nil
	}
	var apID uint64
	err := tx.Table("order_item oi").
		Select("oi.activity_product_id").
		Where("oi.group_buy_team_id = ? AND oi.is_deleted = ? AND oi.activity_product_id IS NOT NULL", teamID, model.NotDeleted).
		Limit(1).
		Scan(&apID).Error
	if err != nil {
		return err
	}
	if apID == 0 || apID != *activityProductID {
		return ErrGroupBuyInvalid
	}
	return nil
}

func countUserTeamOrders(db *gorm.DB, accountID, teamID uint64) (int64, error) {
	return countUserTeamJoins(db, accountID, teamID, nil, 0)
}

func rollbackGroupTeamForOrder(tx *gorm.DB, orderID uint64) error {
	var item model.OrderItem
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND group_buy_team_id IS NOT NULL AND purchase_type = ?", orderID, model.PurchaseTypeGroup).
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	// 未付款未入团：仅有意向 team_id，无 member，不改 current_count。
	// 用 Count 避免 First 未命中污染 tx.Error。
	var memberCnt int64
	if err := query.NotDeleted(tx.Model(&model.GroupBuyMember{})).
		Where("order_id = ? AND team_id = ?", orderID, *item.GroupBuyTeamID).
		Count(&memberCnt).Error; err != nil {
		return err
	}
	if memberCnt == 0 {
		return nil
	}
	var member model.GroupBuyMember
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND team_id = ?", orderID, *item.GroupBuyTeamID).
		First(&member).Error; err != nil {
		return err
	}
	if err := query.SoftDelete(tx, &model.GroupBuyMember{}, "id = ?", member.ID).Error; err != nil {
		return err
	}

	teamID := *item.GroupBuyTeamID
	var team model.GroupBuyTeam
	if err := query.NotDeleted(tx).First(&team, teamID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if team.CurrentCount == 0 || team.Status != model.GroupBuyTeamPending {
		return nil
	}

	return tx.Model(&team).Update("current_count", gorm.Expr("current_count - 1")).Error
}

func (s *OrderService) getUserOrder(accountID, orderID uint64) (*model.Order, error) {
	return s.getOrderScoped(accountID, orderID, nil)
}

func (s *OrderService) getOrderScoped(accountID, orderID uint64, merchantID *uint64) (*model.Order, error) {
	var order model.Order
	q := query.NotDeleted(s.DB).Where("id = ?", orderID)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	} else if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	return &order, nil
}

func toOrderView(o *model.Order) OrderView {
	statusCode := model.OrderStatusCode(o.Status, o.MerchantReviewStage)
	statusText := model.OrderStatusDisplayText(o.Status, o.MerchantReviewStage)
	// 支付/退款金额优先覆盖履约文案（避免已退款仍显示「已入背包」）
	switch {
	case o.PayStatus == model.PayStatusRefunded ||
		(o.PayAmount > 0 && o.RefundedAmount+0.0001 >= o.PayAmount && o.RefundPendingAmount < 0.0001):
		statusCode = "refunded"
		statusText = "已退款"
	case o.PayStatus == model.PayStatusRefunding || o.RefundPendingAmount > 0.0001:
		statusCode = "refunding"
		statusText = "退款中"
	case o.PayStatus == model.PayStatusPartialRefunded || o.RefundedAmount > 0.0001:
		statusCode = "partial_refunded"
		statusText = "部分退款"
	}
	return OrderView{
		Order: *o, StatusText: statusText, StatusCode: statusCode,
	}
}

// attachItemRefundQuantities 按库存退款流水汇总各明细已退件数，并校正「买2退1」等仍显示已入背包的问题。
func (s *OrderService) attachItemRefundQuantities(view *OrderView) {
	if view == nil || view.ID == 0 {
		return
	}
	applyLogs := func(orderID uint64, items []model.OrderItem) (refundedQty, totalQty uint32, out []model.OrderItem) {
		out = items
		if len(items) == 0 {
			return 0, 0, out
		}
		var logs []model.UserInventoryLog
		if err := query.NotDeleted(s.DB).
			Where("order_id = ? AND event_type = ? AND delta_qty < 0",
				orderID, model.InventoryEventRefund).
			Find(&logs).Error; err != nil {
			return 0, 0, out
		}
		byKey := map[string]uint32{}
		byProduct := map[uint64]uint32{}
		itemCountByProduct := map[uint64]int{}
		for i := range out {
			itemCountByProduct[out[i].ProductID]++
		}
		for _, lg := range logs {
			qty := uint32(-lg.DeltaQty)
			key := fmt.Sprintf("%d\x00%s", lg.ProductID, lg.Spec)
			byKey[key] += qty
			byProduct[lg.ProductID] += qty
			refundedQty += qty
		}
		var assigned uint32
		for i := range out {
			totalQty += out[i].Quantity
			spec := ""
			if out[i].Spec != nil {
				spec = *out[i].Spec
			}
			key := fmt.Sprintf("%d\x00%s", out[i].ProductID, spec)
			qty := byKey[key]
			// 规格对不上时：同订单同商品仅一行则按商品汇总
			if qty == 0 && itemCountByProduct[out[i].ProductID] == 1 {
				qty = byProduct[out[i].ProductID]
			}
			out[i].RefundedQuantity = qty
			assigned += qty
		}
		// 有流水但规格/商品对不上：单明细订单直接挂上总已退件数
		if assigned == 0 && refundedQty > 0 && len(out) == 1 {
			q := refundedQty
			if q > out[0].Quantity {
				q = out[0].Quantity
			}
			out[0].RefundedQuantity = q
			assigned = q
		}
		// 流水缺失时：单明细订单用退款金额估算已退件数（买2退1仍能展示「已退1件」）
		if assigned == 0 && len(out) == 1 && out[0].Quantity > 0 && view.ID == orderID {
			paidRefund := view.RefundedAmount + view.RefundPendingAmount
			unit := out[0].UnitPrice
			if view.PayAmount > 0 {
				unit = view.PayAmount / float64(out[0].Quantity)
			}
			if paidRefund > 0.0001 && unit > 0 {
				est := uint32(math.Floor(paidRefund/unit + 1e-6))
				if est > out[0].Quantity {
					est = out[0].Quantity
				}
				if est > 0 {
					out[0].RefundedQuantity = est
					refundedQty = est
				}
			}
		}
		return refundedQty, totalQty, out
	}

	var refundedQty, totalQty uint32
	var items []model.OrderItem
	refundedQty, totalQty, items = applyLogs(view.ID, view.Items)
	view.Items = items
	for i := range view.Children {
		r, t, childItems := applyLogs(view.Children[i].ID, view.Children[i].Items)
		view.Children[i].Items = childItems
		refundedQty += r
		totalQty += t
	}

	if refundedQty == 0 || view.StatusCode == "refunded" {
		return
	}
	// 退款进行中：保持「退款中」，但已退件数仍展示
	if view.RefundPendingAmount > 0.0001 || view.PayStatus == model.PayStatusRefunding {
		if refundedQty < totalQty {
			// 买 N 退 M 进行中也标部分退款更直观
			view.StatusCode = "partial_refunded"
			view.StatusText = "部分退款"
		}
		return
	}
	if refundedQty < totalQty || view.PayStatus == model.PayStatusPartialRefunded ||
		(view.PayAmount > 0 && view.RefundedAmount+0.0001 < view.PayAmount) {
		view.StatusCode = "partial_refunded"
		view.StatusText = "部分退款"
		return
	}
	view.StatusCode = "refunded"
	view.StatusText = "已退款"
}

func normalizeDeliveryType(deliveryType uint8) (uint8, error) {
	if deliveryType == 0 {
		return model.DeliveryTypePickup, nil
	}
	if deliveryType != model.DeliveryTypePickup && deliveryType != model.DeliveryTypeDelivery {
		return 0, ErrInvalidDeliveryType
	}
	return deliveryType, nil
}

func validateGroupBuyOrderInput(quantity uint32, groupBuyTeamID *uint64, startNewTeam bool) error {
	if quantity != 1 {
		return fmt.Errorf("%w: 拼团每次只能购买 1 件", ErrGroupBuyInvalid)
	}
	hasTeam := groupBuyTeamID != nil && *groupBuyTeamID > 0
	if !hasTeam && !startNewTeam {
		return fmt.Errorf("%w: 请选择拼团或开新团", ErrGroupBuyInvalid)
	}
	if hasTeam && startNewTeam {
		return fmt.Errorf("%w: 不能同时指定参团与开新团", ErrGroupBuyInvalid)
	}
	return nil
}

func assertActivityGroupBuyOnly(purchaseType uint8, actCtx *ActivityOrderContext) error {
	if actCtx == nil || actCtx.ActivityProduct == nil {
		return nil
	}
	ap := actCtx.ActivityProduct
	if ap.EnableGroupBuy == 1 && purchaseType != model.PurchaseTypeGroup {
		return fmt.Errorf("%w: 该活动商品仅支持拼团", ErrGroupBuyInvalid)
	}
	if ap.EnableGroupBuy != 1 && purchaseType == model.PurchaseTypeGroup {
		return fmt.Errorf("%w: 该活动商品不支持拼团", ErrGroupBuyInvalid)
	}
	return nil
}

func assertBagPurchaseAllowed(purchaseType uint8, activityProductID *uint64) error {
	_ = purchaseType
	if activityProductID != nil && *activityProductID > 0 {
		return nil
	}
	// Non-activity bag orders use deal/group channels; enable_* checked per product.
	return nil
}

func assertBagPickupOnly(deliveryType uint8) error {
	if deliveryType == 0 {
		return nil
	}
	dt, err := normalizeDeliveryType(deliveryType)
	if err != nil {
		return err
	}
	if dt == model.DeliveryTypeDelivery {
		return fmt.Errorf("%w: 入包订单不支持下单配送，请使用外卖或背包跑腿", ErrInvalidDeliveryType)
	}
	return nil
}

func (s *OrderService) attachVerifyCode(view *OrderView) {
	if view.Status != model.OrderStatusPendingVerify {
		return
	}
	code, err := s.ensureVerifyCode(&view.Order)
	if err != nil || code == "" {
		return
	}
	view.VerifyCode = &code
}

// attachPickupCode 填充配送单出餐号与配送单 ID（备餐中/待骑手接单/配送中等有关联 DeliveryOrder 的订单）。
func (s *OrderService) attachPickupCode(view *OrderView) {
	var d model.DeliveryOrder
	// 用 Find+Limit，避免无配送单时 First 打出 record not found 日志
	err := query.NotDeleted(s.DB).Select("id", "pickup_code").
		Where("order_id = ?", view.ID).
		Order("id DESC").Limit(1).Find(&d).Error
	if err != nil || d.ID == 0 {
		return
	}
	if d.PickupCode != "" {
		view.PickupCode = d.PickupCode
	}
	did := d.ID
	view.DeliveryOrderID = &did
}

func (s *OrderService) ensureVerifyCode(order *model.Order) (string, error) {
	var vc model.VerificationCode
	err := query.NotDeleted(s.DB).
		Where("order_id = ? AND status = ?", order.ID, model.VerificationCodeUnused).
		Order("id DESC").
		First(&vc).Error
	if err == nil {
		return vc.Code, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	created, err := createVerificationCode(s.DB, order)
	if err != nil {
		return "", err
	}
	return created.Code, nil
}

func getOrCreateVerificationCode(tx *gorm.DB, order *model.Order) (*model.VerificationCode, error) {
	var existing model.VerificationCode
	err := query.NotDeleted(tx).
		Where("order_id = ? AND status = ?", order.ID, model.VerificationCodeUnused).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return createVerificationCode(tx, order)
}

func genOrderNo() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("YJ%s%s", time.Now().Format("20060102150405"), hex.EncodeToString(b))
}

func (s *OrderService) creditOrderInventory(tx *gorm.DB, accountID, orderID uint64, items []model.OrderItem) error {
	if s.InventorySvc == nil || len(items) == 0 {
		return nil
	}
	// 店内套餐：入背包只入套餐本体（选配在使用时再做）
	items = selectInventoryCreditItems(tx, orderID, items)
	if len(items) == 0 {
		return nil
	}
	return s.InventorySvc.CreditFromOrder(tx, accountID, orderID, items)
}

// selectInventoryCreditItems 普通单入账全部明细；套餐单只入账套餐头行。
func selectInventoryCreditItems(tx *gorm.DB, orderID uint64, items []model.OrderItem) []model.OrderItem {
	var order model.Order
	if err := query.NotDeleted(tx).Select("id", "package_product_id").First(&order, orderID).Error; err != nil {
		return items
	}
	if order.PackageProductID == nil || *order.PackageProductID == 0 {
		return items
	}
	pkgID := *order.PackageProductID
	out := make([]model.OrderItem, 0, 1)
	for _, it := range items {
		if it.ProductID == pkgID {
			out = append(out, it)
		}
	}
	return out
}

// filterOutPackageProductItems 兼容旧逻辑名：现改为套餐入账筛选。
func filterOutPackageProductItems(tx *gorm.DB, orderID uint64, items []model.OrderItem) []model.OrderItem {
	return selectInventoryCreditItems(tx, orderID, items)
}

const (
	productChannelDeal    = "deal"
	productChannelGroup   = "group"
	productChannelTakeout = "takeout"
)

func purchaseTypeToChannel(purchaseType uint8) string {
	if purchaseType == model.PurchaseTypeGroup {
		return productChannelGroup
	}
	return productChannelDeal
}

// stockChannelForOrder 决定下单扣减/取消回退用的商品库存通道。
// 活动商品的拼团由 activity_product 配置，底层商品未必开启 enable_group；
// 此时按购买方式扣 group_stock 会 0 行更新并误报「库存不足」。活动单改为优先扣已开启通道（deal > group > takeout）。
func stockChannelForOrder(p model.Product, purchaseType uint8, isActivity bool) string {
	ch := purchaseTypeToChannel(purchaseType)
	if !isActivity {
		return ch
	}
	if productChannelEnabled(p, ch) {
		return ch
	}
	for _, c := range []string{productChannelDeal, productChannelGroup, productChannelTakeout} {
		if productChannelEnabled(p, c) {
			return c
		}
	}
	return ch
}

func productChannelColumns(channel string) (enableCol, stockCol string, syncLegacyStock bool, ok bool) {
	switch channel {
	case productChannelDeal:
		return "enable_deal", "deal_stock", true, true
	case productChannelGroup:
		return "enable_group", "group_stock", false, true
	case productChannelTakeout:
		return "enable_takeout", "takeout_stock", false, true
	default:
		return "", "", false, false
	}
}

func productChannelStock(p model.Product, channel string) uint32 {
	switch channel {
	case productChannelDeal:
		return p.DealStock
	case productChannelGroup:
		return p.GroupStock
	case productChannelTakeout:
		return p.TakeoutStock
	default:
		return 0
	}
}

func productChannelEnabled(p model.Product, channel string) bool {
	switch channel {
	case productChannelDeal:
		return p.EnableDeal == 1
	case productChannelGroup:
		return p.EnableGroup == 1
	case productChannelTakeout:
		return p.EnableTakeout == 1
	default:
		return false
	}
}

func assertProductChannelPurchasable(p model.Product, channel string) error {
	if productChannelEnabled(p, channel) {
		return nil
	}
	switch channel {
	case productChannelGroup:
		return ErrGroupBuyInvalid
	case productChannelTakeout:
		return ErrDeliveryNotAllowed
	default:
		return fmt.Errorf("%w: 该商品未开启团购", ErrSoloPurchaseDisabled)
	}
}

func takeoutGoodsUnitPrice(p model.Product) (float64, error) {
	if err := assertProductChannelPurchasable(p, productChannelTakeout); err != nil {
		return 0, err
	}
	if p.OriginalPrice == nil || *p.OriginalPrice <= 0 {
		return 0, fmt.Errorf("%w: 外卖需设置原价", ErrInvalidProductArg)
	}
	return *p.OriginalPrice, nil
}

// deductChannelStockInTx 按购买通道扣减库存（deal/group/takeout）。
func deductChannelStockInTx(tx *gorm.DB, productID uint64, quantity uint32, channel string) error {
	if quantity == 0 {
		return nil
	}
	enableCol, stockCol, syncLegacy, ok := productChannelColumns(channel)
	if !ok {
		return fmt.Errorf("%w: invalid stock channel", ErrInvalidProductArg)
	}
	base := query.NotDeleted(tx.Model(&model.Product{})).
		Where("id = ? AND "+enableCol+" = 1 AND "+stockCol+" >= ?", productID, quantity)
	var result *gorm.DB
	if syncLegacy {
		result = base.Updates(map[string]interface{}{
			stockCol: gorm.Expr(stockCol+" - ?", quantity),
			"stock":  gorm.Expr("stock - ?", quantity),
		})
	} else {
		result = base.Update(stockCol, gorm.Expr(stockCol+" - ?", quantity))
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInsufficientStock
	}
	return nil
}

func restoreChannelStockInTx(tx *gorm.DB, productID uint64, quantity uint32, channel string) error {
	if quantity == 0 {
		return nil
	}
	_, stockCol, syncLegacy, ok := productChannelColumns(channel)
	if !ok {
		return fmt.Errorf("%w: invalid stock channel", ErrInvalidProductArg)
	}
	base := tx.Model(&model.Product{}).Where("id = ?", productID)
	if syncLegacy {
		return base.Updates(map[string]interface{}{
			stockCol: gorm.Expr(stockCol+" + ?", quantity),
			"stock":  gorm.Expr("stock + ?", quantity),
		}).Error
	}
	return base.Update(stockCol, gorm.Expr(stockCol+" + ?", quantity)).Error
}

// restoreProductStockForOrder 取消/拒单时回退订单商品库存。
func restoreProductStockForOrder(tx *gorm.DB, orderID uint64) error {
	return applyOrderChannelStock(tx, orderID, false)
}

// deductProductStockForOrder 下单/补入账时按通道扣减库存。
func deductProductStockForOrder(tx *gorm.DB, orderID uint64) error {
	return applyOrderChannelStock(tx, orderID, true)
}

func applyOrderChannelStock(tx *gorm.DB, orderID uint64, deduct bool) error {
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ?", orderID).Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		if it.Quantity == 0 {
			continue
		}
		channel := purchaseTypeToChannel(it.PurchaseType)
		if it.ActivityProductID != nil {
			var p model.Product
			if err := tx.Select("id", "enable_deal", "enable_group", "enable_takeout").
				First(&p, it.ProductID).Error; err != nil {
				return err
			}
			channel = stockChannelForOrder(p, it.PurchaseType, true)
		}
		if deduct {
			if err := deductChannelStockInTx(tx, it.ProductID, it.Quantity, channel); err != nil {
				return err
			}
		} else if err := restoreChannelStockInTx(tx, it.ProductID, it.Quantity, channel); err != nil {
			return err
		}
	}
	return nil
}

func genVerifyCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("V%s", hex.EncodeToString(b))
}

func createVerificationCode(tx *gorm.DB, order *model.Order) (*model.VerificationCode, error) {
	code := genVerifyCode()
	orderID := order.ID
	vc := model.VerificationCode{
		OrderID: &orderID, AccountID: order.AccountID, Code: code, Status: model.VerificationCodeUnused,
	}
	exp := time.Now().AddDate(0, 0, 30)
	vc.ExpiredAt = &exp
	if err := tx.Create(&vc).Error; err != nil {
		return nil, err
	}
	return &vc, nil
}
