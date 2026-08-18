package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrHomeCarouselNotFound   = errors.New("home carousel item not found")
	ErrHomeCarouselLimit      = errors.New("home carousel limit exceeded")
	ErrHomeCarouselDupProduct = errors.New("product already in carousel")
	ErrHomeCarouselBadProduct = errors.New("product not found or off shelf")
	ErrHomeCarouselBadLink    = errors.New("invalid carousel link")
)

type HomeCarouselService struct {
	DB *gorm.DB
}

type HomeCarouselItemView struct {
	ID                uint64  `json:"id"`
	ProductID         uint64  `json:"product_id"`
	MerchantID        uint64  `json:"merchant_id"`
	Name              string  `json:"name"`
	CoverURL          string  `json:"cover_url"`
	Price             float64 `json:"price"`
	Channel           string  `json:"channel"`
	ChannelText       string  `json:"channel_text"`
	ActivityID        *uint64 `json:"activity_id,omitempty"`
	ActivityProductID *uint64 `json:"activity_product_id,omitempty"`
	SortOrder         int     `json:"sort_order"`
	Status            uint8   `json:"status"`
}

type HomeCarouselCreateInput struct {
	ProductID         uint64
	ActivityID        *uint64
	ActivityProductID *uint64
	Channel           string
	SortOrder         int
	Status            uint8
}

func normalizeCarouselChannel(ch string) string {
	switch strings.ToLower(strings.TrimSpace(ch)) {
	case model.HomeCarouselChannelGroup:
		return model.HomeCarouselChannelGroup
	default:
		return model.HomeCarouselChannelDeal
	}
}

// carouselChannelForActivityProduct 轮播渠道跟随活动商品类型：拼团活动商品展示活动拼团。
func carouselChannelForActivityProduct(ap model.ActivityProduct) string {
	if activityProductCanGroupBuy(&ap) {
		return model.HomeCarouselChannelGroup
	}
	return model.HomeCarouselChannelDeal
}

func carouselChannelText(channel string, hasActivity bool) string {
	if hasActivity {
		if channel == model.HomeCarouselChannelGroup {
			return "活动拼团"
		}
		return "活动直购"
	}
	if channel == model.HomeCarouselChannelGroup {
		return "拼团"
	}
	return "团购"
}

func (s *HomeCarouselService) ListPublic() ([]HomeCarouselItemView, error) {
	var rows []model.HomeCarousel
	err := query.NotDeleted(s.DB.Model(&model.HomeCarousel{})).
		Where("status = ?", model.HomeCarouselStatusOn).
		Order("sort_order ASC, id ASC").
		Limit(model.HomeCarouselMaxItems).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.enrich(rows, true)
}

func (s *HomeCarouselService) ListAdmin() ([]HomeCarouselItemView, error) {
	var rows []model.HomeCarousel
	err := query.NotDeleted(s.DB.Model(&model.HomeCarousel{})).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.enrich(rows, false)
}

func (s *HomeCarouselService) enrich(rows []model.HomeCarousel, publicOnlyOnShelf bool) ([]HomeCarouselItemView, error) {
	if len(rows) == 0 {
		return []HomeCarouselItemView{}, nil
	}
	productIDs := make([]uint64, 0, len(rows))
	apIDs := make([]uint64, 0)
	actIDs := make([]uint64, 0)
	for _, r := range rows {
		productIDs = append(productIDs, r.ProductID)
		if r.ActivityProductID != nil && *r.ActivityProductID > 0 {
			apIDs = append(apIDs, *r.ActivityProductID)
		}
		if r.ActivityID != nil && *r.ActivityID > 0 {
			actIDs = append(actIDs, *r.ActivityID)
		}
	}

	var products []model.Product
	q := query.NotDeleted(s.DB.Model(&model.Product{})).Where("id IN ?", productIDs)
	if publicOnlyOnShelf {
		q = q.Where("status = ?", model.ProductStatusOn)
	}
	if err := q.Find(&products).Error; err != nil {
		return nil, err
	}
	byProduct := map[uint64]model.Product{}
	for _, p := range products {
		byProduct[p.ID] = p
	}

	byAP := map[uint64]model.ActivityProduct{}
	if len(apIDs) > 0 {
		var aps []model.ActivityProduct
		aq := query.NotDeleted(s.DB.Model(&model.ActivityProduct{})).Where("id IN ?", apIDs)
		if err := aq.Find(&aps).Error; err != nil {
			return nil, err
		}
		for _, ap := range aps {
			byAP[ap.ID] = ap
		}
	}

	byAct := map[uint64]model.Activity{}
	if len(actIDs) > 0 {
		var acts []model.Activity
		if err := query.NotDeleted(s.DB.Model(&model.Activity{})).Where("id IN ?", actIDs).Find(&acts).Error; err != nil {
			return nil, err
		}
		for _, a := range acts {
			byAct[a.ID] = a
		}
	}

	now := time.Now()
	out := make([]HomeCarouselItemView, 0, len(rows))
	for _, r := range rows {
		channel := normalizeCarouselChannel(r.Channel)
		hasActivity := r.ActivityProductID != nil && *r.ActivityProductID > 0
		if hasActivity {
			if ap, ok := byAP[*r.ActivityProductID]; ok {
				channel = carouselChannelForActivityProduct(ap)
			}
		}

		if publicOnlyOnShelf && hasActivity {
			ap, ok := byAP[*r.ActivityProductID]
			if !ok || ap.Status != 1 {
				continue
			}
			aid := r.ActivityID
			if aid == nil {
				v := ap.ActivityID
				aid = &v
			}
			act, ok := byAct[*aid]
			if !ok || act.Status != model.ActivityStatusOn || now.Before(act.StartAt) || now.After(act.EndAt) {
				continue
			}
		}

		p, ok := byProduct[r.ProductID]
		if !ok {
			if publicOnlyOnShelf {
				continue
			}
			view := HomeCarouselItemView{
				ID: r.ID, ProductID: r.ProductID, Name: "(商品已删除)",
				Channel: channel, ChannelText: carouselChannelText(channel, hasActivity),
				ActivityID: r.ActivityID, ActivityProductID: r.ActivityProductID,
				SortOrder: r.SortOrder, Status: r.Status,
			}
			out = append(out, view)
			continue
		}

		price := p.Price
		if hasActivity {
			if ap, ok := byAP[*r.ActivityProductID]; ok {
				if channel == model.HomeCarouselChannelGroup && ap.GroupBuyPrice != nil && *ap.GroupBuyPrice > 0 {
					price = *ap.GroupBuyPrice
				} else {
					price = ap.ActivityPrice
				}
			}
		} else if channel == model.HomeCarouselChannelGroup && p.GroupBuyPrice != nil && *p.GroupBuyPrice > 0 {
			price = *p.GroupBuyPrice
		}

		view := HomeCarouselItemView{
			ID: r.ID, ProductID: p.ID, MerchantID: p.MerchantID,
			Name: p.Name, CoverURL: p.CoverURL, Price: price,
			Channel: channel, ChannelText: carouselChannelText(channel, hasActivity),
			ActivityID: r.ActivityID, ActivityProductID: r.ActivityProductID,
			SortOrder: r.SortOrder, Status: r.Status,
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *HomeCarouselService) countDup(productID uint64, activityProductID *uint64, channel string) (int64, error) {
	q := query.NotDeleted(s.DB.Model(&model.HomeCarousel{}))
	if activityProductID != nil && *activityProductID > 0 {
		// 活动商品轮播渠道由 AP 类型决定，同一 AP 只能出现一次。
		q = q.Where("activity_product_id = ?", *activityProductID)
	} else {
		channel = normalizeCarouselChannel(channel)
		q = q.Where("channel = ?", channel)
		q = q.Where("product_id = ? AND (activity_product_id IS NULL OR activity_product_id = 0)", productID)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

func (s *HomeCarouselService) resolveCreateLink(in HomeCarouselCreateInput) (productID uint64, activityID, activityProductID *uint64, channel string, err error) {
	channel = normalizeCarouselChannel(in.Channel)
	productID = in.ProductID

	hasAP := in.ActivityProductID != nil && *in.ActivityProductID > 0
	hasAct := in.ActivityID != nil && *in.ActivityID > 0
	if hasAP != hasAct {
		return 0, nil, nil, "", fmt.Errorf("%w: activity_id and activity_product_id required together", ErrHomeCarouselBadLink)
	}

	if hasAP {
		var ap model.ActivityProduct
		if err := query.NotDeleted(s.DB).First(&ap, *in.ActivityProductID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, nil, nil, "", ErrHomeCarouselBadLink
			}
			return 0, nil, nil, "", err
		}
		if ap.ActivityID != *in.ActivityID {
			return 0, nil, nil, "", fmt.Errorf("%w: activity mismatch", ErrHomeCarouselBadLink)
		}
		if ap.Status != 1 {
			return 0, nil, nil, "", ErrHomeCarouselBadLink
		}
		channel = carouselChannelForActivityProduct(ap)
		productID = ap.ProductID
		aid, apid := ap.ActivityID, ap.ID
		activityID, activityProductID = &aid, &apid
	}

	if productID == 0 {
		return 0, nil, nil, "", fmt.Errorf("%w: product_id", ErrHomeCarouselBadProduct)
	}
	var product model.Product
	if err := query.NotDeleted(s.DB).First(&product, productID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil, nil, "", ErrHomeCarouselBadProduct
		}
		return 0, nil, nil, "", err
	}
	if !hasAP && channel == model.HomeCarouselChannelGroup {
		if product.EnableGroup != 1 && product.EnableGroupBuy != 1 {
			return 0, nil, nil, "", fmt.Errorf("%w: product group disabled", ErrHomeCarouselBadLink)
		}
	}
	return productID, activityID, activityProductID, channel, nil
}

func (s *HomeCarouselService) Create(in HomeCarouselCreateInput) (*HomeCarouselItemView, error) {
	productID, activityID, activityProductID, channel, err := s.resolveCreateLink(in)
	if err != nil {
		return nil, err
	}

	dup, err := s.countDup(productID, activityProductID, channel)
	if err != nil {
		return nil, err
	}
	if dup > 0 {
		return nil, ErrHomeCarouselDupProduct
	}

	status := in.Status
	if status != model.HomeCarouselStatusOff {
		status = model.HomeCarouselStatusOn
	}
	var enabled int64
	if err := query.NotDeleted(s.DB.Model(&model.HomeCarousel{})).
		Where("status = ?", model.HomeCarouselStatusOn).Count(&enabled).Error; err != nil {
		return nil, err
	}
	if status == model.HomeCarouselStatusOn && enabled >= model.HomeCarouselMaxItems {
		return nil, ErrHomeCarouselLimit
	}

	sortOrder := in.SortOrder
	if sortOrder == 0 {
		var maxSort int
		_ = query.NotDeleted(s.DB.Model(&model.HomeCarousel{})).
			Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSort)
		sortOrder = maxSort + 10
	}

	row := model.HomeCarousel{
		ProductID:         productID,
		ActivityID:        activityID,
		ActivityProductID: activityProductID,
		Channel:           channel,
		SortOrder:         sortOrder,
		Status:            status,
	}
	if err := s.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	views, err := s.enrich([]model.HomeCarousel{row}, false)
	if err != nil || len(views) == 0 {
		return nil, err
	}
	return &views[0], nil
}

type HomeCarouselPatch struct {
	SortOrder *int
	Status    *uint8
}

func (s *HomeCarouselService) Update(id uint64, patch HomeCarouselPatch) (*HomeCarouselItemView, error) {
	var row model.HomeCarousel
	if err := query.NotDeleted(s.DB).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHomeCarouselNotFound
		}
		return nil, err
	}
	updates := map[string]interface{}{}
	if patch.SortOrder != nil {
		updates["sort_order"] = *patch.SortOrder
		row.SortOrder = *patch.SortOrder
	}
	if patch.Status != nil {
		st := *patch.Status
		if st != model.HomeCarouselStatusOff {
			st = model.HomeCarouselStatusOn
		}
		if st == model.HomeCarouselStatusOn && row.Status != model.HomeCarouselStatusOn {
			var enabled int64
			if err := query.NotDeleted(s.DB.Model(&model.HomeCarousel{})).
				Where("status = ? AND id <> ?", model.HomeCarouselStatusOn, id).
				Count(&enabled).Error; err != nil {
				return nil, err
			}
			if enabled >= model.HomeCarouselMaxItems {
				return nil, ErrHomeCarouselLimit
			}
		}
		updates["status"] = st
		row.Status = st
	}
	if len(updates) == 0 {
		views, err := s.enrich([]model.HomeCarousel{row}, false)
		if err != nil || len(views) == 0 {
			return nil, err
		}
		return &views[0], nil
	}
	if err := s.DB.Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	views, err := s.enrich([]model.HomeCarousel{row}, false)
	if err != nil || len(views) == 0 {
		return nil, err
	}
	return &views[0], nil
}

func (s *HomeCarouselService) Delete(id uint64) error {
	var row model.HomeCarousel
	if err := query.NotDeleted(s.DB).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrHomeCarouselNotFound
		}
		return err
	}
	return query.SoftDelete(s.DB, &model.HomeCarousel{}, "id = ?", id).Error
}

func (s *HomeCarouselService) Reorder(ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			res := query.NotDeleted(tx.Model(&model.HomeCarousel{})).
				Where("id = ?", id).
				Update("sort_order", (i+1)*10)
			if res.Error != nil {
				return res.Error
			}
		}
		return nil
	})
}
