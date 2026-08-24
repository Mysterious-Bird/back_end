package handler

import (
	"errors"
	"strconv"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type CreateOrderRequest struct {
	ProductID         uint64                          `json:"product_id" example:"1"`
	MerchantID        uint64                          `json:"merchant_id"`
	Quantity          uint32                          `json:"quantity" example:"1"`
	PurchaseType      uint8                           `json:"purchase_type" example:"1"`
	GroupBuyID        *uint64                         `json:"group_buy_id"`
	GroupBuyTeamID    *uint64                         `json:"group_buy_team_id"`
	StartNewTeam      bool                            `json:"start_new_team"`
	ActivityProductID *uint64                         `json:"activity_product_id" example:"1"`
	BargainSessionID  *uint64                         `json:"bargain_session_id" example:"1"`
	DeliveryType      uint8                           `json:"delivery_type" example:"1"`
	AddressID         *uint64                         `json:"address_id"`
	DeliveryLatitude  *float64                        `json:"delivery_latitude"`
	DeliveryLongitude *float64                        `json:"delivery_longitude"`
	Remark            *string                         `json:"remark"`
	CartItemID        *uint64                         `json:"cart_item_id"`
	UserCouponID      *uint64                         `json:"user_coupon_id" example:"1"`
	PackageSelections []service.PackageSelectionInput `json:"package_selections"`
}

type RequestUseRequest struct {
	DeliveryType      uint8    `json:"delivery_type" binding:"required"`
	AddressID         *uint64  `json:"address_id"`
	DeliveryLatitude  *float64 `json:"delivery_latitude"`
	DeliveryLongitude *float64 `json:"delivery_longitude"`
	Remark            *string  `json:"remark"`
}

// CreateOrder godoc
// @Summary      创建订单
// @Description  默认 Mock 支付：下单事务内记已付。可选 user_coupon_id。微信支付见 POST /user/orders/{id}/pay
// @Tags         用户-订单
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CreateOrderRequest  true  "下单信息"
// @Success      200   {object}  response.Body{data=service.OrderView}
// @Router       /user/orders [post]
func (h *UserHandler) CreateOrder(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效: "+err.Error())
		return
	}
	if req.ActivityProductID == nil && req.ProductID == 0 {
		response.BadRequest(c, "请指定 product_id 或 activity_product_id")
		return
	}
	usePackage := len(req.PackageSelections) > 0
	if !usePackage {
		if req.ActivityProductID != nil {
			usePackage = h.OrderSvc.IsActivityProductPackage(*req.ActivityProductID)
		} else if req.ProductID > 0 {
			usePackage = h.OrderSvc.IsProductPackage(req.ProductID)
		}
	}

	var view *service.OrderView
	var err error
	if usePackage {
		view, err = h.OrderSvc.CreatePackage(accountID, service.CreatePackageOrderInput{
			ProductID: req.ProductID, MerchantID: req.MerchantID, Quantity: req.Quantity,
			PackageSelections: req.PackageSelections,
			PurchaseType:      req.PurchaseType, GroupBuyID: req.GroupBuyID, GroupBuyTeamID: req.GroupBuyTeamID,
			StartNewTeam:      req.StartNewTeam,
			ActivityProductID: req.ActivityProductID,
			DeliveryType:      req.DeliveryType, AddressID: req.AddressID, Remark: req.Remark,
			DeliveryLatitude: req.DeliveryLatitude, DeliveryLongitude: req.DeliveryLongitude,
			UserCouponID: req.UserCouponID,
		})
	} else {
		if req.MerchantID == 0 {
			response.BadRequest(c, "请指定 merchant_id")
			return
		}
		view, err = h.OrderSvc.Create(accountID, service.CreateOrderInput{
			ProductID: req.ProductID, MerchantID: req.MerchantID, Quantity: req.Quantity,
			PurchaseType: req.PurchaseType, GroupBuyID: req.GroupBuyID, GroupBuyTeamID: req.GroupBuyTeamID,
			StartNewTeam:      req.StartNewTeam,
			ActivityProductID: req.ActivityProductID,
			BargainSessionID:  req.BargainSessionID,
			DeliveryType:      req.DeliveryType, AddressID: req.AddressID, Remark: req.Remark,
			DeliveryLatitude: req.DeliveryLatitude, DeliveryLongitude: req.DeliveryLongitude,
			CartItemID: req.CartItemID, UserCouponID: req.UserCouponID,
		})
	}
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

// CancelOrder godoc
// @Summary      取消订单
// @Tags         用户-订单
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "订单 ID"
// @Success      200  {object}  response.Body
// @Router       /user/orders/{id}/cancel [post]
func (h *UserHandler) CancelOrder(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.OrderSvc.Cancel(accountID, id); err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, nil)
}

// RequestUse godoc
// @Summary      申请使用
// @Description  商家已通过订单审核后，用户申请自取/配送
// @Tags         用户-订单
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                true  "订单 ID"
// @Param        body  body  RequestUseRequest  true  "使用方式"
// @Success      200   {object}  response.Body{data=service.OrderView}
// @Router       /user/orders/{id}/request-use [post]
func (h *UserHandler) RequestUse(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req RequestUseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.OrderSvc.RequestUse(accountID, id, service.RequestUseInput{
		DeliveryType: req.DeliveryType, AddressID: req.AddressID, Remark: req.Remark,
		DeliveryLatitude: req.DeliveryLatitude, DeliveryLongitude: req.DeliveryLongitude,
	})
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

// ConfirmPickup godoc
// @Summary      确认自取完成
// @Tags         用户-订单
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "订单 ID"
// @Success      200  {object}  response.Body{data=service.OrderView}
// @Router       /user/orders/{id}/confirm-pickup [post]
func (h *UserHandler) ConfirmPickup(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.OrderSvc.ConfirmPickup(accountID, id)
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

// GetGroupProgress godoc
// @Summary      拼团进度
// @Description  登录时可返回当前用户参团信息（user_joined、user_join_count、is_leader）；未传 team_id 时优先返回用户进行中的团
// @Tags         用户-商城
// @Produce      json
// @Security     BearerAuth
// @Param        id       path   int  true   "商品 ID"
// @Param        team_id  query  int  false  "拼团实例 ID"
// @Success      200  {object}  response.Body{data=service.GroupBuyProgress}
// @Router       /products/{id}/group [get]
func (h *StoreHandler) GetGroupProgress(c *gin.Context) {
	productID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var teamID *uint64
	if s := c.Query("team_id"); s != "" {
		v, parseErr := parseUintParamFromString(s)
		if parseErr != nil {
			response.BadRequest(c, "team_id 无效")
			return
		}
		teamID = &v
	}
	accountID, _ := auth.AccountID(c)
	progress, err := h.OrderSvc.GetGroupProgress(accountID, productID, teamID)
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, progress)
}

func handleOrderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrPendingPayDuplicate):
		msg := "您有该商品的待付款订单，请前往订单查看并完成支付或取消后再购买"
		var dup *service.PendingPayDuplicateError
		data := gin.H{"reason": "pending_pay_duplicate"}
		if errors.As(err, &dup) && dup != nil {
			data["order_id"] = dup.OrderID
			data["order_no"] = dup.OrderNo
			data["product_id"] = dup.ProductID
			data["status_code"] = dup.StatusCode()
			if raw := err.Error(); len(raw) > len(service.ErrPendingPayDuplicate.Error()) {
				if i := len(service.ErrPendingPayDuplicate.Error()); i+2 < len(raw) && raw[i:i+2] == ": " {
					msg = raw[i+2:]
				}
			}
		}
		response.BadRequestWithData(c, msg, data)
	case errors.Is(err, service.ErrOrderNotFound):
		response.Fail(c, 404, 404, "订单不存在")
	case errors.Is(err, service.ErrOrderStatusInvalid):
		msg := "当前状态不允许此操作"
		if raw := err.Error(); len(raw) > len(service.ErrOrderStatusInvalid.Error()) {
			if i := len(service.ErrOrderStatusInvalid.Error()); i+2 < len(raw) && raw[i:i+2] == ": " {
				msg = raw[i+2:]
			}
		}
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrInvalidProductArg):
		msg := err.Error()
		if i := len(service.ErrInvalidProductArg.Error()); i+2 < len(msg) && msg[i:i+2] == ": " {
			msg = msg[i+2:]
		} else {
			msg = "参数无效"
		}
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrInsufficientStock):
		response.BadRequest(c, "库存不足")
	case errors.Is(err, service.ErrGroupBuyInvalid):
		msg := "拼团信息无效"
		if raw := err.Error(); len(raw) > len(service.ErrGroupBuyInvalid.Error()) {
			if i := len(service.ErrGroupBuyInvalid.Error()); i+2 < len(raw) && raw[i:i+2] == ": " {
				msg = raw[i+2:]
			}
		}
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrGroupBuyAlreadyJoined):
		response.BadRequest(c, "您已参与该拼团，无法重复参团")
	case errors.Is(err, service.ErrBargainNotFound):
		response.Fail(c, 404, 404, "砍价不存在")
	case errors.Is(err, service.ErrBargainExpired):
		response.BadRequest(c, "砍价已过期")
	case errors.Is(err, service.ErrBargainForbidden), errors.Is(err, service.ErrBargainConflict):
		msg := "砍价不可用"
		if raw := err.Error(); len(raw) > 0 {
			msg = raw
			for _, prefix := range []error{service.ErrBargainForbidden, service.ErrBargainConflict} {
				p := prefix.Error()
				if len(raw) > len(p)+2 && raw[:len(p)] == p && raw[len(p):len(p)+2] == ": " {
					msg = raw[len(p)+2:]
					break
				}
			}
		}
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrActivityNotFound), errors.Is(err, service.ErrActivityProductNotFound):
		response.Fail(c, 404, 404, "活动商品不存在")
	case errors.Is(err, service.ErrActivityNotActive):
		response.BadRequest(c, "活动未开始或已结束")
	case errors.Is(err, service.ErrActivityLimitExceeded):
		response.BadRequest(c, "已超过活动购买限制")
	case errors.Is(err, service.ErrActivityRegisterWindow):
		response.BadRequest(c, "不在新用户购买有效期内，请使用普通价下单")
	case errors.Is(err, service.ErrActivityForbidden):
		response.Fail(c, 403, 403, "活动不可用")
	case errors.Is(err, service.ErrSoloPurchaseDisabled):
		response.BadRequest(c, "请使用团购、外卖或活动购买")
	case errors.Is(err, service.ErrPhoneRequired):
		response.BadRequest(c, "请先授权手机号")
	case errors.Is(err, service.ErrAddressRequired):
		response.BadRequest(c, "请选择收货地址")
	case errors.Is(err, service.ErrInvalidDeliveryType):
		response.BadRequest(c, "delivery_type 无效，请传 1=自提 或 2=配送")
	case errors.Is(err, service.ErrDeliveryOutOfRange):
		response.BadRequest(c, "收货地址不在配送范围内")
	case errors.Is(err, service.ErrDeliveryCoordinatesRequired):
		response.BadRequest(c, "配送订单请提供 delivery_latitude、delivery_longitude")
	case errors.Is(err, service.ErrDeliveryZoneInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrInventoryRollback):
		msg := "库存已使用，无法取消/拒绝"
		if raw := err.Error(); len(raw) > len(service.ErrInventoryRollback.Error()) {
			if i := len(service.ErrInventoryRollback.Error()); i+2 < len(raw) && raw[i:i+2] == ": " {
				msg = raw[i+2:]
			}
		}
		response.BadRequest(c, msg)
	case errors.Is(err, payment.ErrInvalidState):
		msg := "退款失败，请稍后重试"
		if raw := err.Error(); len(raw) > len(payment.ErrInvalidState.Error()) {
			if i := len(payment.ErrInvalidState.Error()); i+2 < len(raw) && raw[i:i+2] == ": " {
				msg = raw[i+2:]
			}
		}
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrProductNotFound):
		response.Fail(c, 404, 404, "商品不存在")
	case errors.Is(err, service.ErrUserCouponNotFound):
		response.Fail(c, 404, 404, "优惠券不存在")
	case errors.Is(err, service.ErrUserCouponInvalid):
		response.BadRequest(c, "优惠券不可用或已过期")
	case errors.Is(err, service.ErrCouponNotApplicable):
		response.BadRequest(c, "优惠券不满足使用条件")
	case errors.Is(err, service.ErrCouponUnavailable):
		response.BadRequest(c, "优惠券已失效")
	case errors.Is(err, payment.ErrNotConfigured):
		response.Fail(c, 503, 503, "支付未配置，请使用模拟支付")
	default:
		response.InternalError(c, "操作失败")
	}
}

type MerchantOrderHandler struct {
	MerchantSvc  *service.MerchantService
	OrderSvc     *service.OrderService
	VerifySvc    *service.VerificationService
	InventorySvc *service.InventoryService
	DashboardSvc *service.DashboardService
	DeliverySvc  *service.DeliveryService
}

type ReviewOrderRequest struct {
	Approve      bool    `json:"approve"`
	RejectReason *string `json:"reject_reason"`
}

type VerifyRequest struct {
	Code             string                             `json:"code" binding:"required" example:"V1a2b3c4d"`
	PackageUnits     []service.PackageUnitInput         `json:"package_units"`
	OptionSelections []service.OptionSelectionUnitInput `json:"option_selections"`
}

// ListMerchantOrders godoc
// @Summary      商家订单列表
// @Tags         商家端-订单
// @Produce      json
// @Security     BearerAuth
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页条数"
// @Param        status_code  query  string  false  "pending_merchant|pending_use_merchant|..."
// @Param        type         query  string  false  "order=待订单审核 use=待库存确认"
// @Param        account_id   query  int     false  "按买家用户 ID 筛选"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/orders [get]
func (h *MerchantOrderHandler) ListOrders(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	statusCode := c.Query("status_code")
	if t := c.Query("type"); t != "" && statusCode == "" {
		switch t {
		case "order":
			statusCode = "pending_merchant"
		case "use":
			statusCode = "pending_use_merchant"
		}
	}
	var buyerAccountID *uint64
	if raw := c.Query("account_id"); raw != "" {
		id, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			response.BadRequest(c, "account_id 无效")
			return
		}
		buyerAccountID = &id
	}
	accountIDs, empty, err := service.FindAccountIDsByKeyword(h.OrderSvc.DB, c.Query("keyword"))
	if err != nil {
		response.InternalError(c, "搜索用户失败")
		return
	}
	if empty {
		response.OK(c, query.PageResult{List: []service.OrderView{}, Total: 0, Page: page, PageSize: pageSize})
		return
	}
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	list, total, err := h.OrderSvc.List(0, scope, page, pageSize, nil, statusCode, buyerAccountID, accountIDs, start, end)
	if err != nil {
		response.InternalError(c, "获取订单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetMerchantOrder godoc
// @Summary      商家订单详情
// @Tags         商家端-订单
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "订单 ID"
// @Success      200  {object}  response.Body{data=service.OrderView}
// @Router       /merchant/orders/{id} [get]
func (h *MerchantOrderHandler) GetOrder(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.OrderSvc.GetView(0, id, scope)
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

// ReviewOrder godoc
// @Summary      商家订单审核（第一阶段）
// @Tags         商家端-订单
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                 true  "订单 ID"
// @Param        body  body  ReviewOrderRequest  true  "审核结果"
// @Success      200   {object}  response.Body{data=service.OrderView}
// @Router       /merchant/orders/{id}/review [patch]
func (h *MerchantOrderHandler) ReviewOrder(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ReviewOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.OrderSvc.MerchantReview(*scope, id, req.Approve, req.RejectReason)
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

// UseReviewOrder godoc
// @Summary      商家库存确认（第二阶段）
// @Tags         商家端-订单
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                 true  "订单 ID"
// @Param        body  body  ReviewOrderRequest  true  "审核结果"
// @Success      200   {object}  response.Body{data=service.OrderView}
// @Router       /merchant/orders/{id}/use-review [patch]
func (h *MerchantOrderHandler) UseReviewOrder(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ReviewOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.OrderSvc.MerchantUseReview(*scope, id, req.Approve)
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

// PreviewVerify godoc
// @Summary      扫码查询核销信息
// @Description  根据核销码查询商品信息；仅商品所属商家可查看，数量固定不可选
// @Tags         商家端-核销
// @Produce      json
// @Security     BearerAuth
// @Param        code  query  string  true  "核销码"
// @Success      200   {object}  response.Body{data=service.VerifyPreviewView}
// @Router       /merchant/verify [get]
func (h *MerchantOrderHandler) PreviewVerify(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	code := c.Query("code")
	if code == "" {
		response.BadRequest(c, "请提供核销码")
		return
	}
	preview, err := h.VerifySvc.LookupByCode(*scope, code)
	if err != nil {
		handleVerifyError(c, err)
		return
	}
	response.OK(c, preview)
}

// Verify godoc
// @Summary      确认核销
// @Description  核销码一次性使用，整单/整次使用记录完成，无需传数量
// @Tags         商家端-核销
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  VerifyRequest  true  "核销码"
// @Success      200   {object}  response.Body
// @Router       /merchant/verify [post]
func (h *MerchantOrderHandler) Verify(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	operatorID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req VerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	record, err := h.VerifySvc.Verify(*scope, operatorID, req.Code, req.PackageUnits, req.OptionSelections)
	if err != nil {
		handleVerifyError(c, err)
		return
	}
	response.OK(c, record)
}

func handleVerifyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrVerifyCodeNotFound):
		response.Fail(c, 404, 404, "核销码无效")
	case errors.Is(err, service.ErrVerifyCodeUsed):
		response.BadRequest(c, "核销码已使用")
	case errors.Is(err, service.ErrVerifyCodeExpired):
		response.BadRequest(c, "核销码已过期")
	case errors.Is(err, service.ErrVerifyMerchantMismatch):
		response.Fail(c, 403, 403, "非本店商品，无法核销")
	case errors.Is(err, service.ErrVerifyRiderRequired):
		response.BadRequest(c, "请等待骑手接单后再核销")
	case errors.Is(err, service.ErrPackageSelectionRequired):
		response.BadRequest(c, "套餐请先完成选配再核销")
	case errors.Is(err, service.ErrOptionRequired):
		response.BadRequest(c, "请先完成规格选择再核销")
	case errors.Is(err, service.ErrOptionInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrInsufficientStock):
		response.BadRequest(c, "套餐内商品库存不足")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "核销失败")
	}
}

// ListVerificationRecords godoc
// @Summary      商家核销记录
// @Tags         商家端-核销
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/verification-records [get]
func (h *MerchantOrderHandler) ListVerificationRecords(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	list, total, err := h.VerifySvc.ListFiltered(service.VerificationListFilter{
		MerchantID: scope,
		Keyword:    c.Query("keyword"),
		StartDate:  start,
		EndDate:    end,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		response.InternalError(c, "获取记录失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListInventoryUsages godoc
// @Summary      背包使用记录（商家）
// @Tags         商家端-背包
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Param        status     query  int  false  "5=取消待审核"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/inventory-usages [get]
func (h *MerchantOrderHandler) ListInventoryUsages(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	var status *uint8
	if s := c.Query("status"); s != "" {
		v, parseErr := strconv.ParseUint(s, 10, 8)
		if parseErr != nil {
			response.BadRequest(c, "status 无效")
			return
		}
		u := uint8(v)
		status = &u
	}
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	list, total, err := h.InventorySvc.ListUsagesForMerchant(*scope, status, page, pageSize, c.Query("keyword"), start, end)
	if err != nil {
		response.InternalError(c, "获取使用记录失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetMerchantInventoryUsage godoc
// @Summary      背包使用记录详情（商家）
// @Tags         商家端-背包
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "使用记录 ID"
// @Success      200  {object}  response.Body{data=service.InventoryUsageView}
// @Router       /merchant/inventory-usages/{id} [get]
func (h *MerchantOrderHandler) GetInventoryUsage(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.InventorySvc.GetUsageViewForMerchant(*scope, id)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// ListPendingPackageSelections godoc
// @Summary      待套餐选配列表（核销后）
// @Tags         商家端-背包
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/inventory-usages/package-pending [get]
func (h *MerchantOrderHandler) ListPendingPackageSelections(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.InventorySvc.ListPendingPackageSelections(*scope, page, pageSize)
	if err != nil {
		response.InternalError(c, "获取待选配列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ConfirmPackageSelection godoc
// @Summary      确认套餐选配（核销后）
// @Tags         商家端-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "使用记录 ID"
// @Param        body  body  ConfirmPackageSelectionRequest  true  "选配"
// @Success      200   {object}  response.Body{data=service.InventoryUsageView}
// @Router       /merchant/inventory-usages/{id}/confirm-package [post]
func (h *MerchantOrderHandler) ConfirmPackageSelection(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ConfirmPackageSelectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.InventorySvc.ConfirmPackageSelection(*scope, id, req.PackageSelections, req.PackageUnits)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// ReviewCancelInventoryUsage godoc
// @Summary      审核背包使用取消申请
// @Description  骑手配送单用户申请取消后，商家同意则回滚库存并取消配送
// @Tags         商家端-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                 true  "使用记录 ID"
// @Param        body  body  ReviewOrderRequest  true  "审核结果"
// @Success      200   {object}  response.Body{data=service.InventoryUsageView}
// @Router       /merchant/inventory-usages/{id}/cancel-review [patch]
func (h *MerchantOrderHandler) ReviewCancelInventoryUsage(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ReviewOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.InventorySvc.MerchantReviewCancelUsage(*scope, id, req.Approve, req.RejectReason)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// MarkDeliveryPrepared godoc
// @Summary      商家确认出餐
// @Description  备餐中订单确认出餐后，配送单对骑手可见可接单，订单进入待骑手接单
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送单 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /merchant/deliveries/{id}/prepare [post]
func (h *MerchantOrderHandler) MarkDeliveryPrepared(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		response.Fail(c, 403, 403, "无商家权限")
		return
	}
	if h.DeliverySvc == nil {
		response.InternalError(c, "配送服务未配置")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	d, err := h.DeliverySvc.MarkPrepared(*scope, id)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, d)
}

type RejectDeliveryPrepareRequest struct {
	Reason string `json:"reason" binding:"required" example:"今日已打烊"`
}

// RejectDeliveryPrepare godoc
// @Summary      商家拒绝备餐中的外卖订单
// @Description  备餐中可拒绝出餐；商品回退用户背包，拒绝原因写入订单备注供用户在全部订单查看
// @Tags         商家端
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "配送单 ID"
// @Param        body  body  RejectDeliveryPrepareRequest  true  "拒绝原因"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /merchant/deliveries/{id}/reject [post]
func (h *MerchantOrderHandler) RejectDeliveryPrepare(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		response.Fail(c, 403, 403, "无商家权限")
		return
	}
	if h.DeliverySvc == nil {
		response.InternalError(c, "配送服务未配置")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req RejectDeliveryPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请填写拒绝原因")
		return
	}
	d, err := h.DeliverySvc.RejectPrepare(*scope, id, req.Reason)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, d)
}

// ListPreparingDeliveries godoc
// @Summary      商家端备餐中的配送单列表
// @Description  含购买订单路径和背包使用路径的备餐中配送单（merchant_prepared=0）
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/deliveries/preparing [get]
func (h *MerchantOrderHandler) ListPreparingDeliveries(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		response.Fail(c, 403, 403, "无商家权限")
		return
	}
	if h.DeliverySvc == nil {
		response.InternalError(c, "配送服务未配置")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.DeliverySvc.ListPreparingForMerchant(*scope, page, pageSize)
	if err != nil {
		response.InternalError(c, "获取备餐配送单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListPreparedDeliveries godoc
// @Summary      商家端已出餐待接单的配送单列表
// @Description  已确认出餐但骑手尚未接单的配送单
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/deliveries/prepared [get]
func (h *MerchantOrderHandler) ListPreparedDeliveries(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		response.Fail(c, 403, 403, "无商家权限")
		return
	}
	if h.DeliverySvc == nil {
		response.InternalError(c, "配送服务未配置")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.DeliverySvc.ListPreparedForMerchant(*scope, page, pageSize)
	if err != nil {
		response.InternalError(c, "获取已出餐配送单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

func (h *MerchantOrderHandler) merchantScope(c *gin.Context) (*uint64, error) {
	return resolveMerchantScope(c, h.MerchantSvc)
}

type RiderHandler struct {
	DeliverySvc *service.DeliveryService
	EarningSvc  *service.RiderEarningService
}

// ListRiderOrders godoc
// @Summary      骑手配送单列表
// @Description  scope=pending 待接单 active 配送中 history 历史
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Param        scope      query  string  false  "pending|active|history"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /rider/orders [get]
func (h *RiderHandler) ListOrders(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.DeliverySvc.ListForRider(riderID, c.Query("scope"), page, pageSize)
	if err != nil {
		response.InternalError(c, "获取配送单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// AcceptDelivery godoc
// @Summary      骑手接单
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送单 ID"
// @Success      200  {object}  response.Body
// @Router       /rider/orders/{id}/accept [post]
func (h *RiderHandler) AcceptDelivery(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	d, err := h.DeliverySvc.Accept(riderID, id)
	if err != nil {
		if errors.Is(err, service.ErrDeliveryNotFound) {
			response.Fail(c, 404, 404, "配送单不存在或已被接单")
			return
		}
		response.InternalError(c, "接单失败")
		return
	}
	response.OK(c, d)
}

// StartDelivery godoc
// @Summary      开始配送
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送单 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /rider/orders/{id}/start [post]
func (h *RiderHandler) StartDelivery(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	d, err := h.DeliverySvc.Start(riderID, id)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, d)
}

// ReportException godoc
// @Summary      骑手上报异常
// @Description  已接单/配送中可上报，进入异常状态等管理员处理。接单后不可取消，遇异常走此接口。
// @Tags         骑手端
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                      true  "配送单 ID"
// @Param        body  body  ReportExceptionRequest   true  "异常原因"
// @Success      200   {object}  response.Body{data=service.DeliveryView}
// @Router       /rider/orders/{id}/exception [post]
func (h *RiderHandler) ReportException(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ReportExceptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Reason == "" {
		response.BadRequest(c, "请填写异常原因")
		return
	}
	d, err := h.DeliverySvc.ReportException(riderID, id, req.Reason)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, d)
}

// GetDelivery godoc
// @Summary      骑手查单条配送详情
// @Description  含商家门店信息/出餐号/用户地址，接单后查看
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送单 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /rider/orders/{id} [get]
func (h *RiderHandler) GetDelivery(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	d, err := h.DeliverySvc.GetForRider(riderID, id)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, d)
}

// CompleteDelivery godoc
// @Summary      骑手确认送达
// @Description  可传送达备注与照片；用户确认收货后订单/使用单才完成
// @Tags         骑手端
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                      true  "配送单 ID"
// @Param        body  body  CompleteDeliveryRequest  false  "送达信息"
// @Success      200   {object}  response.Body{data=service.DeliveryView}
// @Router       /rider/orders/{id}/complete [post]
func (h *RiderHandler) CompleteDelivery(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req CompleteDeliveryRequest
	_ = c.ShouldBindJSON(&req)
	d, err := h.DeliverySvc.Complete(riderID, id, service.CompleteDeliveryInput{
		Remark: req.Remark, Photos: req.Photos,
	})
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, d)
}

// EarningsSummary godoc
// @Summary      骑手收益汇总
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.EarningsSummary}
// @Router       /rider/earnings/summary [get]
func (h *RiderHandler) EarningsSummary(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	sum, err := h.EarningSvc.GetSummary(riderID)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, sum)
}

// ListEarnings godoc
// @Summary      骑手收益记录
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Param        status     query  string  false  "pending|settled|cancelled"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /rider/earnings [get]
func (h *RiderHandler) ListEarnings(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.ListEarnings(riderID, c.Query("status"), page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListSettlements godoc
// @Summary      骑手结账记录
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /rider/settlements [get]
func (h *RiderHandler) ListSettlements(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.ListSettlements(riderID, page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// RequestSettlement godoc
// @Summary      骑手申请结账（全额）
// @Description  申请金额 = 待结账余额 - 待审批申请占用金额
// @Tags         骑手端
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=model.RiderSettlement}
// @Router       /rider/settlements/request [post]
func (h *RiderHandler) RequestSettlement(c *gin.Context) {
	riderID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	st, err := h.EarningSvc.RequestSettlement(riderID)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientEarnings) {
			response.BadRequest(c, "无可用待结账金额")
			return
		}
		response.InternalError(c, "申请失败")
		return
	}
	response.OK(c, st)
}

type AdminDashboardHandler struct {
	DashboardSvc *service.DashboardService
	OrderSvc     *service.OrderService
	VerifySvc    *service.VerificationService
}

// AdminDashboard godoc
// @Summary      管理端工作台统计
// @Tags         管理端
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.AdminDashboard}
// @Router       /admin/dashboard [get]
func (h *AdminDashboardHandler) Dashboard(c *gin.Context) {
	d, err := h.DashboardSvc.Admin()
	if err != nil {
		response.InternalError(c, "获取统计失败")
		return
	}
	response.OK(c, d)
}

// AdminSalesReport godoc
// @Summary      销售额报表（管理端）
// @Description  统计有效营业额/有效订单（核销完成、确认收货、外卖完成后计入）及核销次数。可选 merchant_id、日期范围
// @Tags         管理端-统计
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int     false  "指定商家，不传为全平台"
// @Param        start_date   query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date     query  string  false  "结束日期 YYYY-MM-DD"
// @Success      200  {object}  response.Body{data=service.SalesReport}
// @Router       /admin/sales [get]
func (h *AdminDashboardHandler) SalesReport(c *gin.Context) {
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
	}
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	report, err := h.DashboardSvc.SalesReport(service.SalesReportFilter{
		MerchantID: merchantID, StartDate: start, EndDate: end,
	})
	if err != nil {
		response.InternalError(c, "获取销售额失败")
		return
	}
	response.OK(c, report)
}

// ListAdminOrders godoc
// @Summary      全平台订单
// @Tags         管理端
// @Produce      json
// @Security     BearerAuth
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页条数"
// @Param        status_code  query  string  false  "状态码筛选"
// @Param        account_id   query  int     false  "按买家用户 ID 筛选"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/orders [get]
func (h *AdminDashboardHandler) ListOrders(c *gin.Context) {
	page, pageSize := parsePage(c)
	var buyerAccountID *uint64
	if raw := c.Query("account_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "account_id 无效")
			return
		}
		buyerAccountID = &id
	}
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
	}
	accountIDs, empty, err := service.FindAccountIDsByKeyword(h.OrderSvc.DB, c.Query("keyword"))
	if err != nil {
		response.InternalError(c, "搜索用户失败")
		return
	}
	if empty {
		response.OK(c, query.PageResult{List: []service.OrderView{}, Total: 0, Page: page, PageSize: pageSize})
		return
	}
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	list, total, err := h.OrderSvc.List(0, merchantID, page, pageSize, nil, c.Query("status_code"), buyerAccountID, accountIDs, start, end)
	if err != nil {
		response.InternalError(c, "获取订单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetAdminOrder godoc
// @Summary      订单详情（管理端）
// @Tags         管理端
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "订单 ID"
// @Success      200  {object}  response.Body{data=service.OrderView}
// @Router       /admin/orders/{id} [get]
func (h *AdminDashboardHandler) GetOrder(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.OrderSvc.GetView(0, id, nil)
	if err != nil {
		handleOrderError(c, err)
		return
	}
	response.OK(c, view)
}

type AdminRejectExpireRefundRequest struct {
	Remark string `json:"remark"`
}

// ListExpireRefundReviews godoc
// @Summary      拼团过期退款待审核列表（管理端）
// @Tags         管理端-背包
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/expire-refunds/pending [get]
func (h *AdminDashboardHandler) ListExpireRefundReviews(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.OrderSvc.ListExpireRefundReviews(page, pageSize)
	if err != nil {
		response.InternalError(c, "获取过期退款审核列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ApproveExpireRefund godoc
// @Summary      通过拼团过期退款（管理端）
// @Tags         管理端-背包
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "使用记录 ID"
// @Success      200  {object}  response.Body
// @Router       /admin/expire-refunds/{id}/approve [post]
func (h *AdminDashboardHandler) ApproveExpireRefund(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "使用记录 ID 无效")
		return
	}
	view, err := h.OrderSvc.AdminApproveExpireRefund(id)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// RejectExpireRefund godoc
// @Summary      拒绝拼团过期退款（作废不退）（管理端）
// @Tags         管理端-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                            true  "使用记录 ID"
// @Param        body  body  AdminRejectExpireRefundRequest false "备注"
// @Success      200  {object}  response.Body
// @Router       /admin/expire-refunds/{id}/reject [post]
func (h *AdminDashboardHandler) RejectExpireRefund(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "使用记录 ID 无效")
		return
	}
	var req AdminRejectExpireRefundRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.OrderSvc.AdminRejectExpireRefund(id, req.Remark); err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// ListAdminVerificationRecords godoc
// @Summary      全平台核销记录
// @Tags         管理端
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/verification-records [get]
func (h *AdminDashboardHandler) ListVerificationRecords(c *gin.Context) {
	page, pageSize := parsePage(c)
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
	}
	list, total, err := h.VerifySvc.ListFiltered(service.VerificationListFilter{
		MerchantID: merchantID,
		Keyword:    c.Query("keyword"),
		StartDate:  start,
		EndDate:    end,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		response.InternalError(c, "获取记录失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// MerchantDashboard godoc
// @Summary      商家工作台统计
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.MerchantDashboard}
// @Router       /merchant/dashboard [get]
func (h *MerchantOrderHandler) Dashboard(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	d, err := h.DashboardSvc.Merchant(*scope)
	if err != nil {
		response.InternalError(c, "获取统计失败")
		return
	}
	response.OK(c, d)
}

// MerchantSalesReport godoc
// @Summary      销售额报表（商家端）
// @Description  本店有效营业额/有效订单（核销完成、确认收货、外卖完成后计入）及核销次数
// @Tags         商家端-统计
// @Produce      json
// @Security     BearerAuth
// @Param        start_date  query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date    query  string  false  "结束日期 YYYY-MM-DD"
// @Success      200  {object}  response.Body{data=service.SalesReport}
// @Router       /merchant/sales [get]
func (h *MerchantOrderHandler) SalesReport(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	start, end, err := service.ParseSalesDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		response.BadRequest(c, "日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	report, err := h.DashboardSvc.SalesReport(service.SalesReportFilter{
		MerchantID: scope, StartDate: start, EndDate: end,
	})
	if err != nil {
		response.InternalError(c, "获取销售额失败")
		return
	}
	response.OK(c, report)
}

type CategoryHandler struct {
	CategorySvc *service.CategoryService
}

// ListCategories godoc
// @Summary      商品分类列表（按商家）
// @Description  须传 merchant_id，返回该店铺自有分类
// @Tags         用户-商城
// @Produce      json
// @Param        merchant_id  query  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=[]model.ProductCategory}
// @Failure      400  {object}  response.Body
// @Router       /categories [get]
func (h *CategoryHandler) ListCategories(c *gin.Context) {
	s := c.Query("merchant_id")
	if s == "" {
		response.BadRequest(c, "请指定 merchant_id")
		return
	}
	merchantID, err := parseUintParamFromString(s)
	if err != nil {
		response.BadRequest(c, "merchant_id 无效")
		return
	}
	list, err := h.CategorySvc.ListByMerchant(merchantID, true)
	if err != nil {
		response.InternalError(c, "获取分类失败")
		return
	}
	response.OK(c, list)
}

var _ model.ProductCategory
