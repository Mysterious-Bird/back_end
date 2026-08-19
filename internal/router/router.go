package router

import (
	"log"
	"os"
	"time"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/handler"
	"yujixinjiang/backend/internal/middleware"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/service"
	"yujixinjiang/backend/internal/storage"
	"yujixinjiang/backend/internal/wechat"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Setup(cfg *config.Config, db *gorm.DB) *gin.Engine {
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(middleware.CORS())

	uploadStore := storage.NewLocal(cfg.Upload)
	if err := uploadStore.EnsureDir(); err != nil {
		panic("创建上传目录失败: " + err.Error())
	}
	r.Static(cfg.Upload.URLPrefix, cfg.Upload.Dir)

	// 生产默认关闭 Swagger；需要时设 ENABLE_SWAGGER=true
	if !config.IsRelease() || os.Getenv("ENABLE_SWAGGER") == "true" {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	health := &handler.HealthHandler{DB: db}

	var wechatClient *wechat.Client
	if cfg.WeChat.AppID != "" && cfg.WeChat.Secret != "" {
		wechatClient = wechat.NewClient(cfg.WeChat.AppID, cfg.WeChat.Secret)
	}
	authHandler := &handler.AuthHandler{Svc: &service.AuthService{
		DB:               db,
		JWTSecret:        cfg.JWT.Secret,
		WeChat:           wechatClient,
		AvatarPublicBase: cfg.Upload.AvatarPublicBase,
	}}

	cartSvc := &service.CartService{DB: db}
	deliveryZoneSvc := &service.DeliveryZoneService{DB: db}
	inventorySvc := &service.InventoryService{DB: db, ZoneSvc: deliveryZoneSvc}
	couponSvc := &service.CouponService{DB: db}
	activitySvc := &service.ActivityService{DB: db}
	announcementSvc := &service.AnnouncementService{DB: db}
	homeCarouselSvc := &service.HomeCarouselService{DB: db}
	bargainSvc := &service.BargainService{DB: db}
	payProvider, err := payment.NewProvider(cfg, db)
	if err != nil {
		panic("初始化支付渠道失败: " + err.Error())
	}
	log.Printf("支付渠道: %s (immediate_settle=%v)", payProvider.Name(), payProvider.ImmediateSettle())
	// 微信支付：启动时下载平台证书（回调验签用）
	if wp, ok := payProvider.(*payment.WeChatProvider); ok && wp.Client != nil {
		if err := wp.Client.FetchPlatformCerts(); err != nil {
			log.Printf("[wechat] 平台证书下载失败: %v", err)
		}
	}
	orderSvc := &service.OrderService{
		DB: db, InventorySvc: inventorySvc, CouponSvc: couponSvc,
		ActivitySvc: activitySvc, ZoneSvc: deliveryZoneSvc, Payment: payProvider,
		PayTimeoutMinutes: cfg.Payment.PayTimeoutMinutes,
		AvatarPublicBase:  cfg.Upload.AvatarPublicBase,
	}
	takeoutSvc := &service.TakeoutService{
		DB: db, ZoneSvc: deliveryZoneSvc, Payment: payProvider,
		PayTimeoutMinutes: cfg.Payment.PayTimeoutMinutes,
	}
	deliveryFeePaySvc := &service.DeliveryFeePayService{
		DB: db, InventorySvc: inventorySvc, ZoneSvc: deliveryZoneSvc, Payment: payProvider,
		PayTimeoutMinutes: cfg.Payment.PayTimeoutMinutes,
	}
	inventorySvc.DeliveryFeePaySvc = deliveryFeePaySvc
	if wp, ok := payProvider.(*payment.WeChatProvider); ok {
		wp.OnPaidInTx = orderSvc.AdvanceAfterPaidInTx
		wp.OnSubjectPaidInTx = func(tx *gorm.DB, sub payment.PaySubject) error {
			switch sub.Type {
			case model.PaySubjectTakeout:
				return takeoutSvc.MarkPaidInTx(tx, sub.ID, time.Now())
			case model.PaySubjectDeliveryFee:
				return deliveryFeePaySvc.MarkPaidInTx(tx, sub.ID, time.Now())
			case model.PaySubjectOrder:
				return orderSvc.AdvanceAfterPaidInTx(tx, sub.ID)
			default:
				return nil
			}
		}
	}
	startGroupExpireWorker(orderSvc)
	startUsageExpireWorker(orderSvc)
	startPendingPayExpireWorker(orderSvc, takeoutSvc, deliveryFeePaySvc)
	service.MigrateBagToPendingVerifyIfEnabled(db)
	paymentHandler := &handler.PaymentHandler{OrderSvc: orderSvc}
	deliverySvc := &service.DeliveryService{DB: db, InventorySvc: inventorySvc, DeliveryFeePaySvc: deliveryFeePaySvc}
	riderEarningSvc := &service.RiderEarningService{DB: db}
	dashboardSvc := &service.DashboardService{DB: db}
	categorySvc := &service.CategoryService{DB: db}
	userSvc := &service.UserService{DB: db}

	userHandler := &handler.UserHandler{
		Svc:          userSvc,
		RiderSvc:     &service.RiderApplicationService{DB: db},
		AddressSvc:   &service.AddressService{DB: db},
		CartSvc:      cartSvc,
		OrderSvc:     orderSvc,
		TakeoutSvc:   takeoutSvc,
		InventorySvc: inventorySvc,
		DeliverySvc:  deliverySvc,
	}
	merchantSvc := &service.MerchantService{DB: db}
	takeoutHandler := &handler.TakeoutHandler{TakeoutSvc: takeoutSvc, MerchantSvc: merchantSvc}
	deliveryFeeHandler := &handler.DeliveryFeeHandler{Svc: deliveryFeePaySvc}
	productSvc := &service.ProductService{DB: db, CategorySvc: categorySvc, GroupCloser: orderSvc}
	verifySvc := &service.VerificationService{DB: db, InventorySvc: inventorySvc, ProductSvc: productSvc}
	riderSvc := &service.RiderApplicationService{DB: db}
	adminHandler := &handler.AdminHandler{MerchantSvc: merchantSvc, ProductSvc: productSvc, RiderSvc: riderSvc, EarningSvc: riderEarningSvc}
	merchantHandler := &handler.MerchantHandler{MerchantSvc: merchantSvc, ProductSvc: productSvc, CategorySvc: categorySvc, OrderSvc: orderSvc}
	storeHandler := &handler.StoreHandler{
		MerchantSvc: merchantSvc, ProductSvc: productSvc, OrderSvc: orderSvc,
		CategorySvc: categorySvc, CouponSvc: couponSvc, ActivitySvc: activitySvc,
		ZoneSvc: deliveryZoneSvc,
	}
	categoryHandler := &handler.CategoryHandler{CategorySvc: categorySvc}
	merchantOrderHandler := &handler.MerchantOrderHandler{
		MerchantSvc: merchantSvc, OrderSvc: orderSvc, VerifySvc: verifySvc,
		InventorySvc: inventorySvc, DashboardSvc: dashboardSvc, DeliverySvc: deliverySvc,
	}
	riderHandler := &handler.RiderHandler{DeliverySvc: deliverySvc, EarningSvc: riderEarningSvc}
	adminDashboardHandler := &handler.AdminDashboardHandler{
		DashboardSvc: dashboardSvc, OrderSvc: orderSvc, VerifySvc: verifySvc,
	}
	uploadHandler := &handler.UploadHandler{Store: uploadStore}
	couponHandler := &handler.CouponHandler{
		CouponSvc: couponSvc, MerchantSvc: merchantSvc, ProductSvc: productSvc,
	}
	adminExtraHandler := &handler.AdminExtraHandler{
		UserSvc: userSvc, CategorySvc: categorySvc,
		DeliverySvc: deliverySvc, InventorySvc: inventorySvc,
	}
	announcementHandler := &handler.AnnouncementHandler{
		AnnouncementSvc: announcementSvc, MerchantSvc: merchantSvc,
	}
	homeCarouselHandler := &handler.HomeCarouselHandler{Svc: homeCarouselSvc}
	bargainHandler := &handler.BargainHandler{BargainSvc: bargainSvc}
	activityHandler := &handler.ActivityHandler{
		ActivitySvc: activitySvc, MerchantSvc: merchantSvc,
	}
	rankHandler := &handler.RankHandler{
		RankSvc: &service.RankService{DB: db},
	}
	deliveryZoneHandler := &handler.DeliveryZoneHandler{
		ZoneSvc: deliveryZoneSvc, MerchantSvc: merchantSvc, TencentKey: cfg.Map.TencentKey,
	}
	fulfillmentEventSvc := &service.FulfillmentEventService{DB: db}
	fulfillmentEventHandler := &handler.FulfillmentEventHandler{
		DB: db, Svc: fulfillmentEventSvc, MerchantSvc: merchantSvc,
	}

	r.GET("/api/health", health.Check)

	public := r.Group("/api")
	{
		public.POST("/payments/wechat/notify", paymentHandler.WeChatNotify)
		public.POST("/auth/login", authHandler.Login)
		public.GET("/categories", categoryHandler.ListCategories)
		public.GET("/products", storeHandler.ListProductsByMerchant)
		public.GET("/products/:id/group", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.GetGroupProgress)
		public.GET("/products/:id/group/teams", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.ListProductJoinableTeams)
		public.GET("/products/:id", storeHandler.GetProduct)

		public.GET("/announcements", announcementHandler.ListPublic)
		public.GET("/home/carousel", homeCarouselHandler.ListPublic)
		public.GET("/bargain/sessions/:id", middleware.OptionalAuth(cfg.JWT.Secret, db), bargainHandler.Get)
		public.GET("/seckill/products", middleware.OptionalAuth(cfg.JWT.Secret, db), activityHandler.ListSeckillProducts)
		public.GET("/rank/hot-groups", rankHandler.ListHotGroups)
		public.GET("/rank/hot-sales", rankHandler.ListHotSales)
		public.GET("/rank/save", rankHandler.ListSaveRank)
		public.GET("/activities/:id", activityHandler.GetPublic)
		public.GET("/activities/:id/products", activityHandler.ListPublicProducts)
		public.GET("/activities/:id/products/:activity_product_id", middleware.OptionalAuth(cfg.JWT.Secret, db), activityHandler.GetPublicProduct)
		public.GET("/activities/:id/products/:activity_product_id/group", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.GetActivityGroupProgress)
		public.GET("/activities/:id/products/:activity_product_id/group/teams", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.ListActivityJoinableTeams)

		public.GET("/merchants", storeHandler.ListMerchants)
		public.GET("/merchants/:id", storeHandler.GetMerchant)
		public.GET("/merchants/:id/store", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.GetMerchantStore)
		public.GET("/merchants/:id/announcements", announcementHandler.ListByMerchantPublic)
		public.GET("/merchants/:id/categories", storeHandler.ListMerchantCategories)
		public.GET("/merchants/:id/activities", activityHandler.ListPublicByMerchant)
		public.GET("/merchants/:id/coupons", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.ListMerchantCoupons)
		public.GET("/merchants/:id/products", storeHandler.ListMerchantProducts)
		public.GET("/merchants/:id/products/:product_id", storeHandler.GetMerchantProduct)
		public.GET("/merchants/:id/delivery-zone", deliveryZoneHandler.GetPublic)
		public.POST("/merchants/:id/delivery-zone/check", deliveryZoneHandler.CheckPublic)
		public.GET("/coupons", middleware.OptionalAuth(cfg.JWT.Secret, db), couponHandler.ListPublic)
	}

	authorized := r.Group("/api")
	authorized.Use(middleware.RequireAuth(cfg.JWT.Secret, db))
	{
		authorized.GET("/auth/me", authHandler.Me)
		authorized.PATCH("/auth/profile", authHandler.UpdateProfile)
		authorized.POST("/auth/wechat/phone", authHandler.WeChatPhone)
		authorized.POST("/auth/avatar", authHandler.Avatar)
		authorized.POST("/upload", uploadHandler.UploadImage)

		// 骑手申请：任意已登录账号可访问，具体能否提交由业务层按账号类型判断
		authorized.POST("/user/rider/application", userHandler.ApplyRider)
		authorized.GET("/user/rider/application", userHandler.GetRiderApplication)

		user := authorized.Group("")
		// admin 可代客下单测试（OrderService 仍按 accountID 隔离数据，无越权风险）
		user.Use(middleware.RequireAccountTypes(model.AccountTypeUser, model.AccountTypeAdmin))
		user.POST("/bargain/sessions", bargainHandler.Start)
		user.POST("/bargain/sessions/:id/help", bargainHandler.Help)
		registerUserRoutes(user, userHandler, couponHandler, paymentHandler, takeoutHandler, deliveryFeeHandler, fulfillmentEventHandler)

		merchant := authorized.Group("/merchant")
		merchant.Use(middleware.RequireAccountTypes(model.AccountTypeMerchant, model.AccountTypeAdmin))
		registerMerchantRoutes(merchant, merchantHandler, merchantOrderHandler, takeoutHandler, couponHandler, announcementHandler, activityHandler, deliveryZoneHandler, fulfillmentEventHandler)

		admin := authorized.Group("/admin")
		admin.Use(middleware.RequireAccountTypes(model.AccountTypeAdmin))
		registerAdminRoutes(admin, adminHandler, adminDashboardHandler, couponHandler, adminExtraHandler, announcementHandler, deliveryZoneHandler, activityHandler, fulfillmentEventHandler, homeCarouselHandler)

		rider := authorized.Group("/rider")
		rider.Use(middleware.RequireRider())
		registerRiderRoutes(rider, riderHandler)
	}

	return r
}

func registerUserRoutes(r *gin.RouterGroup, h *handler.UserHandler, ch *handler.CouponHandler, ph *handler.PaymentHandler, th *handler.TakeoutHandler, dfh *handler.DeliveryFeeHandler, fe *handler.FulfillmentEventHandler) {
	r.GET("/user/overview", h.Overview)
	r.GET("/user/profile", h.Profile)
	r.GET("/user/orders", h.Orders)
	r.POST("/user/orders", h.CreateOrder)
	r.GET("/user/orders/:id/events", fe.ListUserOrderEvents)
	r.GET("/user/orders/:id", h.OrderDetail)
	r.POST("/user/orders/:id/pay", ph.CreatePrepay)
	r.POST("/user/orders/:id/cancel", h.CancelOrder)
	r.POST("/user/orders/:id/request-use", h.RequestUse)
	r.POST("/user/orders/:id/confirm-pickup", h.ConfirmPickup)
	r.POST("/user/orders/:id/confirm-receipt", h.ConfirmOrderReceipt)
	r.GET("/user/payment/provider", ph.Provider)
	r.POST("/user/takeout-orders", th.Create)
	r.GET("/user/takeout-orders", th.List)
	r.GET("/user/takeout-orders/:id/events", fe.ListUserTakeoutEvents)
	r.GET("/user/takeout-orders/:id", th.Get)
	r.POST("/user/takeout-orders/:id/pay", th.Pay)
	r.POST("/user/takeout-orders/:id/cancel", th.Cancel)
	r.POST("/user/delivery-fee-orders", dfh.Create)
	r.POST("/user/delivery-fee-orders/:id/pay", dfh.Pay)
	r.GET("/user/deliveries", h.ListUserDeliveries)
	r.GET("/user/deliveries/:id/events", fe.ListUserDeliveryEvents)
	r.GET("/user/deliveries/:id", h.GetUserDelivery)
	r.POST("/user/deliveries/:id/confirm", h.ConfirmDeliveryReceipt)
	r.POST("/user/deliveries/:id/exception", h.ReportUserDeliveryException)
	r.POST("/user/deliveries/:id/cancel", h.CancelUserDelivery)
	r.GET("/user/cart", h.Cart)
	r.POST("/user/cart", h.AddCart)
	r.POST("/user/cart/checkout", h.CheckoutCart)
	r.POST("/user/cart/takeout-checkout", h.CheckoutCartTakeout)
	r.PATCH("/user/cart/:id", h.UpdateCart)
	r.DELETE("/user/cart/:id", h.DeleteCart)
	r.GET("/user/coupons", h.Coupons)
	r.GET("/user/coupons/applicable", ch.ListApplicable)
	r.POST("/user/coupons/claim", ch.Claim)
	r.GET("/user/inventory", h.Inventory)
	r.POST("/user/inventory/use-batch", h.UseInventoryBatch)
	r.POST("/user/inventory/:id/use", h.UseInventory)
	r.GET("/user/inventory/:id/refund-sources", h.ListInventoryRefundSources)
	r.POST("/user/inventory/:id/refund", h.RefundInventory)
	r.GET("/user/inventory/usages", h.ListInventoryUsages)
	r.GET("/user/inventory/usages/:id", h.GetInventoryUsage)
	r.POST("/user/inventory/usages/:id/cancel", h.CancelInventoryUsage)
	r.POST("/user/inventory/usages/:id/refund", h.RefundPendingVerifyUsage)
	r.POST("/user/inventory/usages/:id/convert-delivery", h.ConvertPendingVerifyToDelivery)

	r.GET("/user/addresses", h.ListAddresses)
	r.POST("/user/addresses", h.CreateAddress)
	r.GET("/user/addresses/:id", h.GetAddress)
	r.PUT("/user/addresses/:id", h.UpdateAddress)
	r.PATCH("/user/addresses/:id", h.PatchAddress)
	r.DELETE("/user/addresses/:id", h.DeleteAddress)
	r.PATCH("/user/addresses/:id/default", h.SetDefaultAddress)
}

// startGroupExpireWorker 定时关闭超时未成团拼团并模拟退款。
func startGroupExpireWorker(orderSvc *service.OrderService) {
	go func() {
		run := func() {
			n, err := orderSvc.ExpireStaleGroupTeams(time.Now())
			if err != nil {
				log.Printf("拼团超时部分失败: %v (本批成功 %d)", err, n)
			} else if n > 0 {
				log.Printf("拼团超时已处理 %d 个团", n)
			}
		}
		run() // 启动时立即扫一遍，避免刚过期的团要等满一分钟
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// startUsageExpireWorker 定时对待核销已过期记录自动退款。
func startUsageExpireWorker(orderSvc *service.OrderService) {
	go func() {
		run := func() {
			n, err := orderSvc.ExpireStalePendingVerifyUsages(time.Now())
			if err != nil {
				log.Printf("待核销过期退款部分失败: %v (本批成功 %d)", err, n)
			} else if n > 0 {
				log.Printf("待核销过期已自动退款 %d 条", n)
			}
		}
		run()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// startPendingPayExpireWorker 定时关闭超时未支付的入包/外卖/配送费订单。
func startPendingPayExpireWorker(orderSvc *service.OrderService, takeoutSvc *service.TakeoutService, deliveryFeePaySvc *service.DeliveryFeePayService) {
	go func() {
		// 15s 一轮，配合 5 分钟支付窗，倒计时结束后尽快关单
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			n, err := orderSvc.ExpireStalePendingPayOrders(now)
			if err != nil {
				log.Printf("待支付超时处理部分失败: %v (本批成功 %d)", err, n)
			} else if n > 0 {
				log.Printf("待支付超时已处理 %d 个入包订单（关单或补入账）", n)
			}
			tn, terr := takeoutSvc.ExpireStalePendingPay(now)
			if terr != nil {
				log.Printf("外卖待支付超时关单部分失败: %v (本批成功 %d)", terr, tn)
			} else if tn > 0 {
				log.Printf("外卖待支付超时已关闭 %d 个订单", tn)
			}
			fn, ferr := deliveryFeePaySvc.ExpireStalePendingPay(now)
			if ferr != nil {
				log.Printf("配送费待支付超时关单部分失败: %v (本批成功 %d)", ferr, fn)
			} else if fn > 0 {
				log.Printf("配送费待支付超时已关闭 %d 个订单", fn)
			}
		}
	}()
}

func registerMerchantRoutes(r *gin.RouterGroup, h *handler.MerchantHandler, mo *handler.MerchantOrderHandler, to *handler.TakeoutHandler, ch *handler.CouponHandler, ah *handler.AnnouncementHandler, act *handler.ActivityHandler, dz *handler.DeliveryZoneHandler, fe *handler.FulfillmentEventHandler) {
	r.GET("/profile", h.GetProfile)
	r.PATCH("/profile", h.UpdateProfile)
	r.PATCH("/profile/images", h.UpdateShopImages)
	r.GET("/delivery-zone", dz.GetMerchant)
	r.PUT("/delivery-zone", dz.PutMerchant)
	r.PATCH("/delivery-zone", dz.PatchMerchant)
	r.DELETE("/delivery-zone", dz.DeleteMerchant)
	r.POST("/delivery-zone/poi", dz.SearchMerchantPOI)
	r.POST("/deliveries/:id/prepare", mo.MarkDeliveryPrepared)
	r.POST("/deliveries/:id/reject", mo.RejectDeliveryPrepare)
	r.GET("/deliveries/preparing", mo.ListPreparingDeliveries)
	r.GET("/deliveries/prepared", mo.ListPreparedDeliveries)
	r.GET("/takeout-orders", to.MerchantList)
	r.GET("/takeout-orders/:id/events", fe.ListMerchantTakeoutEvents)
	r.GET("/takeout-orders/:id", to.MerchantGet)
	r.POST("/takeout-orders/:id/prepare", to.MerchantPrepare)
	r.POST("/takeout-orders/:id/reject", to.MerchantReject)
	r.GET("/orders/:id/events", fe.ListMerchantOrderEvents)
	r.GET("/dashboard", mo.Dashboard)
	r.GET("/sales", mo.SalesReport)

	// 商品/分类/活动：商家只读；写操作仅管理员（管理端走 /admin）
	catalogWrite := r.Group("")
	catalogWrite.Use(middleware.RejectMerchantCatalogWrites())
	{
		catalogWrite.POST("/products", h.CreateProduct)
		catalogWrite.PUT("/products/:id", h.UpdateProduct)
		catalogWrite.PATCH("/products/:id", h.PatchProduct)
		catalogWrite.DELETE("/products/:id", h.DeleteProduct)
		catalogWrite.PATCH("/products/:id/status", h.UpdateProductStatus)
		catalogWrite.PATCH("/products/:id/price", h.UpdateProductPrice)
		catalogWrite.PATCH("/products/:id/stock", h.UpdateProductStock)
		catalogWrite.PATCH("/products/:id/group-buy", h.UpdateProductGroupBuy)
		catalogWrite.PATCH("/products/:id/coupon", h.UpdateProductCoupon)
		catalogWrite.PATCH("/products/:id/sale", h.UpdateProductSale)
		catalogWrite.PATCH("/products/:id/images", h.UpdateProductImages)

		catalogWrite.POST("/categories", h.CreateCategory)
		catalogWrite.PATCH("/categories/:id", h.UpdateCategory)
		catalogWrite.DELETE("/categories/:id", h.DeleteCategory)

		catalogWrite.POST("/activities", act.CreateMerchant)
		catalogWrite.PATCH("/activities/:id", act.UpdateMerchant)
		catalogWrite.DELETE("/activities/:id", act.DeleteMerchant)
		catalogWrite.POST("/activities/:id/products", act.AddProduct)
		catalogWrite.PATCH("/activities/:id/products/:activity_product_id", act.UpdateProduct)
		catalogWrite.PUT("/activities/:id/products/:activity_product_id", act.UpdateProductPut)
		catalogWrite.DELETE("/activities/:id/products/:activity_product_id", act.RemoveProduct)
	}

	r.GET("/products", h.ListProducts)
	r.GET("/products/:id", h.GetProduct)
	r.GET("/categories", h.ListCategories)

	r.GET("/orders", mo.ListOrders)
	r.GET("/orders/:id", mo.GetOrder)
	r.PATCH("/orders/:id/review", mo.ReviewOrder)
	r.PATCH("/orders/:id/use-review", mo.UseReviewOrder)
	r.GET("/verify", mo.PreviewVerify)
	r.POST("/verify", mo.Verify)
	r.GET("/verification-records", mo.ListVerificationRecords)
	r.GET("/inventory-usages", mo.ListInventoryUsages)
	r.GET("/inventory-usages/package-pending", mo.ListPendingPackageSelections)
	r.GET("/inventory-usages/:id", mo.GetInventoryUsage)
	r.PATCH("/inventory-usages/:id/cancel-review", mo.ReviewCancelInventoryUsage)
	r.POST("/inventory-usages/:id/confirm-package", mo.ConfirmPackageSelection)

	r.GET("/coupons", ch.ListMerchant)
	r.POST("/coupons", ch.CreateMerchant)
	r.GET("/coupons/:id", ch.GetMerchant)
	r.PATCH("/coupons/:id", ch.UpdateMerchant)
	r.PATCH("/coupons/:id/status", ch.UpdateMerchantStatus)

	r.GET("/announcements", ah.ListMerchant)
	r.POST("/announcements", ah.CreateMerchant)
	r.PATCH("/announcements/:id", ah.UpdateMerchant)
	r.DELETE("/announcements/:id", ah.DeleteMerchant)

	r.GET("/activities", act.ListMerchant)
	r.GET("/activities/:id", act.GetMerchant)
	r.GET("/activities/:id/products", act.ListMerchantProducts)
	r.GET("/activities/:id/products/:activity_product_id", act.GetMerchantProduct)
}

func registerAdminRoutes(r *gin.RouterGroup, h *handler.AdminHandler, ad *handler.AdminDashboardHandler, ch *handler.CouponHandler, ae *handler.AdminExtraHandler, ah *handler.AnnouncementHandler, dz *handler.DeliveryZoneHandler, act *handler.ActivityHandler, fe *handler.FulfillmentEventHandler, hc *handler.HomeCarouselHandler) {
	r.POST("/merchants", h.CreateMerchant)
	r.GET("/merchants", h.ListMerchants)
	r.GET("/merchants/:id", h.GetMerchant)
	r.PATCH("/merchants/:id", h.UpdateMerchant)
	r.PATCH("/merchants/:id/status", h.UpdateMerchantStatus)
	r.PATCH("/merchants/:id/images", h.UpdateMerchantImages)
	r.GET("/merchants/:id/operator", h.GetMerchantOperator)
	r.PUT("/merchants/:id/operator", h.PutMerchantOperator)
	r.DELETE("/merchants/:id/operator", h.DeleteMerchantOperator)
	r.GET("/merchants/:id/delivery-zone", dz.GetAdmin)
	r.PUT("/merchants/:id/delivery-zone", dz.PutAdmin)
	r.PATCH("/merchants/:id/delivery-zone", dz.PatchAdmin)
	r.DELETE("/merchants/:id/delivery-zone", dz.DeleteAdmin)
	r.POST("/merchants/:id/delivery-zone/poi", dz.SearchAdminPOI)

	r.POST("/products", h.CreateProduct)
	r.GET("/products", h.ListProducts)
	r.GET("/products/:id", h.GetProduct)
	r.PUT("/products/:id", h.UpdateProduct)
	r.PATCH("/products/:id", h.PatchProduct)
	r.DELETE("/products/:id", h.DeleteProduct)
	r.PATCH("/products/:id/status", h.UpdateProductStatus)
	r.PATCH("/products/:id/price", h.UpdateProductPrice)
	r.PATCH("/products/:id/stock", h.UpdateProductStock)
	r.PATCH("/products/:id/group-buy", h.UpdateProductGroupBuy)
	r.PATCH("/products/:id/coupon", h.UpdateProductCoupon)
	r.PATCH("/products/:id/sale", h.UpdateProductSale)
	r.PATCH("/products/:id/images", h.UpdateProductImages)

	r.GET("/activities", act.ListAdmin)
	r.POST("/activities", act.CreateAdmin)
	r.GET("/activities/:id", act.GetAdmin)
	r.PATCH("/activities/:id", act.UpdateAdmin)
	r.DELETE("/activities/:id", act.DeleteAdmin)
	r.GET("/activities/:id/products", act.ListAdminProducts)
	r.POST("/activities/:id/products", act.AddAdminProduct)
	r.GET("/activities/:id/products/:activity_product_id", act.GetAdminProduct)
	r.PATCH("/activities/:id/products/:activity_product_id", act.UpdateAdminProduct)
	r.PUT("/activities/:id/products/:activity_product_id", act.UpdateAdminProductPut)
	r.DELETE("/activities/:id/products/:activity_product_id", act.RemoveAdminProduct)

	r.GET("/dashboard", ad.Dashboard)
	r.GET("/sales", ad.SalesReport)
	r.GET("/orders", ad.ListOrders)
	r.GET("/orders/:id", ad.GetOrder)
	r.GET("/fulfillment-events", fe.ListAdminEvents)
	r.GET("/verification-records", ad.ListVerificationRecords)

	r.GET("/users", ae.ListUsers)
	r.PATCH("/users/:id/status", ae.SetUserStatus)
	r.GET("/categories", ae.ListCategories)
	r.POST("/categories", ae.CreateCategory)
	r.GET("/categories/:id", ae.GetCategory)
	r.PATCH("/categories/:id", ae.UpdateCategory)
	r.DELETE("/categories/:id", ae.DeleteCategory)
	r.GET("/deliveries", ae.ListDeliveries)
	r.GET("/deliveries/exceptions", ae.ListDeliveryExceptions)
	r.GET("/bag-deliveries/pending", ae.ListPendingBagDeliveries)
	r.POST("/bag-deliveries/:id/approve", ae.ApproveBagDelivery)
	r.POST("/bag-deliveries/:id/reject", ae.RejectBagDelivery)
	r.POST("/deliveries/:id/resolve-resume", ae.AdminResolveDeliveryResume)
	r.POST("/deliveries/:id/resolve-reassign", ae.AdminResolveDeliveryReassign)
	r.POST("/deliveries/:id/resolve-cancel", ae.AdminResolveDeliveryCancel)
	r.GET("/inventory-usages", ae.ListInventoryUsages)

	r.GET("/rider/applications", h.ListRiderApplications)
	r.GET("/rider/applications/:id", h.GetRiderApplication)
	r.PATCH("/rider/applications/:id/review", h.ReviewRiderApplication)

	// 骑手管理（收益/结账/撤销）
	r.GET("/riders", h.ListRiders)
	r.GET("/riders/:id", h.GetRider)
	r.PATCH("/riders/:id/revoke", h.RevokeRider)
	r.GET("/riders/:id/earnings", h.ListRiderEarnings)
	r.GET("/riders/:id/deliveries", h.ListRiderDeliveries)
	r.GET("/riders/:id/settlements", h.ListRiderSettlements)
	r.POST("/riders/:id/settlements", h.CreateSettlement)
	r.GET("/settlements", h.ListAllSettlements)
	r.GET("/settlements/pending", h.ListPendingSettlements)
	r.GET("/settlements/pending/count", h.CountPendingSettlements)
	r.PATCH("/settlements/:id/review", h.ReviewSettlement)

	r.GET("/coupons", ch.ListAdmin)
	r.POST("/coupons", ch.CreateAdmin)
	r.GET("/coupons/:id", ch.GetAdmin)
	r.PATCH("/coupons/:id", ch.UpdateAdmin)
	r.PATCH("/coupons/:id/status", ch.UpdateAdminStatus)

	r.GET("/announcements", ah.ListAdmin)
	r.POST("/announcements", ah.CreateAdmin)
	r.PATCH("/announcements/:id", ah.UpdateAdmin)
	r.DELETE("/announcements/:id", ah.DeleteAdmin)

	r.GET("/home-carousel", hc.ListAdmin)
	r.POST("/home-carousel", hc.Create)
	r.PUT("/home-carousel/reorder", hc.Reorder)
	r.PATCH("/home-carousel/:id", hc.Update)
	r.DELETE("/home-carousel/:id", hc.Delete)
}

func registerRiderRoutes(r *gin.RouterGroup, h *handler.RiderHandler) {
	r.GET("/orders", h.ListOrders)
	r.GET("/orders/:id", h.GetDelivery)
	r.POST("/orders/:id/accept", h.AcceptDelivery)
	r.POST("/orders/:id/start", h.StartDelivery)
	r.POST("/orders/:id/exception", h.ReportException)
	r.POST("/orders/:id/complete", h.CompleteDelivery)
	r.GET("/earnings", h.ListEarnings)
	r.GET("/earnings/summary", h.EarningsSummary)
	r.GET("/settlements", h.ListSettlements)
	r.POST("/settlements/request", h.RequestSettlement)
}
