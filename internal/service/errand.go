package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrErrandNotFound        = errors.New("errand order not found")
	ErrErrandStatusInvalid   = errors.New("errand status invalid")
	ErrErrandGoodsInvalid    = errors.New("errand goods amount invalid")
	ErrErrandUnauthorized    = errors.New("errand unauthorized")
)

// ErrandService 跑腿业务：非平台商品代取代送，下单不收款，送达时收款。
type ErrandService struct {
	DB      *gorm.DB
	Payment payment.Provider
}

type ErrandCreateInput struct {
	PickupShopName  string   `json:"pickup_shop_name"`
	PickupAddress   string   `json:"pickup_address"`
	PickupLatitude  *float64 `json:"pickup_latitude"`
	PickupLongitude *float64 `json:"pickup_longitude"`
	ContactName     string   `json:"contact_name"`
	ContactPhone    string   `json:"contact_phone"`
	Province        string   `json:"province"`
	City            string   `json:"city"`
	District        string   `json:"district"`
	Detail          string   `json:"detail"`
	Latitude        *float64 `json:"latitude"`
	Longitude       *float64 `json:"longitude"`
	LocationName    *string  `json:"location_name"`
	GoodsDesc       string   `json:"goods_desc"`
	GoodsPhotos     []string `json:"goods_photos"`
	VoucherCode     string   `json:"voucher_code"`
	VoucherPhotos   []string `json:"voucher_photos"`
	ItemCount       int      `json:"item_count"`
	DistanceKm      float64  `json:"distance_km"`
	ExpectMinutes   int      `json:"expect_minutes"`
	Remark          string   `json:"remark"`
}

// ErrandView 用户/骑手/管理端展示。
type ErrandView struct {
	model.ErrandOrder
	StatusText   string  `json:"status_text"`
	RiderName    string  `json:"rider_name,omitempty"`
	RiderPhone   string  `json:"rider_phone,omitempty"`
	RiderID      *uint64 `json:"rider_id,omitempty"`
	DeliveryID   *uint64 `json:"delivery_id,omitempty"`
	DeliveryStatus uint8 `json:"delivery_status,omitempty"`
	DeliveryStatusText string `json:"delivery_status_text,omitempty"`
	// 用户端待支付提示
	NeedPay bool `json:"need_pay"`
}

// errandFeeByCount 按件计费：2 件以内按基础价，第 3 件起每件加 1 元。
func errandFeeByCount(base float64, itemCount int) float64 {
	if itemCount < 1 {
		itemCount = 1
	}
	extra := 0
	if itemCount > 2 {
		extra = itemCount - 2
	}
	return roundMoney(base + float64(extra))
}

func genErrandOrderNo() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return fmt.Sprintf("PT%s%s", time.Now().Format("20060102150405"), strings.ToUpper(hex.EncodeToString(b)))
}

func (s *ErrandService) paymentProvider() payment.Provider {
	if s.Payment != nil {
		return s.Payment
	}
	return &payment.MockProvider{DB: s.DB}
}

// LoadErrandSettings 读取跑腿配置（缺省时使用默认值）。
func LoadErrandSettings(db *gorm.DB) *model.ErrandSettings {
	settings := &model.ErrandSettings{
		ID: 1, DeliveryFee: 1, PlatformCommissionPercent: 0,
		AutoConfirmHours: 24, GoodsMaxAmount: 5000,
	}
	var row model.ErrandSettings
	if err := db.First(&row, 1).Error; err == nil {
		if row.AutoConfirmHours > 0 {
			settings = &row
		}
	}
	return settings
}

// UpdateErrandSettings 管理端更新跑腿配置。
func (s *ErrandService) UpdateErrandSettings(in model.ErrandSettings) (*model.ErrandSettings, error) {
	if in.DeliveryFee < 0 || in.PlatformCommissionPercent < 0 || in.PlatformCommissionPercent > 100 {
		return nil, fmt.Errorf("%w: 配置数值无效", ErrErrandGoodsInvalid)
	}
	if in.AutoConfirmHours <= 0 {
		in.AutoConfirmHours = 24
	}
	if in.GoodsMaxAmount <= 0 {
		in.GoodsMaxAmount = 5000
	}
	updates := map[string]interface{}{
		"delivery_fee":                roundMoney(in.DeliveryFee),
		"platform_commission_percent": in.PlatformCommissionPercent,
		"auto_confirm_hours":          in.AutoConfirmHours,
		"goods_max_amount":            roundMoney(in.GoodsMaxAmount),
	}
	var row model.ErrandSettings
	if err := s.DB.First(&row, 1).Error; err != nil {
		row = model.ErrandSettings{ID: 1}
		if err := s.DB.Create(&row).Error; err != nil {
			return nil, err
		}
	}
	if err := s.DB.Model(&model.ErrandSettings{}).Where("id = 1").Updates(updates).Error; err != nil {
		return nil, err
	}
	return LoadErrandSettings(s.DB), nil
}

func trimErrandInput(in *ErrandCreateInput) {
	in.PickupShopName = strings.TrimSpace(in.PickupShopName)
	in.PickupAddress = strings.TrimSpace(in.PickupAddress)
	in.ContactName = strings.TrimSpace(in.ContactName)
	in.ContactPhone = strings.TrimSpace(in.ContactPhone)
	in.Province = strings.TrimSpace(in.Province)
	in.City = strings.TrimSpace(in.City)
	in.District = strings.TrimSpace(in.District)
	in.Detail = strings.TrimSpace(in.Detail)
	in.GoodsDesc = strings.TrimSpace(in.GoodsDesc)
	in.VoucherCode = strings.TrimSpace(in.VoucherCode)
	in.Remark = strings.TrimSpace(in.Remark)
}

// Create 下单（不收款），生成跑腿单与配送子单。
func (s *ErrandService) Create(accountID uint64, in ErrandCreateInput) (*ErrandView, error) {
	trimErrandInput(&in)
	if accountID == 0 {
		return nil, ErrErrandUnauthorized
	}
	if in.ContactName == "" || in.ContactPhone == "" {
		return nil, fmt.Errorf("%w: 请填写联系人和手机号", ErrErrandGoodsInvalid)
	}
	if in.PickupAddress == "" {
		return nil, fmt.Errorf("%w: 请填写取货地址", ErrErrandGoodsInvalid)
	}
	if strings.TrimSpace(in.Detail) == "" {
		return nil, fmt.Errorf("%w: 请填写收货地址", ErrErrandGoodsInvalid)
	}
	if in.GoodsDesc == "" {
		return nil, fmt.Errorf("%w: 请填写商品信息", ErrErrandGoodsInvalid)
	}
	if len([]rune(in.GoodsDesc)) > 200 {
		return nil, fmt.Errorf("%w: 商品信息最多 200 字", ErrErrandGoodsInvalid)
	}
	if len(in.GoodsPhotos) > 6 || len(in.VoucherPhotos) > 6 {
		return nil, fmt.Errorf("%w: 照片最多 6 张", ErrErrandGoodsInvalid)
	}
	if len([]rune(in.VoucherCode)) > 128 {
		return nil, fmt.Errorf("%w: 券码过长", ErrErrandGoodsInvalid)
	}
	if in.ItemCount < 1 {
		in.ItemCount = 1
	}
	if in.ItemCount > 20 {
		return nil, fmt.Errorf("%w: 总件数最多 20 件", ErrErrandGoodsInvalid)
	}
	settings := LoadErrandSettings(s.DB)

	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		eo := model.ErrandOrder{
			OrderNo:     genErrandOrderNo(),
			AccountID:   accountID,
			Status:      model.ErrandStatusPendingAccept,
			PayStatus:   model.PayStatusUnpaid,
			DeliveryFee: errandFeeByCount(settings.DeliveryFee, in.ItemCount),
			ItemCount:   in.ItemCount,
			GoodsDesc:   in.GoodsDesc,
			GoodsPhotos: in.GoodsPhotos,
			VoucherCode: in.VoucherCode,
			VoucherPhotos: in.VoucherPhotos,
			PickupSnapshot: &model.ErrandPickupSnapshot{
				ShopName: in.PickupShopName, Address: in.PickupAddress,
				Latitude: in.PickupLatitude, Longitude: in.PickupLongitude,
			},
			DropSnapshot: &model.AddressSnapshot{
				ContactName: in.ContactName, ContactPhone: in.ContactPhone,
				Province: in.Province, City: in.City, District: in.District,
				Detail: in.Detail, Latitude: in.Latitude, Longitude: in.Longitude,
				LocationName: in.LocationName,
			},
			DistanceKm:    roundMoney(in.DistanceKm),
			ExpectMinutes: in.ExpectMinutes,
		}
		if in.Remark != "" {
			eo.Remark = &in.Remark
		}
		if err := tx.Create(&eo).Error; err != nil {
			return err
		}
		delivery := model.DeliveryOrder{
			ErrandOrderID: &eo.ID,
			Status:        model.DeliveryPendingAccept,
		}
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}
		if err := tx.Model(&eo).Update("delivery_order_id", delivery.ID).Error; err != nil {
			return err
		}
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventCreated,
			ActorRole:   model.FulfillmentActorUser,
			Title:       "跑腿订单已创建",
			Detail:      map[string]interface{}{"goods": eo.GoodsDesc},
		})
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ErrandService) buildView(db *gorm.DB, eo model.ErrandOrder) *ErrandView {
	view := &ErrandView{ErrandOrder: eo, StatusText: model.ErrandStatusText(eo.Status)}
	view.NeedPay = eo.PayStatus == model.PayStatusUnpaid
	if eo.PayMode != 0 && eo.PayStatus != model.PayStatusUnpaid {
		view.NeedPay = false
	}
	if eo.DeliveryOrderID != nil {
		view.DeliveryID = eo.DeliveryOrderID
		var d model.DeliveryOrder
		if err := query.NotDeleted(db).First(&d, *eo.DeliveryOrderID).Error; err == nil {
			view.DeliveryStatus = d.Status
			view.DeliveryStatusText = model.DeliveryStatusText(d.Status)
			if d.RiderID != nil {
				view.RiderID = d.RiderID
				var acc model.Account
				if err := query.NotDeleted(db).Select("id", "nickname", "phone").First(&acc, *d.RiderID).Error; err == nil {
					if acc.Nickname != nil {
						view.RiderName = *acc.Nickname
					}
					if acc.Phone != nil {
						view.RiderPhone = *acc.Phone
					}
				}
				var app model.RiderApplication
				if err := query.NotDeleted(db).
					Where("account_id = ? AND status = ?", *d.RiderID, model.RiderApplicationApproved).
					Order("id DESC").First(&app).Error; err == nil && app.RealName != "" {
					view.RiderName = app.RealName
				}
			}
		}
	}
	return view
}

// List 用户跑腿单列表；statuses 为空表示全部。
func (s *ErrandService) List(accountID uint64, page, pageSize int, statuses []uint8) ([]ErrandView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	q := query.NotDeleted(s.DB.Model(&model.ErrandOrder{})).Where("account_id = ?", accountID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.ErrandOrder
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]ErrandView, 0, len(list))
	for i := range list {
		out = append(out, *s.buildView(s.DB, list[i]))
	}
	return out, total, nil
}

// Get 用户详情。
func (s *ErrandService) Get(accountID, id uint64) (*ErrandView, error) {
	var eo model.ErrandOrder
	q := query.NotDeleted(s.DB).Where("id = ?", id)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&eo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrErrandNotFound
		}
		return nil, err
	}
	return s.buildView(s.DB, eo), nil
}

// Cancel 用户取消：接单前/接单后未取货可免费取消（未收款不涉及退款）。
func (s *ErrandService) Cancel(accountID, id uint64, reason string) (*ErrandView, error) {
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND account_id = ?", id, accountID).First(&eo).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrErrandNotFound
			}
			return err
		}
		if eo.Status != model.ErrandStatusPendingAccept && eo.Status != model.ErrandStatusAccepted {
			return fmt.Errorf("%w: 当前状态不可取消，请联系客服", ErrErrandStatusInvalid)
		}
		if eo.PayStatus != model.PayStatusUnpaid {
			return fmt.Errorf("%w: 已付款订单请联系客服", ErrErrandStatusInvalid)
		}
		reasonText := strings.TrimSpace(reason)
		if reasonText == "" {
			reasonText = "用户取消"
		}
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":        model.ErrandStatusCancelled,
			"cancel_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		if eo.DeliveryOrderID != nil {
			_ = tx.Model(&model.DeliveryOrder{}).
				Where("id = ? AND status IN ?", *eo.DeliveryOrderID,
					[]int{int(model.DeliveryPendingAccept), int(model.DeliveryAccepted)}).
				Updates(map[string]interface{}{
					"status":           model.DeliveryCancelled,
					"exception_reason": reasonText,
				}).Error
		}
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventCancelled,
			ActorRole:   model.FulfillmentActorUser,
			Title:       "订单已取消",
			Detail:      map[string]interface{}{"reason": reasonText},
		})
		eo.Status = model.ErrandStatusCancelled
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RiderPickup 骑手取货：无券码时代购，需录入实报商品费并上传小票。
func (s *ErrandService) RiderPickup(riderID, id uint64, goodsAmount float64, receiptPhotos []string) (*ErrandView, error) {
	settings := LoadErrandSettings(s.DB)
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&eo, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrErrandNotFound
			}
			return err
		}
		if eo.Status != model.ErrandStatusAccepted {
			return fmt.Errorf("%w: 当前状态不可取货", ErrErrandStatusInvalid)
		}
		var d model.DeliveryOrder
		if eo.DeliveryOrderID == nil {
			return ErrErrandNotFound
		}
		if err := query.NotDeleted(tx).First(&d, *eo.DeliveryOrderID).Error; err != nil {
			return ErrErrandNotFound
		}
		if d.RiderID == nil || *d.RiderID != riderID {
			return ErrErrandUnauthorized
		}
		hasVoucher := strings.TrimSpace(eo.VoucherCode) != "" || len(eo.VoucherPhotos) > 0
		goods := roundMoney(goodsAmount)
		if hasVoucher {
			goods = 0
			if goodsAmount > 0 {
				return fmt.Errorf("%w: 该订单使用券码取货，无需代购金额", ErrErrandGoodsInvalid)
			}
		} else {
			if goods <= 0 {
				return fmt.Errorf("%w: 代购订单请填写实际商品金额", ErrErrandGoodsInvalid)
			}
			if goods > settings.GoodsMaxAmount {
				return fmt.Errorf("%w: 商品金额超过上限 %.2f 元", ErrErrandGoodsInvalid, settings.GoodsMaxAmount)
			}
			if len(receiptPhotos) == 0 {
				return fmt.Errorf("%w: 请上传购物小票照片", ErrErrandGoodsInvalid)
			}
			if len(receiptPhotos) > 6 {
				return fmt.Errorf("%w: 小票照片最多 6 张", ErrErrandGoodsInvalid)
			}
		}
		commission := roundMoney(eo.DeliveryFee * settings.PlatformCommissionPercent / 100)
		riderEarnings := roundMoney(eo.DeliveryFee - commission + goods)
		payAmount := roundMoney(eo.DeliveryFee + goods)
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":               model.ErrandStatusDelivering,
			"goods_amount":         goods,
			"goods_receipt_photos": toJSONColumn(receiptPhotos),
			"pay_amount":           payAmount,
			"rider_earnings":       riderEarnings,
			"platform_commission":  commission,
		}).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status":     model.DeliveryDelivering,
			"started_at": now,
		}).Error; err != nil {
			return err
		}
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventPicking,
			ActorRole:   model.FulfillmentActorRider,
			ActorID:     &riderID,
			Title:       "骑手已取货",
			Detail:      map[string]interface{}{"goods_amount": goods},
		})
		eo.Status = model.ErrandStatusDelivering
		eo.GoodsAmount = goods
		eo.PayAmount = payAmount
		eo.RiderEarnings = riderEarnings
		eo.PlatformCommission = commission
		eo.GoodsReceiptPhotos = receiptPhotos
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RiderComplete 骑手送达：选择平台支付（等待用户付款）或线下现金（立即完成收款）。
func (s *ErrandService) RiderComplete(riderID, id uint64, payMode uint8) (*ErrandView, error) {
	var out *ErrandView
	var createdEarning bool
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&eo, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrErrandNotFound
			}
			return err
		}
		if eo.Status != model.ErrandStatusDelivering {
			return fmt.Errorf("%w: 当前状态不可确认送达", ErrErrandStatusInvalid)
		}
		if eo.DeliveryOrderID == nil {
			return ErrErrandNotFound
		}
		var d model.DeliveryOrder
		if err := query.NotDeleted(tx).First(&d, *eo.DeliveryOrderID).Error; err != nil {
			return ErrErrandNotFound
		}
		if d.RiderID == nil || *d.RiderID != riderID {
			return ErrErrandUnauthorized
		}
		payAmount := roundMoney(eo.DeliveryFee + eo.GoodsAmount)
		switch payMode {
		case model.ErrandPayModePlatform:
			if eo.PayStatus == model.PayStatusUnpaid {
				expire := time.Now().Add(10 * time.Minute)
				if err := tx.Model(&eo).Updates(map[string]interface{}{
					"pay_mode":      model.ErrandPayModePlatform,
					"pay_amount":    payAmount,
					"pay_expire_at": expire,
				}).Error; err != nil {
					return err
				}
				eo.PayMode = model.ErrandPayModePlatform
				eo.PayAmount = payAmount
				eo.PayExpireAt = &expire
				AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
					SubjectType: model.FulfillmentSubjectErrand,
					SubjectID:   eo.ID,
					EventCode:   "pay_requested",
					ActorRole:   model.FulfillmentActorRider,
					ActorID:     &riderID,
					Title:       "等待用户平台支付",
					Detail:      map[string]interface{}{"amount": payAmount},
				})
			}
		case model.ErrandPayModeCash:
			now := time.Now()
			if err := tx.Model(&eo).Updates(map[string]interface{}{
				"pay_mode":         model.ErrandPayModeCash,
				"pay_amount":       payAmount,
				"collected_amount": payAmount,
				"pay_status":       model.PayStatusPaid,
				"paid_at":          now,
				"status":           model.ErrandStatusPendingConfirm,
				"pay_expire_at":    nil,
			}).Error; err != nil {
				return err
			}
			if err := tx.Model(&d).Updates(map[string]interface{}{
				"status":       model.DeliveryDelivered,
				"delivered_at": now,
			}).Error; err != nil {
				return err
			}
			AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
				SubjectType: model.FulfillmentSubjectErrand,
				SubjectID:   eo.ID,
				EventCode:   "cash_collected",
				ActorRole:   model.FulfillmentActorRider,
				ActorID:     &riderID,
				Title:       "骑手已送达并收取现金",
				Detail:      map[string]interface{}{"amount": payAmount},
			})
			eo.PayMode = model.ErrandPayModeCash
			eo.PayAmount = payAmount
			eo.CollectedAmount = payAmount
			eo.PayStatus = model.PayStatusPaid
			eo.Status = model.ErrandStatusPendingConfirm
			createdEarning = true
		default:
			return fmt.Errorf("%w: 请选择收款方式", ErrErrandStatusInvalid)
		}
		_ = createdEarning
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreatePrepay 用户对跑腿单发起平台支付（送达时）。
func (s *ErrandService) CreatePrepay(accountID, id uint64) (*payment.PrepayResult, error) {
	var eo model.ErrandOrder
	if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", id, accountID).First(&eo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrErrandNotFound
		}
		return nil, err
	}
	if eo.Status != model.ErrandStatusAccepted && eo.Status != model.ErrandStatusDelivering {
		return nil, fmt.Errorf("%w: 当前状态不可支付", ErrErrandStatusInvalid)
	}
	if eo.PayStatus != model.PayStatusUnpaid {
		return &payment.PrepayResult{Provider: s.paymentProvider().Name(), AlreadyPaid: true, NeedPay: false, Message: "订单已支付"}, nil
	}
	if eo.PayMode == 0 {
		if err := s.DB.Model(&eo).Update("pay_mode", model.ErrandPayModePlatform).Error; err != nil {
			return nil, err
		}
	}
	sub, err := payment.ErrandSubjectFromID(s.DB, eo.ID, 0)
	if err != nil {
		return nil, err
	}
	return s.paymentProvider().CreatePrepayForSubject(sub)
}

// Confirm 用户确认送达完成。
func (s *ErrandService) Confirm(accountID, id uint64) (*ErrandView, error) {
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND account_id = ?", id, accountID).First(&eo).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrErrandNotFound
			}
			return err
		}
		if err := s.confirmInTx(tx, &eo, model.FulfillmentActorUser); err != nil {
			return err
		}
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// confirmInTx 完成跑腿单：状态 4 → 5，配送单确认，生成骑手收益。
func (s *ErrandService) confirmInTx(tx *gorm.DB, eo *model.ErrandOrder, actor string) error {
	if eo.Status != model.ErrandStatusPendingConfirm {
		return fmt.Errorf("%w: 当前状态不可确认", ErrErrandStatusInvalid)
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status":      model.ErrandStatusCompleted,
		"finished_at": now,
	}
	if err := tx.Model(eo).Updates(updates).Error; err != nil {
		return err
	}
	eo.Status = model.ErrandStatusCompleted
	eo.FinishedAt = &now
	var riderID *uint64
	if eo.DeliveryOrderID != nil {
		var d model.DeliveryOrder
		if err := query.NotDeleted(tx).First(&d, *eo.DeliveryOrderID).Error; err == nil {
			riderID = d.RiderID
			_ = tx.Model(&d).Updates(map[string]interface{}{
				"status":         model.DeliveryConfirmed,
				"user_confirmed": 1,
			}).Error
			if d.RiderID != nil && eo.RiderEarnings > 0 {
				var cnt int64
				_ = tx.Model(&model.RiderEarning{}).
					Where("delivery_order_id = ? AND is_deleted = 0", d.ID).Count(&cnt).Error
				if cnt == 0 {
					earning := model.RiderEarning{
						RiderID:         *d.RiderID,
						DeliveryOrderID: d.ID,
						ErrandOrderID:   &eo.ID,
						MerchantID:      0,
						Amount:          eo.RiderEarnings,
						Status:          model.RiderEarningPending,
						CreatedAt:       now,
					}
					if err := tx.Create(&earning).Error; err != nil {
						return err
					}
				}
			}
		}
	}
	var actorID *uint64
	if actor == model.FulfillmentActorUser {
		actorID = nil
	} else if riderID != nil {
		actorID = riderID
	}
	AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
		SubjectType: model.FulfillmentSubjectErrand,
		SubjectID:   eo.ID,
		EventCode:   model.EventCompleted,
		ActorRole:   actor,
		ActorID:     actorID,
		Title:       "订单已完成",
	})
	return nil
}

// Appeal 用户申诉（送达后 24 小时内）。
func (s *ErrandService) Appeal(accountID, id uint64, reason string) (*ErrandView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写申诉说明", ErrErrandStatusInvalid)
	}
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND account_id = ?", id, accountID).First(&eo).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrErrandNotFound
			}
			return err
		}
		if eo.Status != model.ErrandStatusPendingConfirm && eo.Status != model.ErrandStatusCompleted {
			return fmt.Errorf("%w: 当前状态不可申诉", ErrErrandStatusInvalid)
		}
		if eo.FinishedAt != nil && time.Since(*eo.FinishedAt) > 24*time.Hour {
			return fmt.Errorf("%w: 已超过 24 小时申诉期", ErrErrandStatusInvalid)
		}
		reasonText := "用户申诉：" + reason
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":           model.ErrandStatusException,
			"exception_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		if eo.DeliveryOrderID != nil {
			_ = tx.Model(&model.DeliveryOrder{}).
				Where("id = ? AND status IN ?", *eo.DeliveryOrderID,
					[]int{int(model.DeliveryDelivered), int(model.DeliveryConfirmed)}).
				Updates(map[string]interface{}{
					"status":           model.DeliveryException,
					"exception_reason": reasonText,
				}).Error
		}
		eo.Status = model.ErrandStatusException
		eo.ExceptionReason = &reasonText
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventExceptionReported,
			ActorRole:   model.FulfillmentActorUser,
			Title:       "用户发起申诉",
			Detail:      map[string]interface{}{"reason": reasonText},
		})
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReportExceptionByRider 骑手上报异常（外卖配送异常会同步到跑腿单）。
func (s *ErrandService) ReportExceptionByRider(riderID, id uint64, reason string) (*ErrandView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写异常说明", ErrErrandStatusInvalid)
	}
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&eo, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrErrandNotFound
			}
			return err
		}
		if eo.Status != model.ErrandStatusAccepted && eo.Status != model.ErrandStatusDelivering {
			return fmt.Errorf("%w: 当前状态不可上报异常", ErrErrandStatusInvalid)
		}
		if eo.DeliveryOrderID == nil {
			return ErrErrandNotFound
		}
		var d model.DeliveryOrder
		if err := query.NotDeleted(tx).First(&d, *eo.DeliveryOrderID).Error; err != nil {
			return ErrErrandNotFound
		}
		if d.RiderID == nil || *d.RiderID != riderID {
			return ErrErrandUnauthorized
		}
		reasonText := "骑手上报：" + reason
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":           model.ErrandStatusException,
			"exception_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		eo.Status = model.ErrandStatusException
		eo.ExceptionReason = &reasonText
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventExceptionReported,
			ActorRole:   model.FulfillmentActorRider,
			ActorID:     &riderID,
			Title:       "骑手上报异常",
			Detail:      map[string]interface{}{"reason": reasonText},
		})
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// --- 管理端 ---

// AdminList 管理端跑腿单列表。
func (s *ErrandService) AdminList(status *uint8, keyword string, page, pageSize int) ([]ErrandView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	q := query.NotDeleted(s.DB.Model(&model.ErrandOrder{}))
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		q = q.Where("order_no LIKE ? OR goods_desc LIKE ? OR drop_snapshot->>'$.contact_phone' LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.ErrandOrder
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]ErrandView, 0, len(list))
	for i := range list {
		out = append(out, *s.buildView(s.DB, list[i]))
	}
	return out, total, nil
}

func (s *ErrandService) AdminGet(id uint64) (*ErrandView, error) {
	return s.Get(0, id)
}

// AdminResume 异常恢复：已付款回到待确认，否则回到配送中。
func (s *ErrandService) AdminResume(id uint64, remark string) (*ErrandView, error) {
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&eo, id).Error; err != nil {
			return ErrErrandNotFound
		}
		if eo.Status != model.ErrandStatusException {
			return fmt.Errorf("%w: 仅异常单可恢复", ErrErrandStatusInvalid)
		}
		target := model.ErrandStatusDelivering
		if eo.PayStatus == model.PayStatusPaid {
			target = model.ErrandStatusPendingConfirm
		}
		note := appendAdminRemark(eo.ExceptionReason, remark)
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":           target,
			"exception_reason": note,
		}).Error; err != nil {
			return err
		}
		if eo.DeliveryOrderID != nil {
			deliveryStatus := uint8(model.DeliveryDelivering)
			if target == model.ErrandStatusPendingConfirm {
				deliveryStatus = model.DeliveryDelivered
			}
			_ = tx.Model(&model.DeliveryOrder{}).
				Where("id = ? AND status = ?", *eo.DeliveryOrderID, model.DeliveryException).
				Updates(map[string]interface{}{"status": deliveryStatus, "exception_reason": note}).Error
		}
		eo.Status = target
		eo.ExceptionReason = &note
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventAdminResolved,
			ActorRole:   model.FulfillmentActorAdmin,
			Title:       "管理员恢复订单",
			Detail:      map[string]interface{}{"remark": remark},
		})
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AdminReassign 异常改派：清空骑手回到待接单。
func (s *ErrandService) AdminReassign(id uint64, remark string) (*ErrandView, error) {
	var out *ErrandView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&eo, id).Error; err != nil {
			return ErrErrandNotFound
		}
		if eo.Status != model.ErrandStatusException {
			return fmt.Errorf("%w: 仅异常单可改派", ErrErrandStatusInvalid)
		}
		note := appendAdminRemark(eo.ExceptionReason, remark)
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":           model.ErrandStatusPendingAccept,
			"exception_reason": note,
		}).Error; err != nil {
			return err
		}
		if eo.DeliveryOrderID != nil {
			_ = tx.Model(&model.DeliveryOrder{}).
				Where("id = ?", *eo.DeliveryOrderID).
				Updates(map[string]interface{}{
					"status":           model.DeliveryPendingAccept,
					"rider_id":         nil,
					"accepted_at":      nil,
					"started_at":       nil,
					"exception_reason": note,
				}).Error
			_ = tx.Model(&model.RiderEarning{}).
				Where("delivery_order_id = ? AND status = ?", *eo.DeliveryOrderID, model.RiderEarningPending).
				Update("status", model.RiderEarningCancelled).Error
		}
		eo.Status = model.ErrandStatusPendingAccept
		eo.ExceptionReason = &note
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AdminCancel 异常取消：平台已支付则退款，现金单仅取消。
func (s *ErrandService) AdminCancel(id uint64, remark string) (*ErrandView, error) {
	var out *ErrandView
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		var eo model.ErrandOrder
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&eo, id).Error; err != nil {
			return ErrErrandNotFound
		}
		if eo.Status != model.ErrandStatusException && eo.Status != model.ErrandStatusPendingConfirm {
			return fmt.Errorf("%w: 当前状态不可取消", ErrErrandStatusInvalid)
		}
		note := appendAdminRemark(eo.ExceptionReason, remark)
		if eo.PayMode == model.ErrandPayModePlatform && eo.PayStatus == model.PayStatusPaid {
			sub, err := payment.ErrandSubjectFromID(tx, eo.ID, 0)
			if err != nil {
				return err
			}
			if err := s.paymentProvider().RefundSubjectAmountInTx(tx, sub, 0, "管理员取消跑腿退款"); err != nil {
				return err
			}
		}
		if err := tx.Model(&eo).Updates(map[string]interface{}{
			"status":           model.ErrandStatusCancelled,
			"cancel_reason":    note,
			"exception_reason": note,
		}).Error; err != nil {
			return err
		}
		if eo.DeliveryOrderID != nil {
			_ = tx.Model(&model.DeliveryOrder{}).
				Where("id = ?", *eo.DeliveryOrderID).
				Updates(map[string]interface{}{"status": model.DeliveryCancelled, "exception_reason": note}).Error
			_ = tx.Model(&model.RiderEarning{}).
				Where("delivery_order_id = ? AND status = ?", *eo.DeliveryOrderID, model.RiderEarningPending).
				Update("status", model.RiderEarningCancelled).Error
		}
		eo.Status = model.ErrandStatusCancelled
		eo.CancelReason = &note
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectErrand,
			SubjectID:   eo.ID,
			EventCode:   model.EventCancelled,
			ActorRole:   model.FulfillmentActorAdmin,
			Title:       "管理员取消订单",
			Detail:      map[string]interface{}{"remark": remark},
		})
		out = s.buildView(tx, eo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return out, nil
}

// AutoConfirmStale 定时自动确认（默认 24 小时）。
func (s *ErrandService) AutoConfirmStale(now time.Time) (int, error) {
	settings := LoadErrandSettings(s.DB)
	deadline := now.Add(-time.Duration(settings.AutoConfirmHours) * time.Hour)
	var list []model.ErrandOrder
	if err := query.NotDeleted(s.DB).
		Where("status = ? AND updated_at <= ?", model.ErrandStatusPendingConfirm, deadline).
		Limit(50).Find(&list).Error; err != nil {
		return 0, err
	}
	n := 0
	for i := range list {
		eo := list[i]
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			var locked model.ErrandOrder
			if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
				First(&locked, eo.ID).Error; err != nil {
				return err
			}
			if locked.Status != model.ErrandStatusPendingConfirm {
				return nil
			}
			return s.confirmInTx(tx, &locked, model.FulfillmentActorSystem)
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
