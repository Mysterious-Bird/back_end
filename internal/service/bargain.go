package service

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrBargainNotFound      = errors.New("bargain session not found")
	ErrBargainNotEnabled    = errors.New("bargain not enabled")
	ErrBargainAlreadyHelped = errors.New("already helped this bargain")
	ErrBargainExpired       = errors.New("bargain expired")
	ErrBargainForbidden     = errors.New("bargain forbidden")
	ErrBargainDailyLimit    = errors.New("bargain daily help limit")
	ErrBargainNoRemain      = errors.New("bargain already at floor")
	ErrBargainConflict      = errors.New("bargain session conflict")
)

type BargainService struct {
	DB *gorm.DB
}

type BargainHelpView struct {
	HelperAccountID uint64    `json:"helper_account_id"`
	CutAmount       float64   `json:"cut_amount"`
	IsNewUser       bool      `json:"is_new_user"`
	CreatedAt       time.Time `json:"created_at"`
}

type BargainSessionView struct {
	model.BargainSession
	ProductName   string            `json:"product_name"`
	ProductCover  string            `json:"product_cover"`
	CanBuy        bool              `json:"can_buy"`
	CanHelp       bool              `json:"can_help"`
	IsInitiator   bool              `json:"is_initiator"`
	AlreadyHelped bool              `json:"already_helped"`
	GuideText     string            `json:"guide_text"`
	Helps         []BargainHelpView `json:"helps"`
}

func roundBargainMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

// rollCutAmount 在 [min,max] 与 remain 内随机砍幅；接近底价时压缩上限。
func rollCutAmount(remain, min, max float64, r *rand.Rand) float64 {
	remain = roundBargainMoney(remain)
	if remain <= 0 {
		return 0
	}
	if max < min {
		min, max = max, min
	}
	cap := remain
	if remain > 0.5 {
		soft := roundBargainMoney(remain * 0.3)
		if soft > 0 && soft < cap {
			cap = soft
		}
	}
	hi := max
	if hi > cap {
		hi = cap
	}
	lo := min
	if lo > hi {
		lo = hi
	}
	if hi <= 0 {
		return roundBargainMoney(math.Min(0.01, remain))
	}
	if lo >= hi {
		return roundBargainMoney(hi)
	}
	span := hi - lo
	v := lo + r.Float64()*span
	v = roundBargainMoney(v)
	if v < 0.01 && remain >= 0.01 {
		v = 0.01
	}
	if v > remain {
		v = remain
	}
	return v
}

func validateBargainOnActivityProduct(input ActivityProductInput) error {
	if input.EnableBargain != 1 {
		return nil
	}
	if input.EnableGroupBuy == 1 {
		return fmt.Errorf("%w: 砍价与拼团不能同时开启", ErrInvalidProductArg)
	}
	if input.BargainFloorPrice == nil || *input.BargainFloorPrice <= 0 {
		return fmt.Errorf("%w: bargain_floor_price", ErrInvalidProductArg)
	}
	if *input.BargainFloorPrice >= input.ActivityPrice {
		return fmt.Errorf("%w: 底价须低于活动价", ErrInvalidProductArg)
	}
	if input.BargainDurationHours == 0 {
		return fmt.Errorf("%w: bargain_duration_hours", ErrInvalidProductArg)
	}
	if input.BargainNewMin < 0 || input.BargainNewMax < input.BargainNewMin {
		return fmt.Errorf("%w: bargain_new 区间无效", ErrInvalidProductArg)
	}
	if input.BargainOldMin < 0 || input.BargainOldMax < input.BargainOldMin {
		return fmt.Errorf("%w: bargain_old 区间无效", ErrInvalidProductArg)
	}
	if input.BargainSelfCutMax <= 0 {
		return fmt.Errorf("%w: bargain_self_cut_max", ErrInvalidProductArg)
	}
	return nil
}

func normalizeBargainInput(input ActivityProductInput) ActivityProductInput {
	if input.EnableBargain != 1 {
		input.EnableBargain = 0
		input.BargainFloorPrice = nil
		return input
	}
	if input.EnableGroupBuy == 1 {
		input.EnableBargain = 0
		input.BargainFloorPrice = nil
		return input
	}
	if input.BargainDurationHours == 0 {
		input.BargainDurationHours = 24
	}
	if input.BargainNewUserHours == 0 {
		input.BargainNewUserHours = 48
	}
	if input.BargainHelpDailyMax == 0 {
		input.BargainHelpDailyMax = 20
	}
	if input.BargainSelfCutMax <= 0 {
		input.BargainSelfCutMax = 1
	}
	if input.BargainNewMax <= 0 {
		input.BargainNewMin, input.BargainNewMax = 1, 5
	}
	if input.BargainOldMax <= 0 {
		input.BargainOldMin, input.BargainOldMax = 0.1, 1
	}
	return input
}

func assertSessionBuyable(sess *model.BargainSession, buyerID uint64, now time.Time) error {
	if sess == nil {
		return ErrBargainNotFound
	}
	if sess.InitiatorAccountID != buyerID {
		return ErrBargainForbidden
	}
	if sess.Status == model.BargainStatusExpired || now.After(sess.ExpireAt) {
		return ErrBargainExpired
	}
	if sess.Status == model.BargainStatusOrdered {
		return fmt.Errorf("%w: 已下单", ErrBargainConflict)
	}
	if sess.Status != model.BargainStatusOngoing {
		return ErrBargainConflict
	}
	return nil
}

func (s *BargainService) expireIfNeeded(tx *gorm.DB, sess *model.BargainSession, now time.Time) error {
	if sess.Status != model.BargainStatusOngoing {
		return nil
	}
	if !now.After(sess.ExpireAt) {
		return nil
	}
	sess.Status = model.BargainStatusExpired
	return tx.Model(sess).Update("status", model.BargainStatusExpired).Error
}

func (s *BargainService) StartSession(accountID, activityProductID uint64) (*BargainSessionView, error) {
	now := time.Now()
	var out *BargainSessionView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		ap, act, product, err := s.loadBargainAP(tx, activityProductID)
		if err != nil {
			return err
		}
		var existing model.BargainSession
		err = query.NotDeleted(tx).
			Where("activity_product_id = ? AND initiator_account_id = ? AND status = ?",
				activityProductID, accountID, model.BargainStatusOngoing).
			First(&existing).Error
		if err == nil {
			_ = s.expireIfNeeded(tx, &existing, now)
			if existing.Status == model.BargainStatusOngoing {
				view, vErr := s.buildView(tx, &existing, product, &accountID)
				if vErr != nil {
					return vErr
				}
				out = view
				return nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		hours := ap.BargainDurationHours
		if hours == 0 {
			hours = 24
		}
		floor := *ap.BargainFloorPrice
		sess := model.BargainSession{
			ActivityID:         act.ID,
			ActivityProductID:  ap.ID,
			ProductID:          ap.ProductID,
			MerchantID:         product.MerchantID,
			InitiatorAccountID: accountID,
			OriginPrice:        roundBargainMoney(ap.ActivityPrice),
			FloorPrice:         roundBargainMoney(floor),
			CurrentPrice:       roundBargainMoney(ap.ActivityPrice),
			Status:             model.BargainStatusOngoing,
			ExpireAt:           now.Add(time.Duration(hours) * time.Hour),
		}
		if err := tx.Create(&sess).Error; err != nil {
			return err
		}
		cut := rollCutAmount(
			sess.CurrentPrice-sess.FloorPrice,
			0.01,
			math.Min(ap.BargainSelfCutMax, ap.BargainOldMax),
			rand.New(rand.NewSource(now.UnixNano()^int64(accountID))),
		)
		if cut > ap.BargainSelfCutMax {
			cut = ap.BargainSelfCutMax
		}
		cut = roundBargainMoney(cut)
		if cut > 0 {
			help := model.BargainHelp{
				SessionID:       sess.ID,
				HelperAccountID: accountID,
				CutAmount:       cut,
				IsNewUser:       0,
			}
			if err := tx.Create(&help).Error; err != nil {
				return err
			}
			sess.CurrentPrice = roundBargainMoney(sess.CurrentPrice - cut)
			sess.SelfCutDone = 1
			if err := tx.Model(&sess).Updates(map[string]interface{}{
				"current_price": sess.CurrentPrice,
				"self_cut_done": 1,
			}).Error; err != nil {
				return err
			}
		}
		view, vErr := s.buildView(tx, &sess, product, &accountID)
		if vErr != nil {
			return vErr
		}
		out = view
		return nil
	})
	return out, err
}

func (s *BargainService) Help(sessionID, helperAccountID uint64) (*BargainSessionView, error) {
	now := time.Now()
	var out *BargainSessionView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var sess model.BargainSession
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&sess, sessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBargainNotFound
			}
			return err
		}
		if err := s.expireIfNeeded(tx, &sess, now); err != nil {
			return err
		}
		if sess.Status != model.BargainStatusOngoing || now.After(sess.ExpireAt) {
			return ErrBargainExpired
		}
		remain := roundBargainMoney(sess.CurrentPrice - sess.FloorPrice)
		if remain <= 0 {
			return ErrBargainNoRemain
		}

		ap, _, product, err := s.loadBargainAP(tx, sess.ActivityProductID)
		if err != nil {
			return err
		}

		var helper model.Account
		if err := query.NotDeleted(tx).First(&helper, helperAccountID).Error; err != nil {
			return err
		}
		newHours := ap.BargainNewUserHours
		if newHours == 0 {
			newHours = 48
		}
		isNew := helper.CreatedAt.After(now.Add(-time.Duration(newHours) * time.Hour))
		isSelf := helperAccountID == sess.InitiatorAccountID
		if isSelf {
			if sess.SelfCutDone == 1 {
				return ErrBargainAlreadyHelped
			}
		} else {
			settings, sErr := s.getSettingsTx(tx)
			if sErr != nil {
				return sErr
			}
			windowStart, _ := calendarWindowAt(now, "day", settings.HelpDailyRefreshTime)
			var dayCount int64
			if err := tx.Model(&model.BargainHelp{}).
				Where("helper_account_id = ? AND created_at >= ?", helperAccountID, windowStart).
				Count(&dayCount).Error; err != nil {
				return err
			}
			maxDaily := settings.HelpDailyMax
			if maxDaily == 0 {
				maxDaily = 20
			}
			if uint32(dayCount) >= maxDaily {
				return ErrBargainDailyLimit
			}
		}

		min, max := ap.BargainOldMin, ap.BargainOldMax
		if isNew && !isSelf {
			min, max = ap.BargainNewMin, ap.BargainNewMax
		}
		if isSelf {
			max = math.Min(max, ap.BargainSelfCutMax)
		}
		cut := rollCutAmount(remain, min, max, rand.New(rand.NewSource(now.UnixNano()^int64(helperAccountID)^int64(sessionID))))
		if isSelf && cut > ap.BargainSelfCutMax {
			cut = ap.BargainSelfCutMax
		}
		cut = roundBargainMoney(cut)
		if cut <= 0 {
			return ErrBargainNoRemain
		}

		help := model.BargainHelp{
			SessionID:       sess.ID,
			HelperAccountID: helperAccountID,
			CutAmount:       cut,
			IsNewUser:       boolToUint8(isNew && !isSelf),
		}
		if err := tx.Create(&help).Error; err != nil {
			if isMySQLDuplicateKey(err) || errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrBargainAlreadyHelped
			}
			// sqlite unique
			if isUniqueViolation(err) {
				return ErrBargainAlreadyHelped
			}
			return err
		}
		sess.CurrentPrice = roundBargainMoney(sess.CurrentPrice - cut)
		updates := map[string]interface{}{"current_price": sess.CurrentPrice}
		if isSelf {
			sess.SelfCutDone = 1
			updates["self_cut_done"] = 1
		}
		if err := tx.Model(&sess).Updates(updates).Error; err != nil {
			return err
		}
		view, vErr := s.buildView(tx, &sess, product, &helperAccountID)
		if vErr != nil {
			return vErr
		}
		out = view
		return nil
	})
	return out, err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate")
}

func (s *BargainService) GetSession(sessionID uint64, viewerAccountID *uint64) (*BargainSessionView, error) {
	now := time.Now()
	var sess model.BargainSession
	if err := query.NotDeleted(s.DB).First(&sess, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBargainNotFound
		}
		return nil, err
	}
	_ = s.DB.Transaction(func(tx *gorm.DB) error {
		return s.expireIfNeeded(tx, &sess, now)
	})
	if err := query.NotDeleted(s.DB).First(&sess, sessionID).Error; err != nil {
		return nil, err
	}
	_, _, product, err := s.loadBargainAP(s.DB, sess.ActivityProductID)
	if err != nil {
		return nil, err
	}
	return s.buildView(s.DB, &sess, product, viewerAccountID)
}

func (s *BargainService) loadBargainAP(tx *gorm.DB, activityProductID uint64) (*model.ActivityProduct, *model.Activity, *model.Product, error) {
	var ap model.ActivityProduct
	if err := query.NotDeleted(tx).Preload("Product").First(&ap, activityProductID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrActivityProductNotFound
		}
		return nil, nil, nil, err
	}
	if ap.EnableBargain != 1 || ap.BargainFloorPrice == nil || ap.Status != 1 {
		return nil, nil, nil, ErrBargainNotEnabled
	}
	var act model.Activity
	if err := query.NotDeleted(tx).First(&act, ap.ActivityID).Error; err != nil {
		return nil, nil, nil, ErrActivityNotFound
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, nil, nil, ErrActivityNotActive
	}
	if ap.Product == nil || ap.Product.ID == 0 {
		var p model.Product
		if err := query.NotDeleted(tx).First(&p, ap.ProductID).Error; err != nil {
			return nil, nil, nil, ErrProductNotFound
		}
		ap.Product = &p
	}
	return &ap, &act, ap.Product, nil
}

func (s *BargainService) buildView(tx *gorm.DB, sess *model.BargainSession, product *model.Product, viewer *uint64) (*BargainSessionView, error) {
	var helps []model.BargainHelp
	if err := tx.Where("session_id = ?", sess.ID).Order("id ASC").Find(&helps).Error; err != nil {
		return nil, err
	}
	hv := make([]BargainHelpView, 0, len(helps))
	already := false
	for _, h := range helps {
		hv = append(hv, BargainHelpView{
			HelperAccountID: h.HelperAccountID,
			CutAmount:       h.CutAmount,
			IsNewUser:       h.IsNewUser == 1,
			CreatedAt:       h.CreatedAt,
		})
		if viewer != nil && h.HelperAccountID == *viewer {
			already = true
		}
	}
	isInit := viewer != nil && *viewer == sess.InitiatorAccountID
	canBuy := false
	canHelp := false
	now := time.Now()
	if sess.Status == model.BargainStatusOngoing && !now.After(sess.ExpireAt) {
		if isInit {
			canBuy = true
		}
		if viewer != nil && !already {
			if *viewer != sess.InitiatorAccountID || sess.SelfCutDone == 0 {
				if roundBargainMoney(sess.CurrentPrice-sess.FloorPrice) > 0 {
					canHelp = true
				}
			}
		}
	}
	cover := ""
	name := ""
	if product != nil {
		cover = product.CoverURL
		name = product.Name
	}
	guide := fmt.Sprintf("已砍至 ¥%.2f · 底价 ¥%.2f · 再邀好友有机会更低", sess.CurrentPrice, sess.FloorPrice)
	if roundBargainMoney(sess.CurrentPrice-sess.FloorPrice) <= 0 {
		guide = fmt.Sprintf("已到底价 ¥%.2f，可立即购买", sess.FloorPrice)
	}
	return &BargainSessionView{
		BargainSession: *sess,
		ProductName:    name,
		ProductCover:   cover,
		CanBuy:         canBuy,
		CanHelp:        canHelp,
		IsInitiator:    isInit,
		AlreadyHelped:  already,
		GuideText:      guide,
		Helps:          hv,
	}, nil
}

func markBargainSessionOrdered(tx *gorm.DB, sessionID, orderID uint64) error {
	res := query.NotDeleted(tx).Model(&model.BargainSession{}).
		Where("id = ? AND status = ?", sessionID, model.BargainStatusOngoing).
		Updates(map[string]interface{}{
			"status":   model.BargainStatusOrdered,
			"order_id": orderID,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrBargainConflict
	}
	return nil
}

func restoreBargainSessionIfUnpaid(tx *gorm.DB, orderID uint64) error {
	var sess model.BargainSession
	err := query.NotDeleted(tx).Where("order_id = ? AND status = ?", orderID, model.BargainStatusOrdered).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now()
	status := model.BargainStatusOngoing
	if now.After(sess.ExpireAt) {
		status = model.BargainStatusExpired
	}
	return tx.Model(&sess).Updates(map[string]interface{}{
		"status":   status,
		"order_id": nil,
	}).Error
}

func defaultBargainSettings() model.BargainSettings {
	return model.BargainSettings{
		ID:                   1,
		HelpDailyMax:         20,
		HelpDailyRefreshTime: "00:00:00",
	}
}

func (s *BargainService) getSettingsTx(tx *gorm.DB) (*model.BargainSettings, error) {
	var row model.BargainSettings
	err := tx.First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = defaultBargainSettings()
		if cErr := tx.Create(&row).Error; cErr != nil {
			return nil, cErr
		}
		return &row, nil
	}
	if err != nil {
		return nil, err
	}
	if row.HelpDailyMax == 0 {
		row.HelpDailyMax = 20
	}
	if strings.TrimSpace(row.HelpDailyRefreshTime) == "" {
		row.HelpDailyRefreshTime = "00:00:00"
	}
	return &row, nil
}

func (s *BargainService) GetSettings() (*model.BargainSettings, error) {
	return s.getSettingsTx(s.DB)
}

type UpdateBargainSettingsInput struct {
	HelpDailyMax         *uint32
	HelpDailyRefreshTime *string
}

func (s *BargainService) UpdateSettings(input UpdateBargainSettingsInput) (*model.BargainSettings, error) {
	row, err := s.getSettingsTx(s.DB)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if input.HelpDailyMax != nil {
		if *input.HelpDailyMax == 0 {
			return nil, fmt.Errorf("%w: help_daily_max", ErrInvalidProductArg)
		}
		updates["help_daily_max"] = *input.HelpDailyMax
	}
	if input.HelpDailyRefreshTime != nil {
		norm, nErr := NormalizeDailyRefreshTime(*input.HelpDailyRefreshTime)
		if nErr != nil {
			return nil, fmt.Errorf("%w: help_daily_refresh_time 格式无效", ErrInvalidProductArg)
		}
		updates["help_daily_refresh_time"] = norm
	}
	if len(updates) == 0 {
		return row, nil
	}
	if err := s.DB.Model(&model.BargainSettings{}).Where("id = ?", 1).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetSettings()
}
