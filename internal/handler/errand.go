package handler

import (
	"errors"
	"strconv"
	"strings"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func errandAccountID(c *gin.Context) (uint64, bool) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return 0, false
	}
	return accountID, true
}

func parseErrandStatus(c *gin.Context) (*uint8, bool) {
	raw := c.Query("status")
	if raw == "" {
		return nil, true
	}
	v, err := strconv.ParseUint(raw, 10, 8)
	if err != nil {
		response.BadRequest(c, "status 参数无效")
		return nil, false
	}
	s := uint8(v)
	return &s, true
}

// parseErrandStatuses 支持 status 单值或 statuses 逗号分隔多值（用户端「进行中」分组）。
func parseErrandStatuses(c *gin.Context) ([]uint8, bool) {
	raw := c.Query("statuses")
	if raw == "" {
		status, ok := parseErrandStatus(c)
		if !ok {
			return nil, false
		}
		if status == nil {
			return nil, true
		}
		return []uint8{*status}, true
	}
	parts := strings.Split(raw, ",")
	out := make([]uint8, 0, len(parts))
	for _, part := range parts {
		v, err := strconv.ParseUint(strings.TrimSpace(part), 10, 8)
		if err != nil {
			response.BadRequest(c, "statuses 参数无效")
			return nil, false
		}
		out = append(out, uint8(v))
	}
	return out, true
}

type ErrandHandler struct {
	Svc *service.ErrandService
}

func handleErrandError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrErrandNotFound):
		response.Fail(c, 404, 404, "跑腿订单不存在")
	case errors.Is(err, service.ErrErrandUnauthorized):
		response.Fail(c, 403, 403, "无权操作该跑腿订单")
	case errors.Is(err, service.ErrErrandStatusInvalid), errors.Is(err, service.ErrErrandGoodsInvalid):
		msg := err.Error()
		if idx := strings.Index(msg, ": "); idx >= 0 {
			msg = msg[idx+2:]
		}
		response.BadRequest(c, msg)
	default:
		response.InternalError(c, "操作失败")
	}
}

// Create 用户下单（无需支付）。
func (h *ErrandHandler) Create(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	var in service.ErrandCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.Svc.Create(accountID, in)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

// List 用户跑腿单列表。
func (h *ErrandHandler) List(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	page, pageSize := parsePage(c)
	statuses, ok := parseErrandStatuses(c)
	if !ok {
		return
	}
	list, total, err := h.Svc.List(accountID, page, pageSize, statuses)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, &query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// Get 用户跑腿单详情。
func (h *ErrandHandler) Get(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	view, err := h.Svc.Get(accountID, id)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

type errandCancelRequest struct {
	Reason string `json:"reason"`
}

// Cancel 用户取消（未收款不涉及退款）。
func (h *ErrandHandler) Cancel(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	var req errandCancelRequest
	_ = c.ShouldBindJSON(&req)
	view, err := h.Svc.Cancel(accountID, id, req.Reason)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

// Pay 用户送达时平台支付。
func (h *ErrandHandler) Pay(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	prepay, err := h.Svc.CreatePrepay(accountID, id)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, prepay)
}

// Confirm 用户确认送达。
func (h *ErrandHandler) Confirm(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	view, err := h.Svc.Confirm(accountID, id)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

type errandReasonRequest struct {
	Reason string `json:"reason"`
}

// Appeal 用户申诉。
func (h *ErrandHandler) Appeal(c *gin.Context) {
	accountID, ok := errandAccountID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	var req errandReasonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.Svc.Appeal(accountID, id, req.Reason)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

// --- 骑手端：以配送单 ID 操作（骑手列表复用配送单） ---

func (h *ErrandHandler) deliveryErrand(c *gin.Context, riderID uint64) (uint64, bool) {
	deliveryID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "配送单 ID 无效")
		return 0, false
	}
	var d model.DeliveryOrder
	if err := query.NotDeleted(h.Svc.DB).First(&d, deliveryID).Error; err != nil {
		response.Fail(c, 404, 404, "配送单不存在")
		return 0, false
	}
	if d.RiderID == nil || *d.RiderID != riderID {
		response.Fail(c, 403, 403, "无权操作该配送单")
		return 0, false
	}
	if d.ErrandOrderID == nil {
		response.BadRequest(c, "该配送单不是跑腿单")
		return 0, false
	}
	return *d.ErrandOrderID, true
}

type errandPickupRequest struct {
	GoodsAmount   float64  `json:"goods_amount"`
	ReceiptPhotos []string `json:"goods_receipt_photos"`
}

// RiderPickup 骑手确认取货（无券码时代购需录商品费+小票）。
func (h *ErrandHandler) RiderPickup(c *gin.Context) {
	riderID, ok := errandAccountID(c)
	if !ok {
		return
	}
	errandID, ok := h.deliveryErrand(c, riderID)
	if !ok {
		return
	}
	var req errandPickupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.Svc.RiderPickup(riderID, errandID, req.GoodsAmount, req.ReceiptPhotos)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

type errandCompleteRequest struct {
	PayMode uint8 `json:"pay_mode" binding:"required"`
}

// RiderComplete 骑手确认送达并选择收款方式。
func (h *ErrandHandler) RiderComplete(c *gin.Context) {
	riderID, ok := errandAccountID(c)
	if !ok {
		return
	}
	errandID, ok := h.deliveryErrand(c, riderID)
	if !ok {
		return
	}
	var req errandCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.PayMode != model.ErrandPayModePlatform && req.PayMode != model.ErrandPayModeCash) {
		response.BadRequest(c, "请选择收款方式")
		return
	}
	view, err := h.Svc.RiderComplete(riderID, errandID, req.PayMode)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

// RiderException 骑手上报跑腿异常。
func (h *ErrandHandler) RiderException(c *gin.Context) {
	riderID, ok := errandAccountID(c)
	if !ok {
		return
	}
	errandID, ok := h.deliveryErrand(c, riderID)
	if !ok {
		return
	}
	var req errandReasonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.Svc.ReportExceptionByRider(riderID, errandID, req.Reason)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

// --- 管理端 ---

// AdminList 管理端跑腿单列表。
func (h *ErrandHandler) AdminList(c *gin.Context) {
	page, pageSize := parsePage(c)
	status, ok := parseErrandStatus(c)
	if !ok {
		return
	}
	list, total, err := h.Svc.AdminList(status, c.Query("keyword"), page, pageSize)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, &query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// AdminGet 管理端详情。
func (h *ErrandHandler) AdminGet(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	view, err := h.Svc.AdminGet(id)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

type errandAdminResolveRequest struct {
	Remark string `json:"remark"`
}

// AdminResume 异常恢复。
func (h *ErrandHandler) AdminResume(c *gin.Context) {
	h.adminResolve(c, h.Svc.AdminResume)
}

// AdminReassign 异常改派。
func (h *ErrandHandler) AdminReassign(c *gin.Context) {
	h.adminResolve(c, h.Svc.AdminReassign)
}

// AdminCancel 异常取消退款。
func (h *ErrandHandler) AdminCancel(c *gin.Context) {
	h.adminResolve(c, h.Svc.AdminCancel)
}

func (h *ErrandHandler) adminResolve(c *gin.Context, fn func(uint64, string) (*service.ErrandView, error)) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	var req errandAdminResolveRequest
	_ = c.ShouldBindJSON(&req)
	view, err := fn(id, req.Remark)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, view)
}

// AdminGetSettings 跑腿配置。
func (h *ErrandHandler) AdminGetSettings(c *gin.Context) {
	response.OK(c, service.LoadErrandSettings(h.Svc.DB))
}

type errandUserSettings struct {
	DeliveryFee      float64 `json:"delivery_fee"`
	AutoConfirmHours int     `json:"auto_confirm_hours"`
	GoodsMaxAmount   float64 `json:"goods_max_amount"`
}

// UserGetSettings 用户端只读配置（费用展示，不暴露平台抽成）。
func (h *ErrandHandler) UserGetSettings(c *gin.Context) {
	settings := service.LoadErrandSettings(h.Svc.DB)
	response.OK(c, &errandUserSettings{
		DeliveryFee:      settings.DeliveryFee,
		AutoConfirmHours: settings.AutoConfirmHours,
		GoodsMaxAmount:   settings.GoodsMaxAmount,
	})
}// AdminUpdateSettings 更新跑腿配置。
func (h *ErrandHandler) AdminUpdateSettings(c *gin.Context) {
	var in model.ErrandSettings
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	settings, err := h.Svc.UpdateErrandSettings(in)
	if err != nil {
		handleErrandError(c, err)
		return
	}
	response.OK(c, settings)
}
