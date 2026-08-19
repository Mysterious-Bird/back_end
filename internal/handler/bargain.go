package handler

import (
	"errors"

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
	case errors.Is(err, service.ErrBargainConflict):
		response.BadRequest(c, "砍价状态冲突")
	case errors.Is(err, service.ErrActivityNotFound), errors.Is(err, service.ErrActivityProductNotFound):
		response.Fail(c, 404, 404, "活动商品不存在")
	case errors.Is(err, service.ErrActivityNotActive):
		response.BadRequest(c, "活动未开始或已结束")
	default:
		c.Error(err)
		response.InternalError(c, "砍价失败")
	}
}
