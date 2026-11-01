package handler

import (
	"strconv"
	"strings"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FulfillmentEventHandler struct {
	DB          *gorm.DB
	Svc         *service.FulfillmentEventService
	MerchantSvc *service.MerchantService
}

func (h *FulfillmentEventHandler) list(c *gin.Context, subjectType string, subjectID uint64) {
	list, err := h.Svc.List(subjectType, subjectID, 100)
	if err != nil {
		response.InternalError(c, "获取履约进度失败")
		return
	}
	response.OK(c, list)
}

// ListUserTakeoutEvents GET /user/takeout-orders/:id/events
func (h *FulfillmentEventHandler) ListUserTakeoutEvents(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "无效订单 ID")
		return
	}
	var to model.TakeoutOrder
	if err := query.NotDeleted(h.DB).Select("id").Where("id = ? AND account_id = ?", id, accountID).First(&to).Error; err != nil {
		response.Fail(c, 404, 404, "订单不存在")
		return
	}
	h.list(c, model.FulfillmentSubjectTakeout, id)
}

// ListUserOrderEvents GET /user/orders/:id/events
func (h *FulfillmentEventHandler) ListUserOrderEvents(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "无效订单 ID")
		return
	}
	var o model.Order
	if err := query.NotDeleted(h.DB).Select("id").Where("id = ? AND account_id = ?", id, accountID).First(&o).Error; err != nil {
		response.Fail(c, 404, 404, "订单不存在")
		return
	}
	h.list(c, model.FulfillmentSubjectOrder, id)
}

// ListUserDeliveryEvents GET /user/deliveries/:id/events
func (h *FulfillmentEventHandler) ListUserDeliveryEvents(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "无效配送单 ID")
		return
	}
	var n int64
	err = query.NotDeleted(h.DB.Model(&model.DeliveryOrder{})).Where(
		`id = ? AND (
			order_id IN (SELECT id FROM `+"`order`"+` WHERE account_id = ? AND is_deleted = 0)
			OR takeout_order_id IN (SELECT id FROM takeout_order WHERE account_id = ? AND is_deleted = 0)
			OR inventory_usage_id IN (SELECT id FROM user_inventory_usage WHERE account_id = ? AND is_deleted = 0)
		)`, id, accountID, accountID, accountID,
	).Count(&n).Error
	if err != nil || n == 0 {
		response.Fail(c, 404, 404, "配送单不存在")
		return
	}
	h.list(c, model.FulfillmentSubjectDelivery, id)
}

// ListMerchantTakeoutEvents GET /merchant/takeout-orders/:id/events
func (h *FulfillmentEventHandler) ListMerchantTakeoutEvents(c *gin.Context) {
	merchantID, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "无效订单 ID")
		return
	}
	var to model.TakeoutOrder
	if err := query.NotDeleted(h.DB).Select("id").Where("id = ? AND merchant_id = ?", id, *merchantID).First(&to).Error; err != nil {
		response.Fail(c, 404, 404, "订单不存在")
		return
	}
	h.list(c, model.FulfillmentSubjectTakeout, id)
}

// ListMerchantOrderEvents GET /merchant/orders/:id/events
func (h *FulfillmentEventHandler) ListMerchantOrderEvents(c *gin.Context) {
	merchantID, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "无效订单 ID")
		return
	}
	var o model.Order
	if err := query.NotDeleted(h.DB).Select("id").Where("id = ? AND merchant_id = ?", id, *merchantID).First(&o).Error; err != nil {
		response.Fail(c, 404, 404, "订单不存在")
		return
	}
	h.list(c, model.FulfillmentSubjectOrder, id)
}

// ListAdminEvents GET /admin/fulfillment-events?subject_type=&subject_id=
func (h *FulfillmentEventHandler) ListAdminEvents(c *gin.Context) {
	subjectType := strings.TrimSpace(c.Query("subject_type"))
	id, err := strconv.ParseUint(c.Query("subject_id"), 10, 64)
	if subjectType == "" || err != nil || id == 0 {
		response.BadRequest(c, "请提供 subject_type 与 subject_id")
		return
	}
	switch subjectType {
	case model.FulfillmentSubjectOrder, model.FulfillmentSubjectTakeout,
		model.FulfillmentSubjectDelivery, model.FulfillmentSubjectUsage,
		model.FulfillmentSubjectDeliveryFee, model.FulfillmentSubjectErrand:
	default:
		response.BadRequest(c, "subject_type 无效")
		return
	}
	h.list(c, subjectType, id)
}
