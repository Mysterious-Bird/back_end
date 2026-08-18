package handler

import (
	"errors"
	"strconv"
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	MerchantSvc *service.MerchantService
	ProductSvc  *service.ProductService
	RiderSvc    *service.RiderApplicationService
	EarningSvc  *service.RiderEarningService
}

type CreateMerchantRequest struct {
	Phone        string   `json:"phone" example:"13800138000"`
	OpenID       string   `json:"openid" example:"oABC123"`
	ShopName     string   `json:"shop_name" binding:"required" example:"豫记小店"`
	ShopLogo     *string  `json:"shop_logo"`
	Images       []string `json:"images" example:"/uploads/2026/06/30/a.jpg,/uploads/2026/06/30/b.jpg"`
	ContactPhone *string  `json:"contact_phone"`
	Address      *string  `json:"address"`
	Nickname     *string  `json:"nickname"`
}

type UpdateMerchantStatusRequest struct {
	Status *uint8 `json:"status" binding:"required,oneof=0 1" example:"1"`
}

type UpdateMerchantImagesRequest struct {
	Images   []string `json:"images" binding:"required,min=1"`
	ShopLogo *string  `json:"shop_logo"`
}

// UpdateMerchantProfileRequest 选择性更新商家资料。
type UpdateMerchantProfileRequest struct {
	ShopName         *string             `json:"shop_name" example:"豫记小店"`
	ContactPhone     *string             `json:"contact_phone" example:"13800138000"`
	Address          *string             `json:"address" example:"河南省郑州市"`
	ShopLogo         *string             `json:"shop_logo"`
	Images           *[]string           `json:"images"`
	Latitude         FlexNullableFloat64 `json:"latitude"`
	Longitude        FlexNullableFloat64 `json:"longitude"`
	Lat              FlexNullableFloat64 `json:"lat"`
	Lng              FlexNullableFloat64 `json:"lng"`
	AllowReservation *uint8              `json:"allow_reservation" example:"1"`
	AutoApprove      *uint8              `json:"auto_approve" example:"1"`
	OpenTime         *string             `json:"open_time" example:"09:00"`
	CloseTime        *string             `json:"close_time" example:"22:00"`
	DeliveryFee      *float64            `json:"delivery_fee" example:"5.00"`
	RiderEarnings    *float64            `json:"rider_earnings" example:"4.00"`
}

func (r UpdateMerchantProfileRequest) hasField() bool {
	return r.ShopName != nil || r.ContactPhone != nil || r.Address != nil ||
		r.ShopLogo != nil || r.Images != nil || r.AllowReservation != nil || r.AutoApprove != nil ||
		r.OpenTime != nil || r.CloseTime != nil ||
		r.DeliveryFee != nil || r.RiderEarnings != nil ||
		r.Latitude.Present || r.Longitude.Present || r.Lat.Present || r.Lng.Present
}

type ProductRequest struct {
	MerchantID                 uint64                      `json:"merchant_id" example:"1"`
	CategoryID                 uint64                      `json:"category_id" example:"1"`
	CategoryName               *string                     `json:"category_name" example:"特产"`
	Name                       string                      `json:"name" binding:"required" example:"信阳毛尖"`
	Description                *string                     `json:"description"`
	CoverURL                   string                      `json:"cover_url" example:"/uploads/2026/06/30/abc.jpg"`
	Images                     []string                    `json:"images" example:"/uploads/2026/06/30/a.jpg,/uploads/2026/06/30/b.jpg"`
	Price                      float64                     `json:"price" binding:"required" example:"99.9"`
	OriginalPrice              *float64                    `json:"original_price" example:"129.9"`
	Stock                      uint32                      `json:"stock" example:"100"`
	EnableDeal                 *uint8                      `json:"enable_deal" example:"1"`
	EnableGroup                *uint8                      `json:"enable_group" example:"0"`
	EnableTakeout              *uint8                      `json:"enable_takeout" example:"0"`
	DealStock                  *uint32                     `json:"deal_stock" example:"100"`
	GroupStock                 *uint32                     `json:"group_stock" example:"50"`
	TakeoutStock               *uint32                     `json:"takeout_stock" example:"50"`
	IsHot                      uint8                       `json:"is_hot" example:"0"`
	EnableGroupBuy             *uint8                      `json:"enable_group_buy" example:"0"`
	EnableCoupon               *uint8                      `json:"enable_coupon" example:"1"`
	AllowPickup                *uint8                      `json:"allow_pickup" example:"1"`
	AllowDelivery              *uint8                      `json:"allow_delivery" example:"1"`
	GroupBuyTargetCount        *uint32                     `json:"group_buy_target_count" example:"3"`
	GroupBuyPrice              *float64                    `json:"group_buy_price" example:"79.9"`
	GroupBuyAllowRepeat        *uint8                      `json:"group_buy_allow_repeat" example:"0"`
	GroupBuyMaxConcurrentTeams *uint32                     `json:"group_buy_max_concurrent_teams" example:"0"`
	DealExpireDays             *uint32                     `json:"deal_expire_days" example:"7"`
	GroupExpireDays            *uint32                     `json:"group_expire_days" example:"7"`
	ItemType                   uint8                       `json:"item_type" example:"1"`
	Status                     uint8                       `json:"status" example:"0"`
	PackageGroups              []service.PackageGroupInput `json:"package_groups"`
	OptionGroups               []service.OptionGroupInput  `json:"option_groups"`
	ApplicableMerchantIDs      *[]uint64                   `json:"applicable_merchant_ids"`
}

// UpdateProductRequest 选择性更新商品：只传需要修改的字段，未传字段保留原值。
type UpdateProductRequest struct {
	MerchantID                 *uint64                     `json:"merchant_id"`
	CategoryID                 *uint64                     `json:"category_id"`
	CategoryName               *string                     `json:"category_name"`
	Name                       *string                     `json:"name"`
	Description                *string                     `json:"description"`
	CoverURL                   *string                     `json:"cover_url"`
	Images                     *[]string                   `json:"images"`
	Price                      *float64                    `json:"price"`
	OriginalPrice              *float64                    `json:"original_price"`
	Stock                      *uint32                     `json:"stock"`
	EnableDeal                 *uint8                      `json:"enable_deal"`
	EnableGroup                *uint8                      `json:"enable_group"`
	EnableTakeout              *uint8                      `json:"enable_takeout"`
	DealStock                  *uint32                     `json:"deal_stock"`
	GroupStock                 *uint32                     `json:"group_stock"`
	TakeoutStock               *uint32                     `json:"takeout_stock"`
	IsHot                      *uint8                      `json:"is_hot"`
	EnableGroupBuy             *uint8                      `json:"enable_group_buy"`
	EnableCoupon               *uint8                      `json:"enable_coupon"`
	AllowPickup                *uint8                      `json:"allow_pickup"`
	AllowDelivery              *uint8                      `json:"allow_delivery"`
	GroupBuyTargetCount        *uint32                     `json:"group_buy_target_count"`
	GroupBuyPrice              *float64                    `json:"group_buy_price"`
	GroupBuyAllowRepeat        *uint8                      `json:"group_buy_allow_repeat"`
	GroupBuyMaxConcurrentTeams *uint32                     `json:"group_buy_max_concurrent_teams"`
	DealExpireDays             *uint32                     `json:"deal_expire_days"`
	GroupExpireDays            *uint32                     `json:"group_expire_days"`
	ItemType                   *uint8                      `json:"item_type"`
	Status                     *uint8                      `json:"status"`
	PackageGroups              []service.PackageGroupInput `json:"package_groups"`
	OptionGroups               *[]service.OptionGroupInput `json:"option_groups"`
	ApplicableMerchantIDs      *[]uint64                   `json:"applicable_merchant_ids"`
	ForceCloseGroup            *uint8                      `json:"force_close_group"` // 1=确认关闭拼团并退款待成团
}

func (r UpdateProductRequest) hasField() bool {
	return r.MerchantID != nil || r.CategoryID != nil || r.CategoryName != nil ||
		r.Name != nil || r.Description != nil || r.CoverURL != nil || r.Images != nil ||
		r.Price != nil || r.OriginalPrice != nil || r.Stock != nil || r.IsHot != nil ||
		r.EnableDeal != nil || r.EnableGroup != nil || r.EnableTakeout != nil ||
		r.DealStock != nil || r.GroupStock != nil || r.TakeoutStock != nil ||
		r.EnableGroupBuy != nil || r.EnableCoupon != nil || r.AllowPickup != nil || r.AllowDelivery != nil ||
		r.GroupBuyTargetCount != nil || r.GroupBuyPrice != nil || r.GroupBuyAllowRepeat != nil ||
		r.GroupBuyMaxConcurrentTeams != nil || r.DealExpireDays != nil || r.GroupExpireDays != nil ||
		r.ItemType != nil || r.Status != nil || len(r.PackageGroups) > 0 || r.OptionGroups != nil ||
		r.ApplicableMerchantIDs != nil || r.ForceCloseGroup != nil
}

type UpdateProductImagesRequest struct {
	Images   []string `json:"images" binding:"required,min=1"`
	CoverURL *string  `json:"cover_url"`
}

type UpdateProductStatusRequest struct {
	Status *uint8 `json:"status" binding:"required,oneof=0 1" example:"1"`
}

type UpdateProductPriceRequest struct {
	Price         float64  `json:"price" binding:"required" example:"88.8"`
	OriginalPrice *float64 `json:"original_price"`
}

type UpdateProductStockRequest struct {
	Stock uint32 `json:"stock" binding:"required" example:"50"`
}

type UpdateProductGroupBuyRequest struct {
	EnableGroupBuy             uint8    `json:"enable_group_buy" binding:"required" example:"1"`
	GroupBuyTargetCount        *uint32  `json:"group_buy_target_count" example:"3"`
	GroupBuyPrice              *float64 `json:"group_buy_price" example:"79.9"`
	GroupBuyAllowRepeat        *uint8   `json:"group_buy_allow_repeat" example:"0"`
	GroupBuyMaxConcurrentTeams *uint32  `json:"group_buy_max_concurrent_teams" example:"0"`
	ForceCloseGroup            *uint8   `json:"force_close_group"`
}

type UpdateProductCouponRequest struct {
	EnableCoupon uint8 `json:"enable_coupon" binding:"required" example:"1"`
}

// UpdateProductSaleRequest 商品销售方式（拼团 + 优惠券）一次性保存，供编辑页使用。
type UpdateProductSaleRequest struct {
	EnableGroupBuy             *uint8   `json:"enable_group_buy" example:"1"`
	EnableCoupon               *uint8   `json:"enable_coupon" example:"1"`
	GroupBuyTargetCount        *uint32  `json:"group_buy_target_count" example:"3"`
	GroupBuyPrice              *float64 `json:"group_buy_price" example:"79.9"`
	GroupBuyAllowRepeat        *uint8   `json:"group_buy_allow_repeat" example:"0"`
	GroupBuyMaxConcurrentTeams *uint32  `json:"group_buy_max_concurrent_teams" example:"0"`
	ForceCloseGroup            *uint8   `json:"force_close_group"`
}

// CreateMerchant godoc
// @Summary      创建商家
// @Description  仅需 shop_name；phone、openid 可选，后续可再绑定
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      CreateMerchantRequest  true  "商家信息"
// @Success      200   {object}  response.Body{data=MerchantProfileResp}
// @Failure      400   {object}  response.Body
// @Failure      409   {object}  response.Body
// @Router       /admin/merchants [post]
func (h *AdminHandler) CreateMerchant(c *gin.Context) {
	var req CreateMerchantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	profile, err := h.MerchantSvc.Create(service.CreateMerchantInput{
		Phone: req.Phone, OpenID: req.OpenID, ShopName: req.ShopName, ShopLogo: req.ShopLogo,
		Images: req.Images, ContactPhone: req.ContactPhone, Address: req.Address, Nickname: req.Nickname,
	})
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, profile)
}

// ListMerchants godoc
// @Summary      商家列表
// @Tags         管理端-商家
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Param        status     query  int     false  "状态 0=停业 1=营业"
// @Param        keyword    query  string  false  "店铺名/联系电话"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/merchants [get]
func (h *AdminHandler) ListMerchants(c *gin.Context) {
	page, pageSize := parsePage(c)
	filter := service.MerchantListFilter{Keyword: c.Query("keyword")}
	if s := c.Query("status"); s != "" {
		v, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			response.BadRequest(c, "status 参数无效")
			return
		}
		u := uint8(v)
		filter.Status = &u
	}
	list, total, err := h.MerchantSvc.List(page, pageSize, filter)
	if err != nil {
		response.InternalError(c, "获取商家列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetMerchant godoc
// @Summary      商家详情
// @Tags         管理端-商家
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=MerchantProfileResp}
// @Failure      404  {object}  response.Body
// @Router       /admin/merchants/{id} [get]
func (h *AdminHandler) GetMerchant(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	profile, err := h.MerchantSvc.GetByID(id)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, profile)
}

type bindMerchantOperatorBody struct {
	AccountID FlexUInt64 `json:"account_id"`
}

// GetMerchantOperator godoc
// @Summary      获取商家管理员
// @Tags         管理端-商家
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=service.MerchantOperatorView}
// @Router       /admin/merchants/{id}/operator [get]
func (h *AdminHandler) GetMerchantOperator(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.MerchantSvc.GetOperator(id)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, view)
}

// PutMerchantOperator godoc
// @Summary      绑定/换绑商家管理员
// @Description  将已有用户账号接管为该店唯一商家管理员（一人一店）
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                       true  "商家 ID"
// @Param        body  body  bindMerchantOperatorBody  true  "目标账号"
// @Success      200   {object}  response.Body{data=service.MerchantOperatorView}
// @Router       /admin/merchants/{id}/operator [put]
func (h *AdminHandler) PutMerchantOperator(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var body bindMerchantOperatorBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	accountID := body.AccountID.Uint64()
	if accountID == 0 {
		response.BadRequest(c, "请传 account_id")
		return
	}
	view, err := h.MerchantSvc.BindOperator(id, accountID)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, view)
}

// DeleteMerchantOperator godoc
// @Summary      解除商家管理员绑定
// @Tags         管理端-商家
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=service.MerchantOperatorView}
// @Router       /admin/merchants/{id}/operator [delete]
func (h *AdminHandler) DeleteMerchantOperator(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.MerchantSvc.UnbindOperator(id)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, view)
}

// UpdateMerchant godoc
// @Summary      更新商家资料（选择性）
// @Description  只传需要修改的字段；status 请用 PATCH /merchants/{id}/status
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                           true  "商家 ID"
// @Param        body  body  UpdateMerchantProfileRequest  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=MerchantProfileResp}
// @Router       /admin/merchants/{id} [patch]
func (h *AdminHandler) UpdateMerchant(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateMerchantProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if !req.hasField() {
		response.BadRequest(c, "请至少传一个要更新的字段")
		return
	}
	input, err := toUpdateMerchantInput(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	profile, err := h.MerchantSvc.UpdateProfile(id, input)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, profile)
}

// UpdateMerchantStatus godoc
// @Summary      更新商家营业状态
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "商家 ID"
// @Param        body  body  UpdateMerchantStatusRequest  true  "状态"
// @Success      200   {object}  response.Body{data=MerchantProfileResp}
// @Router       /admin/merchants/{id}/status [patch]
func (h *AdminHandler) UpdateMerchantStatus(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateMerchantStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	profile, err := h.MerchantSvc.UpdateStatus(id, *req.Status)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, profile)
}

// UpdateMerchantImages godoc
// @Summary      更新商家店铺图片
// @Description  绑定 images 数组；shop_logo 不传则取 images[0]
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "商家 ID"
// @Param        body  body  UpdateMerchantImagesRequest  true  "店铺图片"
// @Success      200   {object}  response.Body{data=MerchantProfileResp}
// @Router       /admin/merchants/{id}/images [patch]
func (h *AdminHandler) UpdateMerchantImages(c *gin.Context) {
	h.patchMerchantImages(c)
}

func (h *AdminHandler) patchMerchantImages(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateMerchantImagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	profile, err := h.MerchantSvc.UpdateImages(id, req.Images, req.ShopLogo)
	if err != nil {
		h.handleMerchantError(c, err)
		return
	}
	response.OK(c, profile)
}

// CreateProduct godoc
// @Summary      创建商品
// @Description  管理员可为任意商家创建商品；分类可传 category_id 或 category_name（不存在则自动创建）；enable_group_buy=1 时须传 group_buy_target_count、group_buy_price；enable_coupon 默认 1
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  ProductRequest  true  "商品信息"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products [post]
func (h *AdminHandler) CreateProduct(c *gin.Context) {
	var req ProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	isPackage := req.ItemType == model.ProductItemTypePackage
	if req.MerchantID == 0 {
		response.BadRequest(c, "请指定 merchant_id")
		return
	}
	if err := validateProductCategory(req, isPackage); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	product, err := h.ProductSvc.Create(buildProductInput(req, nil), nil)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	view, err := h.ProductSvc.GetDetailView(product.ID, nil)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, view)
}

// ListProducts godoc
// @Summary      商品列表
// @Tags         管理端-商品
// @Produce      json
// @Security     BearerAuth
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页条数"
// @Param        merchant_id  query  int     false  "商家 ID"
// @Param        category_id  query  int     false  "分类 ID"
// @Param        status       query  int     false  "0=下架 1=上架"
// @Param        keyword      query  string  false  "商品名"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/products [get]
func (h *AdminHandler) ListProducts(c *gin.Context) {
	page, pageSize := parsePage(c)
	filter, err := parseProductListFilter(c, false)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	list, total, err := h.ProductSvc.List(page, pageSize, filter)
	if err != nil {
		response.InternalError(c, "获取商品列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetProduct godoc
// @Summary      商品详情
// @Tags         管理端-商品
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "商品 ID"
// @Success      200  {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id} [get]
func (h *AdminHandler) GetProduct(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.ProductSvc.GetDetailView(id, nil)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, view)
}

// UpdateProduct godoc
// @Summary      更新商品（选择性）
// @Description  只传需要修改的字段，未传字段保留原值；与 PATCH 等价
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                    true  "商品 ID"
// @Param        body  body  UpdateProductRequest  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id} [put]
func (h *AdminHandler) UpdateProduct(c *gin.Context) {
	h.patchProductUpdate(c, nil)
}

// PatchProduct godoc
// @Summary      选择性更新商品
// @Description  只传需要修改的字段，未传字段保留原值
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                    true  "商品 ID"
// @Param        body  body  UpdateProductRequest  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id} [patch]
func (h *AdminHandler) PatchProduct(c *gin.Context) {
	h.patchProductUpdate(c, nil)
}

func (h *AdminHandler) patchProductUpdate(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if !req.hasField() {
		response.BadRequest(c, "请至少传一个要更新的字段")
		return
	}
	if err := validateProductCategoryPatch(req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	existing, err := h.ProductSvc.GetByID(id, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	product, err := h.ProductSvc.Update(id, buildPatchProductInput(req, existing), scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	view, err := h.ProductSvc.GetDetailView(product.ID, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, view)
}

// DeleteProduct godoc
// @Summary      删除商品（逻辑删除）
// @Tags         管理端-商品
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "商品 ID"
// @Success      200  {object}  response.Body
// @Router       /admin/products/{id} [delete]
func (h *AdminHandler) DeleteProduct(c *gin.Context) {
	h.deleteProduct(c, nil)
}

func (h *AdminHandler) deleteProduct(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.ProductSvc.Delete(id, scope); err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateProductStatus godoc
// @Summary      上架/下架商品
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "商品 ID"
// @Param        body  body  UpdateProductStatusRequest   true  "状态"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/status [patch]
func (h *AdminHandler) UpdateProductStatus(c *gin.Context) {
	h.patchProductStatus(c, nil)
}

// UpdateProductPrice godoc
// @Summary      修改商品价格
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "商品 ID"
// @Param        body  body  UpdateProductPriceRequest   true  "价格"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/price [patch]
func (h *AdminHandler) UpdateProductPrice(c *gin.Context) {
	h.patchProductPrice(c, nil)
}

// UpdateProductStock godoc
// @Summary      修改商品库存
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "商品 ID"
// @Param        body  body  UpdateProductStockRequest   true  "库存"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/stock [patch]
func (h *AdminHandler) UpdateProductStock(c *gin.Context) {
	h.patchProductStock(c, nil)
}

// UpdateProductGroupBuy godoc
// @Summary      修改商品拼团配置
// @Description  设置是否支持拼团、成团人数、团购价；enable_group_buy=1 时须填写 group_buy_target_count（≥2）与 group_buy_price（须低于售价）
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                            true  "商品 ID"
// @Param        body  body  UpdateProductGroupBuyRequest   true  "拼团配置"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/group-buy [patch]
func (h *AdminHandler) UpdateProductGroupBuy(c *gin.Context) {
	h.patchProductGroupBuy(c, nil)
}

// UpdateProductCoupon godoc
// @Summary      修改商品优惠券开关
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "商品 ID"
// @Param        body  body  UpdateProductCouponRequest   true  "优惠券开关"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/coupon [patch]
func (h *AdminHandler) UpdateProductCoupon(c *gin.Context) {
	h.patchProductCoupon(c, nil)
}

// UpdateProductSale godoc
// @Summary      更新商品销售方式（拼团/优惠券）
// @Description  编辑页一次性保存 enable_group_buy、enable_coupon、拼团人数与团购价；未传字段保留原值
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                       true  "商品 ID"
// @Param        body  body  UpdateProductSaleRequest  true  "销售方式"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/sale [patch]
func (h *AdminHandler) UpdateProductSale(c *gin.Context) {
	h.patchProductSale(c, nil)
}

// UpdateProductImages godoc
// @Summary      更新商品图片
// @Description  绑定 images 数组；cover_url 不传则取 images[0]
// @Tags         管理端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "商品 ID"
// @Param        body  body  UpdateProductImagesRequest  true  "图片列表"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /admin/products/{id}/images [patch]
func (h *AdminHandler) UpdateProductImages(c *gin.Context) {
	h.patchProductImages(c, nil)
}

func (h *AdminHandler) patchProductStatus(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	product, err := h.ProductSvc.UpdateStatus(id, *req.Status, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) patchProductPrice(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	product, err := h.ProductSvc.UpdatePrice(id, req.Price, req.OriginalPrice, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) patchProductStock(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	product, err := h.ProductSvc.UpdateStock(id, req.Stock, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) patchProductGroupBuy(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductGroupBuyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	product, err := h.ProductSvc.UpdateGroupBuy(id, service.GroupBuyConfigInput{
		EnableGroupBuy:             req.EnableGroupBuy,
		GroupBuyTargetCount:        req.GroupBuyTargetCount,
		GroupBuyPrice:              req.GroupBuyPrice,
		GroupBuyAllowRepeat:        req.GroupBuyAllowRepeat,
		GroupBuyMaxConcurrentTeams: req.GroupBuyMaxConcurrentTeams,
		ForceCloseGroup:            req.ForceCloseGroup != nil && *req.ForceCloseGroup == 1,
	}, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) patchProductCoupon(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	product, err := h.ProductSvc.UpdateCoupon(id, req.EnableCoupon, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) patchProductSale(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductSaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if req.EnableGroupBuy == nil && req.EnableCoupon == nil &&
		req.GroupBuyTargetCount == nil && req.GroupBuyPrice == nil && req.GroupBuyAllowRepeat == nil &&
		req.GroupBuyMaxConcurrentTeams == nil {
		response.BadRequest(c, "请至少传一个销售方式字段")
		return
	}
	product, err := h.ProductSvc.UpdateSaleOptions(id, service.UpdateProductSaleInput{
		EnableGroupBuy:             req.EnableGroupBuy,
		EnableCoupon:               req.EnableCoupon,
		GroupBuyTargetCount:        req.GroupBuyTargetCount,
		GroupBuyPrice:              req.GroupBuyPrice,
		GroupBuyAllowRepeat:        req.GroupBuyAllowRepeat,
		GroupBuyMaxConcurrentTeams: req.GroupBuyMaxConcurrentTeams,
		ForceCloseGroup:            req.ForceCloseGroup != nil && *req.ForceCloseGroup == 1,
	}, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) patchProductImages(c *gin.Context, scope *uint64) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateProductImagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	product, err := h.ProductSvc.UpdateImages(id, req.Images, req.CoverURL, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

func (h *AdminHandler) handleMerchantError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrPhoneExists):
		response.Fail(c, 409, 409, "手机号已被使用")
	case errors.Is(err, service.ErrOpenIDExists):
		response.Fail(c, 409, 409, "openid 已被使用")
	case errors.Is(err, service.ErrMerchantNotFound):
		response.Fail(c, 404, 404, "商家不存在")
	case errors.Is(err, service.ErrAccountNotFound):
		response.Fail(c, 404, 404, "用户不存在")
	case errors.Is(err, service.ErrOperatorAlreadyBound):
		response.Fail(c, 409, 409, "该用户已是其他店管理员")
	case errors.Is(err, service.ErrAccountNotBindable):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, "参数无效")
	case errors.Is(err, service.ErrInvalidMerchantArg):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "操作失败")
	}
}

func (h *AdminHandler) handleProductError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCatalogInUse):
		msg := service.CatalogInUseMessage(err)
		data := gin.H{"reason": "catalog_in_use"}
		var inUse *service.CatalogInUseError
		if errors.As(err, &inUse) && inUse != nil {
			data["reason"] = inUse.Reason
		}
		response.FailWithData(c, 409, 40910, msg, data)
	case errors.Is(err, service.ErrGroupCloseNeedsConfirm):
		msg := "仍有进行中的拼团订单，关闭通道将退款并解散这些团"
		data := gin.H{"reason": "group_close_needs_confirm"}
		var conf *service.GroupCloseNeedsConfirmError
		if errors.As(err, &conf) && conf != nil {
			data["product_id"] = conf.ProductID
			data["pending_team_count"] = conf.PendingTeamCount
			data["pending_order_count"] = conf.PendingOrderCount
			if raw := err.Error(); len(raw) > len(service.ErrGroupCloseNeedsConfirm.Error()) {
				if i := len(service.ErrGroupCloseNeedsConfirm.Error()); i+2 < len(raw) && raw[i:i+2] == ": " {
					msg = raw[i+2:]
				}
			}
		}
		response.FailWithData(c, 409, 40909, msg, data)
	case errors.Is(err, service.ErrProductNotFound):
		response.Fail(c, 404, 404, "商品不存在")
	case errors.Is(err, service.ErrProductForbidden):
		response.Fail(c, 403, 403, "无权操作该商品")
	case errors.Is(err, service.ErrMerchantNotFound):
		response.Fail(c, 404, 404, "商家不存在")
	case errors.Is(err, service.ErrCategoryNotFound):
		response.Fail(c, 404, 404, "分类不存在")
	case errors.Is(err, service.ErrCategoryForbidden):
		response.Fail(c, 403, 403, "分类不属于该商家")
	case errors.Is(err, service.ErrOptionNotAllowedOnPackage):
		response.BadRequest(c, "套餐商品不可配置规格")
	case errors.Is(err, service.ErrOptionInvalid):
		msg := err.Error()
		if i := strings.Index(msg, ": "); i >= 0 && i+2 < len(msg) {
			msg = msg[i+2:]
		} else {
			msg = "规格配置无效"
		}
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrInvalidProductArg):
		msg := err.Error()
		if i := strings.Index(msg, ": "); i >= 0 && i+2 < len(msg) {
			msg = msg[i+2:]
		} else {
			msg = "参数无效"
		}
		response.BadRequest(c, msg)
	default:
		response.InternalError(c, "操作失败")
	}
}

type MerchantHandler struct {
	MerchantSvc *service.MerchantService
	ProductSvc  *service.ProductService
	CategorySvc *service.CategoryService
	OrderSvc    *service.OrderService
}

// GetProfile godoc
// @Summary      当前商家资料
// @Description  管理员代管时需传 merchant_id 或 Header X-Merchant-Id
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "管理员指定商家 ID"
// @Success      200  {object}  response.Body{data=MerchantProfileResp}
// @Router       /merchant/profile [get]
func (h *MerchantHandler) GetProfile(c *gin.Context) {
	profile, err := resolveMerchantProfile(c, h.MerchantSvc)
	if err != nil {
		return
	}
	response.OK(c, profile)
}

// UpdateProfile godoc
// @Summary      更新当前商家资料（选择性）
// @Description  只传需要修改的字段；改图片也可继续用 PATCH /merchant/profile/images
// @Tags         商家端
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int                           false  "管理员指定商家 ID"
// @Param        body         body   UpdateMerchantProfileRequest  true   "要更新的字段"
// @Success      200  {object}  response.Body{data=MerchantProfileResp}
// @Router       /merchant/profile [patch]
func (h *MerchantHandler) UpdateProfile(c *gin.Context) {
	profile, err := resolveMerchantProfile(c, h.MerchantSvc)
	if err != nil {
		return
	}
	var req UpdateMerchantProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if !req.hasField() {
		response.BadRequest(c, "请至少传一个要更新的字段")
		return
	}
	input, err := toUpdateMerchantInput(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updated, err := h.MerchantSvc.UpdateProfile(profile.ID, input)
	if err != nil {
		admin := &AdminHandler{MerchantSvc: h.MerchantSvc}
		admin.handleMerchantError(c, err)
		return
	}
	// 开启自动审核时，把已有待审订单一并入背包
	if h.OrderSvc != nil && req.AutoApprove != nil && *req.AutoApprove == 1 {
		_, _ = h.OrderSvc.AutoApprovePendingForMerchant(profile.ID)
	}
	response.OK(c, updated)
}

// UpdateShopImages godoc
// @Summary      更新店铺图片
// @Description  绑定 images 数组；shop_logo 不传则取 images[0]；管理员代管时需传 merchant_id
// @Tags         商家端
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "管理员指定商家 ID"
// @Param        body         body   UpdateMerchantImagesRequest  true  "店铺图片"
// @Success      200  {object}  response.Body{data=MerchantProfileResp}
// @Router       /merchant/profile/images [patch]
func (h *MerchantHandler) UpdateShopImages(c *gin.Context) {
	profile, err := resolveMerchantProfile(c, h.MerchantSvc)
	if err != nil {
		return
	}
	var req UpdateMerchantImagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	updated, err := h.MerchantSvc.UpdateImages(profile.ID, req.Images, req.ShopLogo)
	if err != nil {
		admin := &AdminHandler{MerchantSvc: h.MerchantSvc}
		admin.handleMerchantError(c, err)
		return
	}
	response.OK(c, updated)
}

// CreateProduct godoc
// @Summary      创建商品
// @Description  商家创建自己的商品，merchant_id 自动绑定；分类可传 category_id 或 category_name（不存在则自动创建）；enable_group_buy=1 时须传 group_buy_target_count、group_buy_price
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  ProductRequest  true  "商品信息"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products [post]
func (h *MerchantHandler) CreateProduct(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	var req ProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if err := validateProductCategory(req, req.ItemType == model.ProductItemTypePackage); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	product, err := h.ProductSvc.Create(buildProductInput(req, nil), scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, product)
}

// ListProducts godoc
// @Summary      我的商品列表
// @Tags         商家端-商品
// @Produce      json
// @Security     BearerAuth
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页条数"
// @Param        category_id  query  int     false  "分类 ID"
// @Param        status       query  int     false  "0=下架 1=上架"
// @Param        keyword      query  string  false  "商品名"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/products [get]
func (h *MerchantHandler) ListProducts(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	filter, err := parseProductListFilter(c, true)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	filter.MerchantID = scope
	list, total, err := h.ProductSvc.List(page, pageSize, filter)
	if err != nil {
		response.InternalError(c, "获取商品列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetProduct godoc
// @Summary      商品详情
// @Tags         商家端-商品
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "商品 ID"
// @Success      200  {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id} [get]
func (h *MerchantHandler) GetProduct(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	product, err := h.ProductSvc.GetByID(id, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	view, err := h.ProductSvc.GetDetailView(product.ID, scope)
	if err != nil {
		h.handleProductError(c, err)
		return
	}
	response.OK(c, view)
}

// UpdateProduct godoc
// @Summary      更新商品（选择性）
// @Description  只传需要修改的字段，未传字段保留原值；与 PATCH 等价
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                    true  "商品 ID"
// @Param        body  body  UpdateProductRequest  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id} [put]
func (h *MerchantHandler) UpdateProduct(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductUpdate(c, scope)
}

// PatchProduct godoc
// @Summary      选择性更新商品
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                    true  "商品 ID"
// @Param        body  body  UpdateProductRequest  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id} [patch]
func (h *MerchantHandler) PatchProduct(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductUpdate(c, scope)
}

// DeleteProduct godoc
// @Summary      删除商品（逻辑删除）
// @Tags         商家端-商品
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "商品 ID"
// @Success      200  {object}  response.Body
// @Router       /merchant/products/{id} [delete]
func (h *MerchantHandler) DeleteProduct(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.deleteProduct(c, scope)
}

// UpdateProductStatus godoc
// @Summary      上架/下架商品
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "商品 ID"
// @Param        body  body  UpdateProductStatusRequest  true  "状态"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/status [patch]
func (h *MerchantHandler) UpdateProductStatus(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductStatus(c, scope)
}

// UpdateProductPrice godoc
// @Summary      修改商品价格
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                        true  "商品 ID"
// @Param        body  body  UpdateProductPriceRequest  true  "价格"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/price [patch]
func (h *MerchantHandler) UpdateProductPrice(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductPrice(c, scope)
}

// UpdateProductStock godoc
// @Summary      修改商品库存
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                        true  "商品 ID"
// @Param        body  body  UpdateProductStockRequest  true  "库存"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/stock [patch]
func (h *MerchantHandler) UpdateProductStock(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductStock(c, scope)
}

// UpdateProductGroupBuy godoc
// @Summary      修改商品拼团配置
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                            true  "商品 ID"
// @Param        body  body  UpdateProductGroupBuyRequest   true  "拼团配置"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/group-buy [patch]
func (h *MerchantHandler) UpdateProductGroupBuy(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductGroupBuy(c, scope)
}

// UpdateProductCoupon godoc
// @Summary      修改商品优惠券开关
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "商品 ID"
// @Param        body  body  UpdateProductCouponRequest   true  "优惠券开关"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/coupon [patch]
func (h *MerchantHandler) UpdateProductCoupon(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductCoupon(c, scope)
}

// UpdateProductSale godoc
// @Summary      更新商品销售方式（拼团/优惠券）
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                       true  "商品 ID"
// @Param        body  body  UpdateProductSaleRequest  true  "销售方式"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/sale [patch]
func (h *MerchantHandler) UpdateProductSale(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductSale(c, scope)
}

// UpdateProductImages godoc
// @Summary      更新商品图片
// @Tags         商家端-商品
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "商品 ID"
// @Param        body  body  UpdateProductImagesRequest  true  "图片列表"
// @Success      200   {object}  response.Body{data=ProductResp}
// @Router       /merchant/products/{id}/images [patch]
func (h *MerchantHandler) UpdateProductImages(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	admin := &AdminHandler{ProductSvc: h.ProductSvc}
	admin.patchProductImages(c, scope)
}

// ListMerchantCategories godoc
// @Summary      本店商品分类列表
// @Tags         商家端-分类
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "管理员指定商家 ID"
// @Param        status       query  int  false  "0/1，不传返回全部"
// @Success      200  {object}  response.Body{data=[]model.ProductCategory}
// @Router       /merchant/categories [get]
func (h *MerchantHandler) ListCategories(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	var status *uint8
	if raw := c.Query("status"); raw != "" {
		v, parseErr := strconv.ParseUint(raw, 10, 8)
		if parseErr != nil {
			response.BadRequest(c, "status 无效")
			return
		}
		u := uint8(v)
		status = &u
	}
	list, err := h.CategorySvc.ListAllScoped(scope, status)
	if err != nil {
		response.InternalError(c, "获取分类失败")
		return
	}
	response.OK(c, list)
}

// CreateMerchantCategory godoc
// @Summary      创建商品分类
// @Tags         商家端-分类
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "管理员指定商家 ID"
// @Param        body         body  CreateCategoryRequest  true  "分类信息"
// @Success      200  {object}  response.Body{data=model.ProductCategory}
// @Router       /merchant/categories [post]
func (h *MerchantHandler) CreateCategory(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	var req CreateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	cat, err := h.CategorySvc.Create(service.CreateCategoryInput{
		MerchantID: *scope,
		ParentID:   req.ParentID, Name: req.Name, IconURL: req.IconURL,
		SortOrder: req.SortOrder, Status: req.Status,
	})
	if err != nil {
		handleCategoryError(c, err)
		return
	}
	response.OK(c, cat)
}

// UpdateMerchantCategory godoc
// @Summary      更新商品分类
// @Tags         商家端-分类
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id           path  int  true  "分类 ID"
// @Param        merchant_id  query  int  false  "管理员指定商家 ID"
// @Param        body         body  UpdateCategoryRequest  true  "更新字段"
// @Success      200  {object}  response.Body{data=model.ProductCategory}
// @Router       /merchant/categories/{id} [patch]
func (h *MerchantHandler) UpdateCategory(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	cat, err := h.CategorySvc.UpdateForMerchant(id, *scope, service.UpdateCategoryInput{
		Name: req.Name, IconURL: req.IconURL, SortOrder: req.SortOrder, Status: req.Status,
	}, true)
	if err != nil {
		handleCategoryError(c, err)
		return
	}
	response.OK(c, cat)
}

// DeleteMerchantCategory godoc
// @Summary      删除商品分类
// @Tags         商家端-分类
// @Produce      json
// @Security     BearerAuth
// @Param        id           path  int  true  "分类 ID"
// @Param        merchant_id  query  int  false  "管理员指定商家 ID"
// @Success      200  {object}  response.Body
// @Router       /merchant/categories/{id} [delete]
func (h *MerchantHandler) DeleteCategory(c *gin.Context) {
	scope, err := h.merchantScope(c)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.CategorySvc.DeleteForMerchant(id, *scope, true); err != nil {
		handleCategoryError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *MerchantHandler) merchantScope(c *gin.Context) (*uint64, error) {
	return resolveMerchantScope(c, h.MerchantSvc)
}

func (h *MerchantHandler) handleProductError(c *gin.Context, err error) {
	admin := &AdminHandler{}
	admin.handleProductError(c, err)
}

func buildProductInput(req ProductRequest, existing *model.Product) service.ProductInput {
	input := service.ProductInput{
		MerchantID:    req.MerchantID,
		CategoryID:    req.CategoryID,
		Name:          req.Name,
		Description:   req.Description,
		CoverURL:      req.CoverURL,
		Images:        req.Images,
		Price:         req.Price,
		OriginalPrice: req.OriginalPrice,
		Stock:         req.Stock,
		IsHot:         req.IsHot,
		ItemType:      req.ItemType,
		Status:        req.Status,
	}
	if req.CategoryName != nil {
		input.CategoryName = strings.TrimSpace(*req.CategoryName)
	}
	if req.EnableDeal != nil {
		input.EnableDeal = *req.EnableDeal
	}
	if req.EnableGroup != nil {
		input.EnableGroup = *req.EnableGroup
	} else if req.EnableGroupBuy != nil {
		input.EnableGroup = *req.EnableGroupBuy
	}
	if req.EnableTakeout != nil {
		input.EnableTakeout = *req.EnableTakeout
	} else if req.AllowDelivery != nil {
		input.EnableTakeout = *req.AllowDelivery
	}
	if req.DealStock != nil {
		input.DealStock = *req.DealStock
	} else {
		input.DealStock = req.Stock
	}
	if req.GroupStock != nil {
		input.GroupStock = *req.GroupStock
	}
	if req.TakeoutStock != nil {
		input.TakeoutStock = *req.TakeoutStock
	}
	if req.EnableGroupBuy != nil {
		input.EnableGroupBuy = *req.EnableGroupBuy
	} else if existing != nil {
		input.EnableGroupBuy = existing.EnableGroupBuy
	} else {
		input.EnableGroupBuy = input.EnableGroup
	}
	if req.EnableCoupon != nil {
		input.EnableCoupon = *req.EnableCoupon
	} else if existing != nil {
		input.EnableCoupon = existing.EnableCoupon
	} else {
		input.EnableCoupon = 1
	}
	if req.AllowPickup != nil {
		input.AllowPickup = *req.AllowPickup
	} else if existing != nil {
		input.AllowPickup = existing.AllowPickup
	} else {
		input.AllowPickup = 1
	}
	if req.AllowDelivery != nil {
		input.AllowDelivery = *req.AllowDelivery
	} else if existing != nil {
		input.AllowDelivery = existing.AllowDelivery
	} else {
		input.AllowDelivery = input.EnableTakeout
	}
	if req.GroupBuyTargetCount != nil {
		input.GroupBuyTargetCount = req.GroupBuyTargetCount
	} else if existing != nil {
		input.GroupBuyTargetCount = existing.GroupBuyTargetCount
	}
	if req.GroupBuyPrice != nil {
		input.GroupBuyPrice = req.GroupBuyPrice
	} else if existing != nil {
		input.GroupBuyPrice = existing.GroupBuyPrice
	}
	if req.GroupBuyAllowRepeat != nil {
		input.GroupBuyAllowRepeat = *req.GroupBuyAllowRepeat
	} else if existing != nil {
		input.GroupBuyAllowRepeat = existing.GroupBuyAllowRepeat
	}
	if req.GroupBuyMaxConcurrentTeams != nil {
		input.GroupBuyMaxConcurrentTeams = *req.GroupBuyMaxConcurrentTeams
	} else if existing != nil {
		input.GroupBuyMaxConcurrentTeams = existing.GroupBuyMaxConcurrentTeams
	}
	if req.DealExpireDays != nil {
		input.DealExpireDays = req.DealExpireDays
	} else if existing != nil {
		input.DealExpireDays = existing.DealExpireDays
	}
	if req.GroupExpireDays != nil {
		input.GroupExpireDays = req.GroupExpireDays
	} else if existing != nil {
		input.GroupExpireDays = existing.GroupExpireDays
	}
	if len(req.PackageGroups) > 0 {
		input.PackageGroups = req.PackageGroups
	}
	if req.OptionGroups != nil {
		og := req.OptionGroups
		input.OptionGroups = &og
	}
	if req.ApplicableMerchantIDs != nil {
		input.ApplicableMerchantIDs = *req.ApplicableMerchantIDs
	}
	return input
}

func buildPatchProductInput(req UpdateProductRequest, existing *model.Product) service.ProductInput {
	input := service.ProductInput{
		MerchantID:                 existing.MerchantID,
		CategoryID:                 existing.CategoryID,
		Name:                       existing.Name,
		Description:                existing.Description,
		CoverURL:                   existing.CoverURL,
		Images:                     existing.Images,
		Price:                      existing.Price,
		OriginalPrice:              existing.OriginalPrice,
		Stock:                      existing.Stock,
		EnableDeal:                 existing.EnableDeal,
		EnableGroup:                existing.EnableGroup,
		EnableTakeout:              existing.EnableTakeout,
		DealStock:                  existing.DealStock,
		GroupStock:                 existing.GroupStock,
		TakeoutStock:               existing.TakeoutStock,
		IsHot:                      existing.IsHot,
		EnableGroupBuy:             existing.EnableGroupBuy,
		EnableCoupon:               existing.EnableCoupon,
		AllowPickup:                existing.AllowPickup,
		AllowDelivery:              existing.AllowDelivery,
		GroupBuyTargetCount:        existing.GroupBuyTargetCount,
		GroupBuyPrice:              existing.GroupBuyPrice,
		GroupBuyAllowRepeat:        existing.GroupBuyAllowRepeat,
		GroupBuyMaxConcurrentTeams: existing.GroupBuyMaxConcurrentTeams,
		DealExpireDays:             existing.DealExpireDays,
		GroupExpireDays:            existing.GroupExpireDays,
		ItemType:                   existing.ItemType,
		Status:                     existing.Status,
	}
	if req.MerchantID != nil {
		input.MerchantID = *req.MerchantID
	}
	if req.CategoryID != nil {
		input.CategoryID = *req.CategoryID
	}
	if req.CategoryName != nil {
		input.CategoryName = strings.TrimSpace(*req.CategoryName)
		input.CategoryID = 0
	}
	if req.Name != nil {
		input.Name = *req.Name
	}
	if req.Description != nil {
		input.Description = req.Description
	}
	if req.CoverURL != nil {
		input.CoverURL = *req.CoverURL
	}
	if req.Images != nil {
		input.Images = *req.Images
	}
	if req.Price != nil {
		input.Price = *req.Price
	}
	if req.OriginalPrice != nil {
		input.OriginalPrice = req.OriginalPrice
	}
	if req.Stock != nil {
		input.Stock = *req.Stock
		if req.DealStock == nil {
			input.DealStock = *req.Stock
		}
	}
	if req.EnableDeal != nil {
		input.EnableDeal = *req.EnableDeal
	}
	if req.EnableGroup != nil {
		input.EnableGroup = *req.EnableGroup
		input.EnableGroupBuy = *req.EnableGroup
	}
	if req.EnableTakeout != nil {
		input.EnableTakeout = *req.EnableTakeout
		input.AllowDelivery = *req.EnableTakeout
	}
	if req.DealStock != nil {
		input.DealStock = *req.DealStock
		input.Stock = *req.DealStock
	}
	if req.GroupStock != nil {
		input.GroupStock = *req.GroupStock
	}
	if req.TakeoutStock != nil {
		input.TakeoutStock = *req.TakeoutStock
	}
	if req.IsHot != nil {
		input.IsHot = *req.IsHot
	}
	if req.EnableGroupBuy != nil {
		input.EnableGroupBuy = *req.EnableGroupBuy
		if req.EnableGroup == nil {
			input.EnableGroup = *req.EnableGroupBuy
		}
	}
	if req.EnableCoupon != nil {
		input.EnableCoupon = *req.EnableCoupon
	}
	if req.AllowPickup != nil {
		input.AllowPickup = *req.AllowPickup
	}
	if req.AllowDelivery != nil {
		input.AllowDelivery = *req.AllowDelivery
		if req.EnableTakeout == nil {
			input.EnableTakeout = *req.AllowDelivery
		}
	}
	if req.GroupBuyTargetCount != nil {
		input.GroupBuyTargetCount = req.GroupBuyTargetCount
	}
	if req.GroupBuyPrice != nil {
		input.GroupBuyPrice = req.GroupBuyPrice
	}
	if req.GroupBuyAllowRepeat != nil {
		input.GroupBuyAllowRepeat = *req.GroupBuyAllowRepeat
	}
	if req.GroupBuyMaxConcurrentTeams != nil {
		input.GroupBuyMaxConcurrentTeams = *req.GroupBuyMaxConcurrentTeams
	}
	if req.DealExpireDays != nil {
		input.DealExpireDays = req.DealExpireDays
	}
	if req.GroupExpireDays != nil {
		input.GroupExpireDays = req.GroupExpireDays
	}
	if req.ItemType != nil {
		input.ItemType = *req.ItemType
	}
	if req.Status != nil {
		input.Status = *req.Status
	}
	if len(req.PackageGroups) > 0 {
		input.PackageGroups = req.PackageGroups
	}
	if req.OptionGroups != nil {
		input.OptionGroups = req.OptionGroups
	}
	if req.ApplicableMerchantIDs != nil {
		input.HasApplicableMerchantIDs = true
		input.ApplicableMerchantIDs = *req.ApplicableMerchantIDs
	}
	if req.ForceCloseGroup != nil && *req.ForceCloseGroup == 1 {
		input.ForceCloseGroup = true
	}
	return input
}

func toUpdateMerchantInput(req UpdateMerchantProfileRequest) (service.UpdateMerchantInput, error) {
	coords, err := parseMerchantCoordinatePatch(req.Latitude, req.Longitude, req.Lat, req.Lng)
	if err != nil {
		return service.UpdateMerchantInput{}, err
	}
	return service.UpdateMerchantInput{
		ShopName:         req.ShopName,
		ContactPhone:     req.ContactPhone,
		Address:          req.Address,
		ShopLogo:         req.ShopLogo,
		Images:           req.Images,
		Coordinates:      coords,
		AllowReservation: req.AllowReservation,
		AutoApprove:      req.AutoApprove,
		OpenTime:         req.OpenTime,
		CloseTime:        req.CloseTime,
		DeliveryFee:      req.DeliveryFee,
		RiderEarnings:    req.RiderEarnings,
	}, nil
}

func parseMerchantCoordinatePatch(lat, lng, latAlias, lngAlias FlexNullableFloat64) (*service.MerchantCoordinateUpdate, error) {
	l := lat
	if !l.Present {
		l = latAlias
	}
	g := lng
	if !g.Present {
		g = lngAlias
	}
	if !l.Present && !g.Present {
		return nil, nil
	}
	if l.Present != g.Present {
		return nil, errors.New("latitude 与 longitude 需成对填写")
	}
	if l.Null && g.Null {
		return &service.MerchantCoordinateUpdate{Update: true, Clear: true}, nil
	}
	if l.Null || g.Null {
		return nil, errors.New("latitude 与 longitude 需成对填写")
	}
	return &service.MerchantCoordinateUpdate{
		Update: true,
		Lat:    l.Value,
		Lng:    g.Value,
	}, nil
}

func validateProductCategoryPatch(req UpdateProductRequest) error {
	if req.CategoryID == nil && req.CategoryName == nil {
		return nil
	}
	if req.CategoryID != nil && *req.CategoryID > 0 {
		return nil
	}
	if req.CategoryName != nil && strings.TrimSpace(*req.CategoryName) != "" {
		return nil
	}
	return errors.New("请指定 category_id 或 category_name")
}

func validateProductCategory(req ProductRequest, allowEmpty bool) error {
	if req.CategoryID > 0 {
		return nil
	}
	if req.CategoryName != nil && strings.TrimSpace(*req.CategoryName) != "" {
		return nil
	}
	if allowEmpty {
		return nil
	}
	return errors.New("请指定 category_id 或 category_name")
}

func parsePage(c *gin.Context) (int, int) {
	var p query.Page
	_ = c.ShouldBindQuery(&p)
	page, pageSize, _ := p.Normalize()
	return page, pageSize
}

func parseUintParam(c *gin.Context, name string) (uint64, error) {
	return strconv.ParseUint(c.Param(name), 10, 64)
}

func parseProductListFilter(c *gin.Context, merchantScoped bool) (service.ProductListFilter, error) {
	filter := service.ProductListFilter{Keyword: c.Query("keyword")}
	if !merchantScoped {
		if s := c.Query("merchant_id"); s != "" {
			v, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return filter, errors.New("merchant_id 参数无效")
			}
			filter.MerchantID = &v
		}
	}
	if s := c.Query("category_id"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return filter, errors.New("category_id 参数无效")
		}
		filter.CategoryID = &v
	}
	if s := c.Query("status"); s != "" {
		v, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			return filter, errors.New("status 参数无效")
		}
		u := uint8(v)
		filter.Status = &u
	}
	if c.Query("group_buy") == "1" {
		filter.EnableGroupBuyOnly = true
	}
	if c.Query("pickup") == "1" {
		filter.AllowPickupOnly = true
	}
	if c.Query("exclude_package") == "1" {
		filter.ExcludePackage = true
	}
	if s := c.Query("item_type"); s != "" {
		v, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			return filter, errors.New("item_type 参数无效")
		}
		u := uint8(v)
		filter.ItemType = &u
	}
	return filter, nil
}
