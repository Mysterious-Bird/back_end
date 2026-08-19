package handler

import (
	"errors"
	"strconv"
	"strings"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type BargainHandler struct {
	BargainSvc *service.BargainService
}

type startBargainBody struct {
	ActivityProductID uint64 `json:"activity_product_id" binding:"required"`
}

func (h *BargainHandler) Start(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var body startBargainBody
	if err := c.ShouldBindJSON(&body); err != nil || body.ActivityProductID == 0 {
		response.BadRequest(c, "请指定 activity_product_id")
		return
	}
	view, err := h.BargainSvc.StartSession(accountID, body.ActivityProductID)
	if err != nil {
		handleBargainError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *BargainHandler) Get(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var viewer *uint64
	if accountID, ok := auth.AccountID(c); ok {
		viewer = &accountID
	}
	view, err := h.BargainSvc.GetSession(id, viewer)
	if err != nil {
		handleBargainError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *BargainHandler) Help(c *gin.Context) {
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
	view, err := h.BargainSvc.Help(id, accountID)
	if err != nil {
		handleBargainError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *BargainHandler) Cancel(c *gin.Context) {
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
	view, err := h.BargainSvc.CancelSession(accountID, id)
	if err != nil {
		handleBargainError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *BargainHandler) ListMine(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var statusFilter *uint8
	if raw := strings.TrimSpace(c.Query("status")); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 8)
		if err != nil {
			response.BadRequest(c, "status 无效")
			return
		}
		v := uint8(n)
		statusFilter = &v
	}
	list, err := h.BargainSvc.ListMine(accountID, statusFilter)
	if err != nil {
		handleBargainError(c, err)
		return
	}
	response.OK(c, gin.H{"list": list, "total": len(list)})
}

func (h *BargainHandler) GetSettings(c *gin.Context) {
	row, err := h.BargainSvc.GetSettings()
	if err != nil {
		c.Error(err)
		response.InternalError(c, "读取砍价设置失败")
		return
	}
	response.OK(c, row)
}

type updateBargainSettingsBody struct {
	HelpDailyMax         *uint32 `json:"help_daily_max"`
	HelpDailyRefreshTime *string `json:"help_daily_refresh_time"`
}

func (h *BargainHandler) UpdateSettings(c *gin.Context) {
	var body updateBargainSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if body.HelpDailyMax == nil && body.HelpDailyRefreshTime == nil {
		response.BadRequest(c, "请至少修改一项")
		return
	}
	row, err := h.BargainSvc.UpdateSettings(service.UpdateBargainSettingsInput{
		HelpDailyMax:         body.HelpDailyMax,
		HelpDailyRefreshTime: body.HelpDailyRefreshTime,
	})
	if err != nil {
		handleBargainError(c, err)
		return
	}
	response.OK(c, row)
}

func handleBargainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBargainNotFound):
		response.Fail(c, 404, 404, "砍价不存在")
	case errors.Is(err, service.ErrBargainNotEnabled):
		response.BadRequest(c, "该商品未开启砍价")
	case errors.Is(err, service.ErrBargainAlreadyHelped):
		response.BadRequest(c, "您已帮砍过")
	case errors.Is(err, service.ErrBargainExpired):
		response.BadRequest(c, "砍价已过期")
	case errors.Is(err, service.ErrBargainForbidden):
		response.Fail(c, 403, 403, "无权操作该砍价")
	case errors.Is(err, service.ErrBargainDailyLimit):
		response.BadRequest(c, "今日帮砍次数已达上限")
	case errors.Is(err, service.ErrBargainNoRemain):
		response.BadRequest(c, "已砍到底价")
	case errors.Is(err, service.ErrBargainSelfCutDisabled):
		response.BadRequest(c, "该商品未开启发起人自砍")
	case errors.Is(err, service.ErrBargainConflict):
		response.BadRequest(c, "砍价状态冲突")
	case errors.Is(err, service.ErrActivityNotFound), errors.Is(err, service.ErrActivityProductNotFound):
		response.Fail(c, 404, 404, "活动商品不存在")
	case errors.Is(err, service.ErrActivityNotActive):
		response.BadRequest(c, "活动未开始或已结束")
	case errors.Is(err, service.ErrActivityLimitExceeded):
		response.BadRequest(c, "已达限购，无法发起砍价")
	case errors.Is(err, service.ErrActivityRegisterWindow):
		response.BadRequest(c, "不在新用户专享窗口内")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, err.Error())
	default:
		c.Error(err)
		response.InternalError(c, "砍价失败")
	}
}
