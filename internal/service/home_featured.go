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
	ErrHomeFeaturedNotFound   = errors.New("home featured item not found")
	ErrHomeFeaturedDup        = errors.New("item already in featured list")
	ErrHomeFeaturedBadSection = errors.New("invalid featured section")
	ErrHomeFeaturedBadProduct = errors.New("product not found or off shelf")
	ErrHomeFeaturedBadMerchant = errors.New("merchant not found or invalid")
	ErrHomeFeaturedBadLink    = errors.New("invalid featured link")
)

type HomeFeaturedService struct {
	DB *gorm.DB
}

type HomeFeaturedItemView struct {
	ID                uint64  `json:"id"`
	Section           string  `json:"section"`
	ItemType          string  `json:"item_type"`
	ProductID         uint64  `json:"product_id,omitempty"`
	MerchantID        uint64  `json:"merchant_id"`
	Name              string  `json:"name"`
	CoverURL          string  `json:"cover_url"`
	Price             float64 `json:"price,omitempty"`
	OriginPrice       float64 `json:"origin_price,omitempty"`
	Channel           string  `json:"channel,omitempty"`
	ChannelText       string  `json:"channel_text,omitempty"`
	ActivityID        *uint64 `json:"activity_id,omitempty"`
	ActivityProductID *uint64 `json:"activity_product_id,omitempty"`
	Address           string  `json:"address,omitempty"`
	ProductCount      int64   `json:"product_count,omitempty"`
	SortOrder         int     `json:"sort_order"`
	Status            uint8   `json:"status"`
}

type HomeFeaturedCreateInput struct {
	Section           string
	ItemType          string
	ProductID         uint64
	MerchantID        uint64
	ActivityID        *uint64
	ActivityProductID *uint64
	Channel           string
	SortOrder         int
	Status            uint8
}

type HomeFeaturedHiddenKey struct {
	ProductID  uint64 `json:"product_id,omitempty"`
	MerchantID uint64 `json:"merchant_id,omitempty"`
}

type HomeFeaturedPublicConfig struct {
	Pinned []HomeFeaturedItemView  `json:"pinned"`
	Hidden []HomeFeaturedHiddenKey `json:"hidden"`
}

type HomeFeaturedPatch struct {
	SortOrder *int
	Status    *uint8
}

func normalizeFeaturedSection(section string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(section))
	if !model.ValidHomeFeaturedSection(s) {
		return "", ErrHomeFeaturedBadSection
	}
	return s, nil
}

func normalizeFeaturedItemType(itemType string) string {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case model.HomeFeaturedTypeHidden:
		return model.HomeFeaturedTypeHidden
	default:
		return model.HomeFeaturedTypePinned
	}
}

func (s *HomeFeaturedService) ListPublicConfig(section string) (*HomeFeaturedPublicConfig, error) {
	section, err := normalizeFeaturedSection(section)
	if err != nil {
		return nil, err
	}
	var rows []model.HomeFeatured
	err = query.NotDeleted(s.DB.Model(&model.HomeFeatured{})).
		Where("section = ?", section).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	pinnedRows := make([]model.HomeFeatured, 0)
	hiddenRows := make([]model.HomeFeatured, 0)
	for _, r := range rows {
		if normalizeFeaturedItemType(r.ItemType) == model.HomeFeaturedTypeHidden {
			hiddenRows = append(hiddenRows, r)
			continue
		}
		if r.Status == model.HomeFeaturedStatusOn {
			pinnedRows = append(pinnedRows, r)
		}
	}
	pinned, err := s.enrich(section, pinnedRows, true)
	if err != nil {
		return nil, err
	}
	hidden := make([]HomeFeaturedHiddenKey, 0, len(hiddenRows))
	for _, r := range hiddenRows {
		if section == model.HomeFeaturedSectionFood {
			if r.MerchantID != nil && *r.MerchantID > 0 {
				hidden = append(hidden, HomeFeaturedHiddenKey{MerchantID: *r.MerchantID})
			}
			continue
		}
		if r.ProductID != nil && *r.ProductID > 0 {
			hidden = append(hidden, HomeFeaturedHiddenKey{ProductID: *r.ProductID})
		}
	}
	return &HomeFeaturedPublicConfig{Pinned: pinned, Hidden: hidden}, nil
}

func (s *HomeFeaturedService) ListPublic(section string) ([]HomeFeaturedItemView, error) {
	cfg, err := s.ListPublicConfig(section)
	if err != nil {
		return nil, err
	}
	return cfg.Pinned, nil
}

func (s *HomeFeaturedService) ListAdmin(section string) ([]HomeFeaturedItemView, error) {
	section, err := normalizeFeaturedSection(section)
	if err != nil {
		return nil, err
	}
	var rows []model.HomeFeatured
	err = query.NotDeleted(s.DB.Model(&model.HomeFeatured{})).
		Where("section = ?", section).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.enrich(section, rows, false)
}

func (s *HomeFeaturedService) enrich(section string, rows []model.HomeFeatured, publicOnlyOnShelf bool) ([]HomeFeaturedItemView, error) {
	if len(rows) == 0 {
		return []HomeFeaturedItemView{}, nil
	}
	if section == model.HomeFeaturedSectionFood {
		return s.enrichFood(rows, publicOnlyOnShelf)
	}
	return s.enrichProducts(section, rows, publicOnlyOnShelf)
}

func (s *HomeFeaturedService) enrichFood(rows []model.HomeFeatured, publicOnlyOnShelf bool) ([]HomeFeaturedItemView, error) {
	merchantIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if r.MerchantID != nil && *r.MerchantID > 0 {
			merchantIDs = append(merchantIDs, *r.MerchantID)
		}
	}
	var merchants []model.MerchantProfile
	q := query.NotDeleted(s.DB.Model(&model.MerchantProfile{})).Where("id IN ?", merchantIDs)
	if publicOnlyOnShelf {
		q = q.Where("status = ?", model.MerchantStatusOpen)
	}
	if err := q.Find(&merchants).Error; err != nil {
		return nil, err
	}
	byMerchant := map[uint64]model.MerchantProfile{}
	for _, m := range merchants {
		byMerchant[m.ID] = m
	}

	counts := map[uint64]int64{}
	if len(merchantIDs) > 0 {
		type countRow struct {
			MerchantID uint64
			Cnt        int64
		}
		var countRows []countRow
		if err := query.NotDeleted(s.DB.Model(&model.Product{})).
			Select("merchant_id, COUNT(*) as cnt").
			Where("merchant_id IN ? AND status = ?", merchantIDs, model.ProductStatusOn).
			Group("merchant_id").
			Scan(&countRows).Error; err != nil {
			return nil, err
		}
		for _, c := range countRows {
			counts[c.MerchantID] = c.Cnt
		}
	}

	out := make([]HomeFeaturedItemView, 0, len(rows))
	for _, r := range rows {
		if r.MerchantID == nil || *r.MerchantID == 0 {
			if publicOnlyOnShelf {
				continue
			}
			out = append(out, HomeFeaturedItemView{
				ID: r.ID, Section: r.Section, Name: "(商家已删除)",
				SortOrder: r.SortOrder, Status: r.Status,
			})
			continue
		}
		m, ok := byMerchant[*r.MerchantID]
		if !ok {
			if publicOnlyOnShelf {
				continue
			}
			out = append(out, HomeFeaturedItemView{
				ID: r.ID, Section: r.Section, MerchantID: *r.MerchantID, Name: "(商家已删除)",
				SortOrder: r.SortOrder, Status: r.Status,
			})
			continue
		}
		cover := ""
		if m.ShopLogo != nil {
			cover = *m.ShopLogo
		}
		if cover == "" && len(m.Images) > 0 {
			cover = m.Images[0]
		}
		addr := ""
		if m.Address != nil {
			addr = *m.Address
		}
		out = append(out, HomeFeaturedItemView{
			ID: r.ID, Section: r.Section, ItemType: normalizeFeaturedItemType(r.ItemType),
			MerchantID: m.ID,
			Name: m.ShopName, CoverURL: cover, Address: addr,
			ProductCount: counts[m.ID],
			SortOrder: r.SortOrder, Status: r.Status,
		})
	}
	return out, nil
}

func (s *HomeFeaturedService) enrichProducts(section string, rows []model.HomeFeatured, publicOnlyOnShelf bool) ([]HomeFeaturedItemView, error) {
	productIDs := make([]uint64, 0, len(rows))
	apIDs := make([]uint64, 0)
	actIDs := make([]uint64, 0)
	for _, r := range rows {
		if r.ProductID != nil && *r.ProductID > 0 {
			productIDs = append(productIDs, *r.ProductID)
		}
		if r.ActivityProductID != nil && *r.ActivityProductID > 0 {
			apIDs = append(apIDs, *r.ActivityProductID)
		}
		if r.ActivityID != nil && *r.ActivityID > 0 {
			actIDs = append(actIDs, *r.ActivityID)
		}
	}

	var products []model.Product
	pq := query.NotDeleted(s.DB.Model(&model.Product{})).Where("id IN ?", productIDs)
	if publicOnlyOnShelf {
		pq = pq.Where("status = ?", model.ProductStatusOn)
	}
	if err := pq.Find(&products).Error; err != nil {
		return nil, err
	}
	byProduct := map[uint64]model.Product{}
	for _, p := range products {
		byProduct[p.ID] = p
	}

	byAP := map[uint64]model.ActivityProduct{}
	if len(apIDs) > 0 {
		var aps []model.ActivityProduct
		if err := query.NotDeleted(s.DB.Model(&model.ActivityProduct{})).Where("id IN ?", apIDs).Find(&aps).Error; err != nil {
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

	now := timeNow()
	out := make([]HomeFeaturedItemView, 0, len(rows))
	for _, r := range rows {
		if r.ProductID == nil || *r.ProductID == 0 {
			if publicOnlyOnShelf {
				continue
			}
			out = append(out, HomeFeaturedItemView{
				ID: r.ID, Section: r.Section, Name: "(商品已删除)",
				SortOrder: r.SortOrder, Status: r.Status,
			})
			continue
		}

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

		p, ok := byProduct[*r.ProductID]
		if !ok {
			if publicOnlyOnShelf {
				continue
			}
			out = append(out, HomeFeaturedItemView{
				ID: r.ID, Section: r.Section, ProductID: *r.ProductID, Name: "(商品已删除)",
				Channel: channel, ChannelText: carouselChannelText(channel, hasActivity),
				ActivityID: r.ActivityID, ActivityProductID: r.ActivityProductID,
				SortOrder: r.SortOrder, Status: r.Status,
			})
			continue
		}

		if publicOnlyOnShelf && section == model.HomeFeaturedSectionPickup && p.AllowPickup != 1 {
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

		origin := 0.0
		if p.OriginalPrice != nil {
			origin = *p.OriginalPrice
		}

		out = append(out, HomeFeaturedItemView{
			ID: r.ID, Section: r.Section, ItemType: normalizeFeaturedItemType(r.ItemType),
			ProductID: p.ID, MerchantID: p.MerchantID,
			Name: p.Name, CoverURL: p.CoverURL, Price: price, OriginPrice: origin,
			Channel: channel, ChannelText: carouselChannelText(channel, hasActivity),
			ActivityID: r.ActivityID, ActivityProductID: r.ActivityProductID,
			SortOrder: r.SortOrder, Status: r.Status,
		})
	}
	return out, nil
}

func (s *HomeFeaturedService) countDup(section, itemType string, productID uint64, merchantID uint64, activityProductID *uint64, channel string) (int64, error) {
	itemType = normalizeFeaturedItemType(itemType)
	q := query.NotDeleted(s.DB.Model(&model.HomeFeatured{})).
		Where("section = ? AND item_type = ?", section, itemType)
	if section == model.HomeFeaturedSectionFood {
		q = q.Where("merchant_id = ?", merchantID)
	} else if activityProductID != nil && *activityProductID > 0 {
		// 活动商品渠道由 AP 类型决定，同一 AP 只能出现一次。
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

func (s *HomeFeaturedService) resolveCreate(in HomeFeaturedCreateInput) (section, itemType string, productID *uint64, merchantID *uint64, activityID, activityProductID *uint64, channel string, err error) {
	section, err = normalizeFeaturedSection(in.Section)
	if err != nil {
		return
	}
	itemType = normalizeFeaturedItemType(in.ItemType)

	if section == model.HomeFeaturedSectionFood {
		if in.MerchantID == 0 {
			return "", "", nil, nil, nil, nil, "", fmt.Errorf("%w: merchant_id", ErrHomeFeaturedBadMerchant)
		}
		var m model.MerchantProfile
		if err = query.NotDeleted(s.DB).First(&m, in.MerchantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", "", nil, nil, nil, nil, "", ErrHomeFeaturedBadMerchant
			}
			return
		}
		mid := m.ID
		merchantID = &mid
		return section, itemType, nil, merchantID, nil, nil, "", nil
	}

	if itemType == model.HomeFeaturedTypeHidden {
		if in.ProductID == 0 {
			return "", "", nil, nil, nil, nil, "", fmt.Errorf("%w: product_id", ErrHomeFeaturedBadProduct)
		}
		var product model.Product
		if err = query.NotDeleted(s.DB).First(&product, in.ProductID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", "", nil, nil, nil, nil, "", ErrHomeFeaturedBadProduct
			}
			return
		}
		pid := product.ID
		productID = &pid
		return section, itemType, productID, nil, nil, nil, model.HomeCarouselChannelDeal, nil
	}

	// pickup / deal / home_rail pinned — resolve product link (deal supports activity like carousel)
	if section == model.HomeFeaturedSectionDeal || section == model.HomeFeaturedSectionHomeRail {
		pid, aid, apid, ch, linkErr := s.resolveDealLink(in)
		if linkErr != nil {
			return "", "", nil, nil, nil, nil, "", linkErr
		}
		productID = &pid
		activityID, activityProductID = aid, apid
		channel = ch
		return section, itemType, productID, nil, activityID, activityProductID, channel, nil
	}

	// pickup — product only
	if in.ProductID == 0 {
		return "", "", nil, nil, nil, nil, "", fmt.Errorf("%w: product_id", ErrHomeFeaturedBadProduct)
	}
	var product model.Product
	if err = query.NotDeleted(s.DB).First(&product, in.ProductID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil, nil, nil, nil, "", ErrHomeFeaturedBadProduct
		}
		return
	}
	pid := product.ID
	productID = &pid
	return section, itemType, productID, nil, nil, nil, model.HomeCarouselChannelDeal, nil
}

func (s *HomeFeaturedService) resolveDealLink(in HomeFeaturedCreateInput) (productID uint64, activityID, activityProductID *uint64, channel string, err error) {
	carouselIn := HomeCarouselCreateInput{
		ProductID:         in.ProductID,
		ActivityID:        in.ActivityID,
		ActivityProductID: in.ActivityProductID,
		Channel:           in.Channel,
	}
	svc := &HomeCarouselService{DB: s.DB}
	return svc.resolveCreateLink(carouselIn)
}

func (s *HomeFeaturedService) Create(in HomeFeaturedCreateInput) (*HomeFeaturedItemView, error) {
	section, itemType, productID, merchantID, activityID, activityProductID, channel, err := s.resolveCreate(in)
	if err != nil {
		return nil, err
	}

	dupKeyProduct := uint64(0)
	dupKeyMerchant := uint64(0)
	if productID != nil {
		dupKeyProduct = *productID
	}
	if merchantID != nil {
		dupKeyMerchant = *merchantID
	}
	dup, err := s.countDup(section, itemType, dupKeyProduct, dupKeyMerchant, activityProductID, channel)
	if err != nil {
		return nil, err
	}
	if dup > 0 {
		return nil, ErrHomeFeaturedDup
	}

	status := in.Status
	if itemType == model.HomeFeaturedTypeHidden {
		status = model.HomeFeaturedStatusOn
	} else if status != model.HomeFeaturedStatusOff {
		status = model.HomeFeaturedStatusOn
	}

	sortOrder := in.SortOrder
	if itemType == model.HomeFeaturedTypePinned && sortOrder == 0 {
		var maxSort *int
		_ = query.NotDeleted(s.DB.Model(&model.HomeFeatured{})).
			Where("section = ? AND item_type = ?", section, model.HomeFeaturedTypePinned).
			Select("MAX(sort_order)").
			Scan(&maxSort).Error
		if maxSort != nil {
			sortOrder = *maxSort + 10
		} else {
			sortOrder = 10
		}
	}

	row := model.HomeFeatured{
		Section: section, ItemType: itemType, ProductID: productID, MerchantID: merchantID,
		ActivityID: activityID, ActivityProductID: activityProductID,
		Channel: channel, SortOrder: sortOrder, Status: status,
	}
	if err := s.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	views, err := s.enrich(section, []model.HomeFeatured{row}, false)
	if err != nil || len(views) == 0 {
		return &HomeFeaturedItemView{ID: row.ID, Section: section, SortOrder: sortOrder, Status: status}, err
	}
	return &views[0], nil
}

func (s *HomeFeaturedService) Update(id uint64, patch HomeFeaturedPatch) (*HomeFeaturedItemView, error) {
	var row model.HomeFeatured
	if err := query.NotDeleted(s.DB).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHomeFeaturedNotFound
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
		if st != model.HomeFeaturedStatusOff {
			st = model.HomeFeaturedStatusOn
		}
		updates["status"] = st
		row.Status = st
	}
	if len(updates) > 0 {
		if err := s.DB.Model(&row).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	views, err := s.enrich(row.Section, []model.HomeFeatured{row}, false)
	if err != nil || len(views) == 0 {
		return &HomeFeaturedItemView{ID: row.ID, Section: row.Section, SortOrder: row.SortOrder, Status: row.Status}, err
	}
	return &views[0], nil
}

func (s *HomeFeaturedService) Delete(id uint64) error {
	var row model.HomeFeatured
	if err := query.NotDeleted(s.DB).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrHomeFeaturedNotFound
		}
		return err
	}
	return query.SoftDelete(s.DB, &model.HomeFeatured{}, "id = ?", id).Error
}

func (s *HomeFeaturedService) Reorder(section string, ids []uint64) error {
	section, err := normalizeFeaturedSection(section)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			res := query.NotDeleted(tx.Model(&model.HomeFeatured{})).
				Where("id = ? AND section = ? AND item_type = ?", id, section, model.HomeFeaturedTypePinned).
				Update("sort_order", (i+1)*10)
			if res.Error != nil {
				return res.Error
			}
		}
		return nil
	})
}

// timeNow is a test seam; production uses time.Now via home_carousel.go in same package.
func timeNow() time.Time {
	return time.Now()
}
