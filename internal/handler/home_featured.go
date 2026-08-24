package handler

import (
	"errors"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type HomeFeaturedHandler struct {
	Svc *service.HomeFeaturedService
}

type homeFeaturedCreateReq struct {
	Section           string  `json:"section"`
	ItemType          string  `json:"item_type"`
	ProductID         uint64  `json:"product_id"`
	MerchantID        uint64  `json:"merchant_id"`
	ActivityID        *uint64 `json:"activity_id"`
	ActivityProductID *uint64 `json:"activity_product_id"`
	Channel           string  `json:"channel"`
	SortOrder         int     `json:"sort_order"`
	Status            *uint8  `json:"status"`
}

type homeFeaturedPatchReq struct {
	SortOrder *int   `json:"sort_order"`
	Status    *uint8 `json:"status"`
}

type homeFeaturedReorderReq struct {
	Section string   `json:"section" binding:"required"`
	IDs     []uint64 `json:"ids" binding:"required"`
}

func (h *HomeFeaturedHandler) ListPublic(c *gin.Context) {
	section := c.Query("section")
	if section == "" {
		response.BadRequest(c, "请指定 section")
		return
	}
	cfg, err := h.Svc.ListPublicConfig(section)
	if err != nil {
		writeHomeFeaturedErr(c, err)
		return
	}
	response.OK(c, cfg)
}

func (h *HomeFeaturedHandler) ListAdmin(c *gin.Context) {
	section := c.Query("section")
	if section == "" {
		response.BadRequest(c, "请指定 section")
		return
	}
	list, err := h.Svc.ListAdmin(section)
	if err != nil {
		writeHomeFeaturedErr(c, err)
		return
	}
	response.OK(c, gin.H{"list": list})
}

func (h *HomeFeaturedHandler) Create(c *gin.Context) {
	var req homeFeaturedCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if req.Section == "" {
		response.BadRequest(c, "请指定 section")
		return
	}
	status := uint8(model.HomeFeaturedStatusOn)
	if req.Status != nil {
		status = *req.Status
	}
	item, err := h.Svc.Create(service.HomeFeaturedCreateInput{
		Section:           req.Section,
		ItemType:          req.ItemType,
		ProductID:         req.ProductID,
		MerchantID:        req.MerchantID,
		ActivityID:        req.ActivityID,
		ActivityProductID: req.ActivityProductID,
		Channel:           req.Channel,
		SortOrder:         req.SortOrder,
		Status:            status,
	})
	if err != nil {
		writeHomeFeaturedErr(c, err)
		return
	}
	response.OK(c, item)
}

func (h *HomeFeaturedHandler) Update(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req homeFeaturedPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	item, err := h.Svc.Update(id, service.HomeFeaturedPatch{SortOrder: req.SortOrder, Status: req.Status})
	if err != nil {
		writeHomeFeaturedErr(c, err)
		return
	}
	response.OK(c, item)
}

func (h *HomeFeaturedHandler) Delete(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.Svc.Delete(id); err != nil {
		writeHomeFeaturedErr(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *HomeFeaturedHandler) Reorder(c *gin.Context) {
	var req homeFeaturedReorderReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "参数无效")
		return
	}
	if err := h.Svc.Reorder(req.Section, req.IDs); err != nil {
		writeHomeFeaturedErr(c, err)
		return
	}
	list, err := h.Svc.ListAdmin(req.Section)
	if err != nil {
		response.InternalError(c, "获取列表失败")
		return
	}
	response.OK(c, gin.H{"list": list})
}

func writeHomeFeaturedErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrHomeFeaturedNotFound):
		response.BadRequest(c, "展示项不存在")
	case errors.Is(err, service.ErrHomeFeaturedDup):
		response.BadRequest(c, "该项已在列表中")
	case errors.Is(err, service.ErrHomeFeaturedBadSection):
		response.BadRequest(c, "section 无效")
	case errors.Is(err, service.ErrHomeFeaturedBadProduct):
		response.BadRequest(c, "商品不存在或无效")
	case errors.Is(err, service.ErrHomeFeaturedBadMerchant):
		response.BadRequest(c, "商家不存在或无效")
	case errors.Is(err, service.ErrHomeFeaturedBadLink):
		response.BadRequest(c, "活动或拼团链接无效")
	default:
		response.InternalError(c, "操作失败")
	}
}
