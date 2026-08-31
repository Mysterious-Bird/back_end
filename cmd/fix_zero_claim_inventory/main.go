package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/database"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/service"
)

// 修复「已审核通过但未入包」的异常订单：0 元单（砍价砍到底价 / 优惠券抵扣到 0）在下单事务里
// 结算早于订单明细写入，自动审核入包时查不到 order_item 被静默跳过，导致订单显示已通过但
// 背包无商品、无核销码。本工具对这类订单补入包并生成待核销核销码。
//
//	go run ./cmd/fix_zero_claim_inventory -dry-run
//	go run ./cmd/fix_zero_claim_inventory -apply
//	go run ./cmd/fix_zero_claim_inventory -apply -order-id 254   # 只处理指定订单
func main() {
	dryRun := flag.Bool("dry-run", true, "只扫描统计，不写库（默认）")
	apply := flag.Bool("apply", false, "实际写库修复")
	orderID := flag.Uint64("order-id", 0, "只处理指定订单（0=扫描全部异常订单）")
	flag.Parse()
	if *apply {
		*dryRun = false
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	db, err := database.Connect(cfg.DB)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 扫描：已通过待履约 + 无入包流水（order_credit）的订单
	var orders []model.Order
	q := query.NotDeleted(db).Where(
		"status = ? AND merchant_review_stage = ?",
		model.OrderStatusPendingFulfill, model.MerchantReviewApproved,
	)
	if *orderID > 0 {
		q = q.Where("id = ?", *orderID)
	}
	if err := q.Find(&orders).Error; err != nil {
		log.Fatalf("扫描订单失败: %v", err)
	}

	type target struct {
		ID        uint64
		AccountID uint64
		OrderNo   string
		PayAmount float64
	}
	var targets []target
	for _, o := range orders {
		var credited int64
		if err := query.NotDeleted(db.Model(&model.UserInventoryLog{})).
			Where("order_id = ? AND event_type = ?", o.ID, model.InventoryEventOrderCredit).
			Count(&credited).Error; err != nil {
			log.Fatalf("查询订单 %d 流水失败: %v", o.ID, err)
		}
		if credited > 0 {
			continue
		}
		targets = append(targets, target{ID: o.ID, AccountID: o.AccountID, OrderNo: o.OrderNo, PayAmount: o.PayAmount})
	}

	mode := "DRY-RUN"
	if !*dryRun {
		mode = "APPLY"
	}
	fmt.Printf("=== 已通过未入包订单修复 [%s] ===\n", mode)
	if len(targets) == 0 {
		fmt.Println("未发现异常订单")
		os.Exit(0)
	}
	for _, t := range targets {
		fmt.Printf("  order id=%d account=%d pay=%.2f no=%s\n", t.ID, t.AccountID, t.PayAmount, t.OrderNo)
	}
	if *dryRun {
		fmt.Println("（dry-run 未写库，加 -apply 执行修复）")
		os.Exit(0)
	}

	svc := &service.OrderService{
		DB:           db,
		InventorySvc: &service.InventoryService{DB: db},
	}
	for _, t := range targets {
		if err := svc.RepairApprovedWithoutCredit(t.ID); err != nil {
			fmt.Printf("  order %d 修复失败: %v\n", t.ID, err)
			continue
		}
		fmt.Printf("  order %d 已入包并生成核销码\n", t.ID)
	}
	fmt.Println("完成")
}
