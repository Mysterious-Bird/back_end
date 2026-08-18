package handler

import (
	"errors"

	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type HomeCarouselHandler struct {
	Svc *service.HomeCarouselService
}

type homeCarouselCreateReq struct {
	ProductID         uint64  `json:"product_id"`
	ActivityID        *uint64 `json:"activity_id"`
	ActivityProductID *uint64 `json:"activity_product_id"`
	Channel           string  `json:"channel"`
	SortOrder         int     `json:"sort_order"`
	Status            *uint8  `json:"status"`
}

type homeCarouselPatchReq struct {
	SortOrder *int   `json:"sort_order"`
	Status    *uint8 `json:"status"`
}

type homeCarouselReorderReq struct {
	IDs []uint64 `json:"ids" binding:"required"`
}

func (h *HomeCarouselHandler) ListPublic(c *gin.Context) {
	list, err := h.Svc.ListPublic()
	if err != nil {
		response.InternalError(c, "获取轮播失败")
		return
	}
	response.OK(c, gin.H{"list": list})
}

func (h *HomeCarouselHandler) ListAdmin(c *gin.Context) {
	list, err := h.Svc.ListAdmin()
	if err != nil {
		response.InternalError(c, "获取轮播失败")
		return
	}
	response.OK(c, gin.H{"list": list})
}

func (h *HomeCarouselHandler) Create(c *gin.Context) {
	var req homeCarouselCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if req.ProductID == 0 && (req.ActivityProductID == nil || *req.ActivityProductID == 0) {
		response.BadRequest(c, "请选择商品")
		return
	}
	status := uint8(1)
	if req.Status != nil {
		status = *req.Status
	}
	item, err := h.Svc.Create(service.HomeCarouselCreateInput{
		ProductID:         req.ProductID,
		ActivityID:        req.ActivityID,
		ActivityProductID: req.ActivityProductID,
		Channel:           req.Channel,
		SortOrder:         req.SortOrder,
		Status:            status,
	})
	if err != nil {
		writeHomeCarouselErr(c, err)
		return
	}
	response.OK(c, item)
}

func (h *HomeCarouselHandler) Update(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req homeCarouselPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	item, err := h.Svc.Update(id, service.HomeCarouselPatch{SortOrder: req.SortOrder, Status: req.Status})
	if err != nil {
		writeHomeCarouselErr(c, err)
		return
	}
	response.OK(c, item)
}

func (h *HomeCarouselHandler) Delete(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.Svc.Delete(id); err != nil {
		writeHomeCarouselErr(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *HomeCarouselHandler) Reorder(c *gin.Context) {
	var req homeCarouselReorderReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "ids 无效")
		return
	}
	if err := h.Svc.Reorder(req.IDs); err != nil {
		response.InternalError(c, "排序失败")
		return
	}
	list, err := h.Svc.ListAdmin()
	if err != nil {
		response.InternalError(c, "获取轮播失败")
		return
	}
	response.OK(c, gin.H{"list": list})
}

func writeHomeCarouselErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrHomeCarouselNotFound):
		response.BadRequest(c, "轮播项不存在")
	case errors.Is(err, service.ErrHomeCarouselLimit):
		response.BadRequest(c, "最多启用 8 个轮播商品")
	case errors.Is(err, service.ErrHomeCarouselDupProduct):
		response.BadRequest(c, "该商品渠道已在轮播中")
	case errors.Is(err, service.ErrHomeCarouselBadProduct):
		response.BadRequest(c, "商品不存在或无效")
	case errors.Is(err, service.ErrHomeCarouselBadLink):
		response.BadRequest(c, "活动或拼团链接无效")
	default:
		response.InternalError(c, "操作失败")
	}
}
