package handler

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type ActivityHandler struct {
	ActivitySvc *service.ActivityService
	MerchantSvc *service.MerchantService
}

type ActivityRequest struct {
	Name                 string    `json:"name" binding:"required"`
	Description          *string   `json:"description"`
	CoverURL             *string   `json:"cover_url"`
	BannerImages         []string  `json:"banner_images"`
	StartAt              time.Time `json:"start_at" binding:"required"`
	EndAt                time.Time `json:"end_at" binding:"required"`
	Status               uint8     `json:"status" example:"2"`
	EnableCoupon         uint8     `json:"enable_coupon" example:"1"`
	UserMaxQty           uint32    `json:"user_max_qty" example:"0"`
	UserDailyMax         uint32    `json:"user_daily_max" example:"0"`
	UserDailyRefreshTime string    `json:"user_daily_refresh_time" example:"00:00"`
	SortOrder            int       `json:"sort_order"`
}

type ActivityProductRequest struct {
	ProductID                  uint64   `json:"product_id"`
	ActivityPrice              float64  `json:"activity_price"`
	ActivityStock              uint32   `json:"activity_stock" example:"0"`
	PerUserMaxQty              uint32   `json:"per_user_max_qty" example:"0"`
	PerUserMaxOrders           uint32   `json:"per_user_max_orders" example:"0"`
	DailyMax                   uint32   `json:"daily_max" example:"0"`
	WeeklyMax                  uint32   `json:"weekly_max" example:"0"`
	MonthlyMax                 uint32   `json:"monthly_max" example:"0"`
	ActivityMax                uint32   `json:"activity_max" example:"0"`
	RegisterHours              uint32   `json:"register_hours" example:"0"`
	RegisterMax                uint32   `json:"register_max" example:"0"`
	PlatformDailyMax           uint32   `json:"platform_daily_max" example:"0"`
	DailyRefreshTime           string   `json:"daily_refresh_time" example:"00:00:00"`
	WeeklyRefreshWeekday       uint8    `json:"weekly_refresh_weekday" example:"1"`
	WeeklyRefreshTime          string   `json:"weekly_refresh_time" example:"00:00:00"`
	MonthlyRefreshDay          uint8    `json:"monthly_refresh_day" example:"1"`
	MonthlyRefreshTime         string   `json:"monthly_refresh_time" example:"00:00:00"`
	EnableGroupBuy             uint8    `json:"enable_group_buy" example:"0"`
	GroupBuyPrice              *float64 `json:"group_buy_price"`
	GroupBuyTargetCount        *uint32  `json:"group_buy_target_count"`
	GroupBuyAllowRepeat        uint8    `json:"group_buy_allow_repeat" example:"0"`
	GroupBuyMaxJoinsPerUser    uint32   `json:"group_buy_max_joins_per_user" example:"1"`
	GroupBuyMaxConcurrentTeams uint32   `json:"group_buy_max_concurrent_teams" example:"0"`
	ExpireDays                 *uint32  `json:"expire_days" example:"7"`
	EnableCoupon               uint8    `json:"enable_coupon" example:"1"`
	SortOrder                  int      `json:"sort_order"`
	Status                     uint8    `json:"status" example:"1"`
}

// activityProductAddBody 添加活动商品请求体（兼容多种前端字段写法）。
type activityProductAddBody struct {
	ProductID                  FlexUInt64     `json:"product_id"`
	ID                         FlexUInt64     `json:"id"`
	ActivityPrice              FlexFloat64    `json:"activity_price"`
	Price                      FlexFloat64    `json:"price"`
	ActivityStock              FlexUInt32     `json:"activity_stock"`
	Stock                      FlexUInt32     `json:"stock"`
	PerUserMaxQty              FlexUInt32     `json:"per_user_max_qty"`
	PerUserMaxOrders           FlexUInt32     `json:"per_user_max_orders"`
	DailyMax                   FlexUInt32     `json:"daily_max"`
	WeeklyMax                  FlexUInt32     `json:"weekly_max"`
	MonthlyMax                 FlexUInt32     `json:"monthly_max"`
	ActivityMax                FlexUInt32     `json:"activity_max"`
	RegisterHours              FlexUInt32     `json:"register_hours"`
	RegisterMax                FlexUInt32     `json:"register_max"`
	PlatformDailyMax           FlexUInt32     `json:"platform_daily_max"`
	DailyRefreshTime           string         `json:"daily_refresh_time"`
	WeeklyRefreshWeekday       FlexUInt8      `json:"weekly_refresh_weekday"`
	WeeklyRefreshTime          string         `json:"weekly_refresh_time"`
	MonthlyRefreshDay          FlexUInt8      `json:"monthly_refresh_day"`
	MonthlyRefreshTime         string         `json:"monthly_refresh_time"`
	EnableGroupBuy             FlexUInt8      `json:"enable_group_buy"`
	GroupBuyPrice              FlexFloat64Ptr `json:"group_buy_price"`
	GroupBuyTargetCount        FlexUInt32Ptr  `json:"group_buy_target_count"`
	GroupBuyAllowRepeat        FlexUInt8      `json:"group_buy_allow_repeat"`
	GroupBuyMaxJoinsPerUser    FlexUInt32     `json:"group_buy_max_joins_per_user"`
	GroupBuyMaxConcurrentTeams FlexUInt32     `json:"group_buy_max_concurrent_teams"`
	ExpireDays                 FlexUInt32Ptr  `json:"expire_days"`
	EnableCoupon               FlexUInt8      `json:"enable_coupon"`
	SortOrder                  FlexInt        `json:"sort_order"`
	Status                     FlexUInt8      `json:"status"`
}

func parseActivityProductAddBody(c *gin.Context) (ActivityProductRequest, error) {
	var raw activityProductAddBody
	if err := c.ShouldBindJSON(&raw); err != nil {
		return ActivityProductRequest{}, fmt.Errorf("请求格式错误: %w", err)
	}
	productID := raw.ProductID.Uint64()
	if productID == 0 {
		productID = raw.ID.Uint64()
	}
	price := raw.ActivityPrice.Float64()
	if price == 0 {
		price = raw.Price.Float64()
	}
	if productID == 0 {
		return ActivityProductRequest{}, fmt.Errorf("请填写 product_id（商品 ID）")
	}
	if price <= 0 {
		return ActivityProductRequest{}, fmt.Errorf("请填写 activity_price（活动价，须大于 0）")
	}
	stock := raw.ActivityStock.Uint32()
	if stock == 0 && raw.Stock.Uint32() > 0 {
		stock = raw.Stock.Uint32()
	}
	return ActivityProductRequest{
		ProductID: productID, ActivityPrice: price, ActivityStock: stock,
		PerUserMaxQty: raw.PerUserMaxQty.Uint32(), PerUserMaxOrders: raw.PerUserMaxOrders.Uint32(),
		DailyMax: raw.DailyMax.Uint32(), WeeklyMax: raw.WeeklyMax.Uint32(), MonthlyMax: raw.MonthlyMax.Uint32(),
		ActivityMax: raw.ActivityMax.Uint32(), RegisterHours: raw.RegisterHours.Uint32(), RegisterMax: raw.RegisterMax.Uint32(),
		PlatformDailyMax: raw.PlatformDailyMax.Uint32(), DailyRefreshTime: raw.DailyRefreshTime,
		WeeklyRefreshWeekday: raw.WeeklyRefreshWeekday.Uint8(), WeeklyRefreshTime: raw.WeeklyRefreshTime,
		MonthlyRefreshDay: raw.MonthlyRefreshDay.Uint8(), MonthlyRefreshTime: raw.MonthlyRefreshTime,
		EnableGroupBuy: raw.EnableGroupBuy.Uint8(), GroupBuyPrice: raw.GroupBuyPrice.Ptr(),
		GroupBuyTargetCount: raw.GroupBuyTargetCount.Ptr(), GroupBuyAllowRepeat: raw.GroupBuyAllowRepeat.Uint8(),
		GroupBuyMaxJoinsPerUser:    raw.GroupBuyMaxJoinsPerUser.Uint32(),
		GroupBuyMaxConcurrentTeams: raw.GroupBuyMaxConcurrentTeams.Uint32(),
		ExpireDays:                 raw.ExpireDays.Ptr(),
		EnableCoupon:               raw.EnableCoupon.Uint8(), SortOrder: raw.SortOrder.Int(), Status: raw.Status.Uint8(),
	}, nil
}

// activityProductUpdateBody 更新活动商品（兼容字符串数字）。
type activityProductUpdateBody struct {
	ActivityPrice              FlexFloat64Ptr     `json:"activity_price"`
	ActivityStock              FlexUInt32Ptr      `json:"activity_stock"`
	PerUserMaxQty              FlexUInt32Ptr      `json:"per_user_max_qty"`
	PerUserMaxOrders           FlexUInt32Ptr      `json:"per_user_max_orders"`
	DailyMax                   FlexUInt32Ptr      `json:"daily_max"`
	WeeklyMax                  FlexUInt32Ptr      `json:"weekly_max"`
	MonthlyMax                 FlexUInt32Ptr      `json:"monthly_max"`
	ActivityMax                FlexUInt32Ptr      `json:"activity_max"`
	RegisterHours              FlexUInt32Ptr      `json:"register_hours"`
	RegisterMax                FlexUInt32Ptr      `json:"register_max"`
	PlatformDailyMax           FlexUInt32Ptr      `json:"platform_daily_max"`
	DailyRefreshTime           FlexNullableString `json:"daily_refresh_time"`
	WeeklyRefreshWeekday       FlexUInt8Ptr       `json:"weekly_refresh_weekday"`
	WeeklyRefreshTime          FlexNullableString `json:"weekly_refresh_time"`
	MonthlyRefreshDay          FlexUInt8Ptr       `json:"monthly_refresh_day"`
	MonthlyRefreshTime         FlexNullableString `json:"monthly_refresh_time"`
	EnableGroupBuy             FlexUInt8Ptr       `json:"enable_group_buy"`
	GroupBuyPrice              FlexFloat64Ptr     `json:"group_buy_price"`
	GroupBuyTargetCount        FlexUInt32Ptr      `json:"group_buy_target_count"`
	GroupBuyAllowRepeat        FlexUInt8Ptr       `json:"group_buy_allow_repeat"`
	GroupBuyMaxJoinsPerUser    FlexUInt32Ptr      `json:"group_buy_max_joins_per_user"`
	GroupBuyMaxConcurrentTeams FlexUInt32Ptr      `json:"group_buy_max_concurrent_teams"`
	ExpireDays                 FlexUInt32Ptr      `json:"expire_days"`
	EnableCoupon               FlexUInt8Ptr       `json:"enable_coupon"`
	SortOrder                  FlexIntPtr         `json:"sort_order"`
	Status                     FlexUInt8Ptr       `json:"status"`
}

func parseActivityProductUpdateBody(c *gin.Context) (UpdateActivityProductRequest, error) {
	var raw activityProductUpdateBody
	if err := c.ShouldBindJSON(&raw); err != nil {
		return UpdateActivityProductRequest{}, fmt.Errorf("请求格式错误: %w", err)
	}
	return UpdateActivityProductRequest{
		ActivityPrice: raw.ActivityPrice.Ptr(), ActivityStock: raw.ActivityStock.Ptr(),
		PerUserMaxQty: raw.PerUserMaxQty.Ptr(), PerUserMaxOrders: raw.PerUserMaxOrders.Ptr(),
		DailyMax: raw.DailyMax.Ptr(), WeeklyMax: raw.WeeklyMax.Ptr(), MonthlyMax: raw.MonthlyMax.Ptr(),
		ActivityMax: raw.ActivityMax.Ptr(), RegisterHours: raw.RegisterHours.Ptr(), RegisterMax: raw.RegisterMax.Ptr(),
		PlatformDailyMax: raw.PlatformDailyMax.Ptr(), DailyRefreshTime: raw.DailyRefreshTime.Ptr(),
		WeeklyRefreshWeekday: raw.WeeklyRefreshWeekday.Ptr(), WeeklyRefreshTime: raw.WeeklyRefreshTime.Ptr(),
		MonthlyRefreshDay: raw.MonthlyRefreshDay.Ptr(), MonthlyRefreshTime: raw.MonthlyRefreshTime.Ptr(),
		EnableGroupBuy: raw.EnableGroupBuy.Ptr(), GroupBuyPrice: raw.GroupBuyPrice.Ptr(),
		GroupBuyTargetCount: raw.GroupBuyTargetCount.Ptr(), GroupBuyAllowRepeat: raw.GroupBuyAllowRepeat.Ptr(),
		GroupBuyMaxJoinsPerUser:    raw.GroupBuyMaxJoinsPerUser.Ptr(),
		GroupBuyMaxConcurrentTeams: raw.GroupBuyMaxConcurrentTeams.Ptr(),
		ExpireDays:                 raw.ExpireDays.Ptr(),
		EnableCoupon:               raw.EnableCoupon.Ptr(), SortOrder: raw.SortOrder.Ptr(), Status: raw.Status.Ptr(),
	}, nil
}

// UpdateActivityProductRequest 活动商品部分更新，只传需要修改的字段。
type UpdateActivityProductRequest struct {
	ActivityPrice              *float64 `json:"activity_price" example:"9.9"`
	ActivityStock              *uint32  `json:"activity_stock" example:"100"`
	PerUserMaxQty              *uint32  `json:"per_user_max_qty" example:"1"`
	PerUserMaxOrders           *uint32  `json:"per_user_max_orders" example:"0"`
	DailyMax                   *uint32  `json:"daily_max" example:"0"`
	WeeklyMax                  *uint32  `json:"weekly_max" example:"0"`
	MonthlyMax                 *uint32  `json:"monthly_max" example:"0"`
	ActivityMax                *uint32  `json:"activity_max" example:"0"`
	RegisterHours              *uint32  `json:"register_hours" example:"0"`
	RegisterMax                *uint32  `json:"register_max" example:"0"`
	PlatformDailyMax           *uint32  `json:"platform_daily_max" example:"0"`
	DailyRefreshTime           *string  `json:"daily_refresh_time" example:"00:00:00"`
	WeeklyRefreshWeekday       *uint8   `json:"weekly_refresh_weekday" example:"1"`
	WeeklyRefreshTime          *string  `json:"weekly_refresh_time" example:"00:00:00"`
	MonthlyRefreshDay          *uint8   `json:"monthly_refresh_day" example:"1"`
	MonthlyRefreshTime         *string  `json:"monthly_refresh_time" example:"00:00:00"`
	EnableGroupBuy             *uint8   `json:"enable_group_buy" example:"1"`
	GroupBuyPrice              *float64 `json:"group_buy_price" example:"7.9"`
	GroupBuyTargetCount        *uint32  `json:"group_buy_target_count" example:"3"`
	GroupBuyAllowRepeat        *uint8   `json:"group_buy_allow_repeat" example:"0"`
	GroupBuyMaxJoinsPerUser    *uint32  `json:"group_buy_max_joins_per_user" example:"1"`
	GroupBuyMaxConcurrentTeams *uint32  `json:"group_buy_max_concurrent_teams" example:"0"`
	ExpireDays                 *uint32  `json:"expire_days" example:"7"`
	EnableCoupon               *uint8   `json:"enable_coupon" example:"1"`
	SortOrder                  *int     `json:"sort_order"`
	Status                     *uint8   `json:"status" example:"1"`
}

func toActivityInput(req ActivityRequest, merchantID uint64) service.ActivityInput {
	return service.ActivityInput{
		MerchantID: merchantID, Name: req.Name, Description: req.Description,
		CoverURL: req.CoverURL, BannerImages: req.BannerImages,
		StartAt: req.StartAt, EndAt: req.EndAt, Status: req.Status,
		EnableCoupon: req.EnableCoupon, UserMaxQty: req.UserMaxQty,
		UserDailyMax: req.UserDailyMax, UserDailyRefreshTime: req.UserDailyRefreshTime,
		SortOrder: req.SortOrder,
	}
}

func toActivityProductInput(req ActivityProductRequest) service.ActivityProductInput {
	return service.ActivityProductInput{
		ProductID: req.ProductID, ActivityPrice: req.ActivityPrice,
		ActivityStock: req.ActivityStock, PerUserMaxQty: req.PerUserMaxQty,
		PerUserMaxOrders: req.PerUserMaxOrders,
		DailyMax:         req.DailyMax, WeeklyMax: req.WeeklyMax, MonthlyMax: req.MonthlyMax,
		ActivityMax: req.ActivityMax, RegisterHours: req.RegisterHours, RegisterMax: req.RegisterMax,
		PlatformDailyMax: req.PlatformDailyMax, DailyRefreshTime: req.DailyRefreshTime,
		WeeklyRefreshWeekday: req.WeeklyRefreshWeekday, WeeklyRefreshTime: req.WeeklyRefreshTime,
		MonthlyRefreshDay: req.MonthlyRefreshDay, MonthlyRefreshTime: req.MonthlyRefreshTime,
		EnableGroupBuy: req.EnableGroupBuy,
		GroupBuyPrice:  req.GroupBuyPrice, GroupBuyTargetCount: req.GroupBuyTargetCount,
		GroupBuyAllowRepeat:        req.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser:    req.GroupBuyMaxJoinsPerUser,
		GroupBuyMaxConcurrentTeams: req.GroupBuyMaxConcurrentTeams,
		ExpireDays:                 req.ExpireDays,
		EnableCoupon:               req.EnableCoupon, SortOrder: req.SortOrder, Status: req.Status,
	}
}

func toActivityProductPatch(req UpdateActivityProductRequest) service.UpdateActivityProductPatch {
	return service.UpdateActivityProductPatch{
		ActivityPrice: req.ActivityPrice, ActivityStock: req.ActivityStock,
		PerUserMaxQty: req.PerUserMaxQty, PerUserMaxOrders: req.PerUserMaxOrders,
		DailyMax: req.DailyMax, WeeklyMax: req.WeeklyMax, MonthlyMax: req.MonthlyMax,
		ActivityMax: req.ActivityMax, RegisterHours: req.RegisterHours, RegisterMax: req.RegisterMax,
		PlatformDailyMax: req.PlatformDailyMax, DailyRefreshTime: req.DailyRefreshTime,
		WeeklyRefreshWeekday: req.WeeklyRefreshWeekday, WeeklyRefreshTime: req.WeeklyRefreshTime,
		MonthlyRefreshDay: req.MonthlyRefreshDay, MonthlyRefreshTime: req.MonthlyRefreshTime,
		EnableGroupBuy: req.EnableGroupBuy, GroupBuyPrice: req.GroupBuyPrice,
		GroupBuyTargetCount: req.GroupBuyTargetCount, GroupBuyAllowRepeat: req.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser:    req.GroupBuyMaxJoinsPerUser,
		GroupBuyMaxConcurrentTeams: req.GroupBuyMaxConcurrentTeams,
		ExpireDays:                 req.ExpireDays,
		EnableCoupon:               req.EnableCoupon, SortOrder: req.SortOrder, Status: req.Status,
	}
}

func parseActivityProductID(c *gin.Context) (uint64, error) {
	return parseUintParam(c, "activity_product_id")
}

// ListMerchantActivities godoc
// @Summary      本店活动列表
// @Tags         商家端-活动
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/activities [get]
func (h *ActivityHandler) ListMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.ActivitySvc.List(page, pageSize, service.ActivityListFilter{MerchantID: scope})
	if err != nil {
		response.InternalError(c, "获取活动失败")
		return
	}
	response.OK(c, query.PageResult{List: h.ActivitySvc.ToStoreViews(list, false), Total: total, Page: page, PageSize: pageSize})
}

// CreateMerchantActivity godoc
// @Summary      创建活动
// @Tags         商家端-活动
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  ActivityRequest  true  "活动信息"
// @Success      200   {object}  response.Body{data=model.Activity}
// @Router       /merchant/activities [post]
func (h *ActivityHandler) CreateMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	var req ActivityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	act, err := h.ActivitySvc.Create(toActivityInput(req, *scope))
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, act)
}

// GetMerchantActivity godoc
// @Summary      活动详情（含商品）
// @Tags         商家端-活动
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "活动 ID"
// @Success      200  {object}  response.Body{data=model.Activity}
// @Router       /merchant/activities/{id} [get]
func (h *ActivityHandler) GetMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.ActivitySvc.GetDetailView(id, scope)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, view)
}

// UpdateMerchantActivity godoc
// @Summary      更新活动
// @Tags         商家端-活动
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int              true  "活动 ID"
// @Param        body  body  ActivityRequest  true  "活动信息"
// @Success      200   {object}  response.Body{data=model.Activity}
// @Router       /merchant/activities/{id} [patch]
func (h *ActivityHandler) UpdateMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ActivityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	act, err := h.ActivitySvc.Update(id, toActivityInput(req, *scope), scope)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, act)
}

// DeleteMerchantActivity godoc
// @Summary      删除活动
// @Tags         商家端-活动
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "活动 ID"
// @Success      200  {object}  response.Body
// @Router       /merchant/activities/{id} [delete]
func (h *ActivityHandler) DeleteMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.ActivitySvc.Delete(id, scope); err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, nil)
}

// ListMerchantActivityProducts godoc
// @Summary      活动商品列表
// @Tags         商家端-活动
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "活动 ID"
// @Success      200  {object}  response.Body{data=[]model.ActivityProduct}
// @Router       /merchant/activities/{id}/products [get]
func (h *ActivityHandler) ListMerchantProducts(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	list, err := h.ActivitySvc.ListProductItemViews(id, scope, false)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, list)
}

// AddMerchantActivityProduct godoc
// @Summary      添加活动商品
// @Tags         商家端-活动
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                      true  "活动 ID"
// @Param        body  body  ActivityProductRequest   true  "活动商品配置"
// @Success      200   {object}  response.Body{data=model.ActivityProduct}
// @Router       /merchant/activities/{id}/products [post]
func (h *ActivityHandler) AddProduct(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ActivityProductRequest
	if parsed, err := parseActivityProductAddBody(c); err != nil {
		response.BadRequest(c, err.Error())
		return
	} else {
		req = parsed
	}
	ap, err := h.ActivitySvc.AddProduct(activityID, toActivityProductInput(req), scope)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	view, err := h.ActivitySvc.GetProductItemView(activityID, ap.ID, scope)
	if err != nil {
		response.OK(c, ap)
		return
	}
	response.OK(c, view)
}

// GetMerchantActivityProduct godoc
// @Summary      活动商品详情
// @Tags         商家端-活动
// @Produce      json
// @Security     BearerAuth
// @Param        id                   path  int  true  "活动 ID"
// @Param        activity_product_id  path  int  true  "活动商品 ID（activity_product.id）"
// @Success      200  {object}  response.Body{data=model.ActivityProduct}
// @Router       /merchant/activities/{id}/products/{activity_product_id} [get]
func (h *ActivityHandler) GetMerchantProduct(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseActivityProductID(c)
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	view, err := h.ActivitySvc.GetProductItemView(activityID, apID, scope)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, view)
}

// UpdateMerchantActivityProduct godoc
// @Summary      更新活动商品
// @Description  部分更新，只传需要修改的字段；路径 activity_product_id 为 activity_product 表主键
// @Tags         商家端-活动
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id                   path  int                           true  "活动 ID"
// @Param        activity_product_id  path  int                           true  "活动商品 ID"
// @Param        body                 body  UpdateActivityProductRequest  true  "要更新的字段"
// @Success      200  {object}  response.Body{data=model.ActivityProduct}
// @Router       /merchant/activities/{id}/products/{activity_product_id} [patch]
func (h *ActivityHandler) UpdateProduct(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseActivityProductID(c)
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	req, err := parseActivityProductUpdateBody(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ap, err := h.ActivitySvc.UpdateProductInActivity(activityID, apID, toActivityProductPatch(req), scope)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	view, err := h.ActivitySvc.GetProductItemView(activityID, ap.ID, scope)
	if err != nil {
		response.OK(c, ap)
		return
	}
	response.OK(c, view)
}

// RemoveMerchantActivityProduct godoc
// @Summary      移除活动商品
// @Tags         商家端-活动
// @Produce      json
// @Security     BearerAuth
// @Param        id                   path  int  true  "活动 ID"
// @Param        activity_product_id  path  int  true  "活动商品 ID"
// @Success      200  {object}  response.Body
// @Router       /merchant/activities/{id}/products/{activity_product_id} [delete]
func (h *ActivityHandler) RemoveProduct(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseActivityProductID(c)
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	if err := h.ActivitySvc.RemoveProductInActivity(activityID, apID, scope); err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateMerchantActivityProductFull godoc
// @Summary      全量更新活动商品
// @Description  与 PATCH 相同，便于部分客户端使用 PUT
// @Tags         商家端-活动
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id                   path  int                           true  "活动 ID"
// @Param        activity_product_id  path  int                           true  "活动商品 ID"
// @Param        body                 body  UpdateActivityProductRequest  true  "要更新的字段"
// @Success      200  {object}  response.Body{data=model.ActivityProduct}
// @Router       /merchant/activities/{id}/products/{activity_product_id} [put]
func (h *ActivityHandler) UpdateProductPut(c *gin.Context) {
	h.UpdateProduct(c)
}

// ListPublicActivities godoc
// @Summary      商家进行中的活动列表
// @Tags         用户-商城
// @Produce      json
// @Param        merchant_id  path  int  true  "商家 ID"
// @Success      200  {object}  response.Body
// @Router       /merchants/{id}/activities [get]
func (h *ActivityHandler) ListPublicByMerchant(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "merchant_id 无效")
		return
	}
	list, total, err := h.ActivitySvc.List(1, 50, service.ActivityListFilter{
		MerchantID: &merchantID, ActiveOnly: true,
	})
	if err != nil {
		response.InternalError(c, "获取活动失败")
		return
	}
	response.OK(c, query.PageResult{List: h.ActivitySvc.ToStoreViews(list, true), Total: total, Page: 1, PageSize: 50})
}

// GetPublicActivity godoc
// @Summary      活动详情（子商店）
// @Tags         用户-商城
// @Produce      json
// @Param        id  path  int  true  "活动 ID"
// @Success      200  {object}  response.Body{data=service.ActivityPublicDetailView}
// @Router       /activities/{id} [get]
func (h *ActivityHandler) GetPublic(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.ActivitySvc.GetStoreDetail(id)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, view)
}

// ListSeckillProducts godoc
// @Summary      首页秒杀商品列表
// @Description  进行中活动的上架商品；可选登录。未登录或非新用户窗内不返回 register_hours>0 商品；达限仍返回并带 limit_reached/limit_reason
// @Tags         用户-商城
// @Produce      json
// @Success      200  {object}  response.Body{data=[]service.SeckillProductView}
// @Router       /seckill/products [get]
func (h *ActivityHandler) ListSeckillProducts(c *gin.Context) {
	var accountID *uint64
	if id, ok := auth.AccountID(c); ok {
		accountID = &id
	}
	list, err := h.ActivitySvc.ListSeckillForUser(accountID)
	if err != nil {
		c.Error(err)
		response.InternalError(c, "获取秒杀列表失败")
		return
	}
	response.OK(c, list)
}

// ListPublicActivityProducts godoc
// @Summary      活动商品列表（子商店）
// @Description  含直购/拼团价格 sale_options；group_buy=1 仅返回可团购商品
// @Tags         用户-商城
// @Produce      json
// @Param        id         path   int   true   "活动 ID"
// @Param        group_buy  query  bool  false  "仅团购商品"
// @Success      200  {object}  response.Body{data=[]service.ActivityProductStoreView}
// @Router       /activities/{id}/products [get]
func (h *ActivityHandler) ListPublicProducts(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	list, err := h.ActivitySvc.ListStoreProducts(id, c.Query("group_buy") == "1")
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, list)
}

// GetPublicActivityProduct godoc
// @Summary      活动商品详情
// @Tags         用户-商城
// @Produce      json
// @Param        id                  path  int  true  "活动 ID"
// @Param        activity_product_id path  int  true  "活动商品 ID"
// @Success      200  {object}  response.Body{data=service.ActivityProductStoreView}
// @Router       /activities/{id}/products/{activity_product_id} [get]
func (h *ActivityHandler) GetPublicProduct(c *gin.Context) {
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseUintParam(c, "activity_product_id")
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	var accountID *uint64
	if id, ok := auth.AccountID(c); ok {
		accountID = &id
	}
	view, err := h.ActivitySvc.GetStoreProductForUser(activityID, apID, accountID)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, view)
}

func handleActivityError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCatalogInUse):
		msg := service.CatalogInUseMessage(err)
		data := gin.H{"reason": "catalog_in_use"}
		var inUse *service.CatalogInUseError
		if errors.As(err, &inUse) && inUse != nil {
			data["reason"] = inUse.Reason
		}
		response.FailWithData(c, 409, 40910, msg, data)
	case errors.Is(err, service.ErrActivityNotFound), errors.Is(err, service.ErrActivityProductNotFound):
		response.Fail(c, 404, 404, "活动不存在")
	case errors.Is(err, service.ErrActivityNotActive):
		response.BadRequest(c, "活动未开始或已结束")
	case errors.Is(err, service.ErrActivityForbidden):
		response.Fail(c, 403, 403, "无权操作")
	case errors.Is(err, service.ErrProductNotFound):
		response.Fail(c, 404, 404, "商品不存在")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, "活动商品参数无效：请确认 activity_price>0；开启拼团时须填写 group_buy_price（>0）和 group_buy_target_count（≥2）")
	default:
		response.InternalError(c, "操作失败")
	}
}

// ——— 管理端：平台跨店活动（merchant_id=0）———

func (h *ActivityHandler) ListAdmin(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.ActivitySvc.List(page, pageSize, service.ActivityListFilter{})
	if err != nil {
		response.InternalError(c, "获取活动失败")
		return
	}
	response.OK(c, query.PageResult{List: h.ActivitySvc.ToStoreViews(list, false), Total: total, Page: page, PageSize: pageSize})
}

func (h *ActivityHandler) CreateAdmin(c *gin.Context) {
	var req ActivityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	act, err := h.ActivitySvc.Create(toActivityInput(req, 0))
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, act)
}

func (h *ActivityHandler) GetAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.ActivitySvc.GetDetailView(id, nil)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *ActivityHandler) UpdateAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ActivityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	act, err := h.ActivitySvc.Update(id, toActivityInput(req, 0), nil)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, act)
}

func (h *ActivityHandler) DeleteAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.ActivitySvc.Delete(id, nil); err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *ActivityHandler) ListAdminProducts(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	list, err := h.ActivitySvc.ListProductItemViews(id, nil, false)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, list)
}

func (h *ActivityHandler) AddAdminProduct(c *gin.Context) {
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	req, err := parseActivityProductAddBody(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ap, err := h.ActivitySvc.AddProduct(activityID, toActivityProductInput(req), nil)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	view, err := h.ActivitySvc.GetProductItemView(activityID, ap.ID, nil)
	if err != nil {
		response.OK(c, ap)
		return
	}
	response.OK(c, view)
}

func (h *ActivityHandler) GetAdminProduct(c *gin.Context) {
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseActivityProductID(c)
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	view, err := h.ActivitySvc.GetProductItemView(activityID, apID, nil)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *ActivityHandler) UpdateAdminProduct(c *gin.Context) {
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseActivityProductID(c)
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	req, err := parseActivityProductUpdateBody(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ap, err := h.ActivitySvc.UpdateProductInActivity(activityID, apID, toActivityProductPatch(req), nil)
	if err != nil {
		handleActivityError(c, err)
		return
	}
	view, err := h.ActivitySvc.GetProductItemView(activityID, ap.ID, nil)
	if err != nil {
		response.OK(c, ap)
		return
	}
	response.OK(c, view)
}

func (h *ActivityHandler) UpdateAdminProductPut(c *gin.Context) {
	h.UpdateAdminProduct(c)
}

func (h *ActivityHandler) RemoveAdminProduct(c *gin.Context) {
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseActivityProductID(c)
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	if err := h.ActivitySvc.RemoveProductInActivity(activityID, apID, nil); err != nil {
		handleActivityError(c, err)
		return
	}
	response.OK(c, nil)
}

var _ model.Activity
