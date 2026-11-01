-- 跑腿：订单、配置、配送单关联、骑手收益关联
CREATE TABLE IF NOT EXISTS `errand_order` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `order_no` varchar(32) NOT NULL,
  `account_id` bigint unsigned NOT NULL,
  `status` tinyint unsigned NOT NULL DEFAULT '1' COMMENT '1待接单 2已接单 3配送中 4待确认 5已完成 6已取消 7异常',
  `pay_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '0未付 1已付 2退款中 3已退 4部分退',
  `pay_mode` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '0未确定 1平台支付 2线下现金',
  `delivery_fee` decimal(10,2) NOT NULL DEFAULT '1.00',
  `goods_amount` decimal(10,2) NOT NULL DEFAULT '0.00' COMMENT '代购商品费（骑手实报）',
  `pay_amount` decimal(10,2) NOT NULL DEFAULT '0.00' COMMENT '应付=跑腿费+商品费',
  `refunded_amount` decimal(10,2) NOT NULL DEFAULT '0.00',
  `collected_amount` decimal(10,2) NOT NULL DEFAULT '0.00' COMMENT '线下实收',
  `rider_earnings` decimal(10,2) NOT NULL DEFAULT '0.00',
  `platform_commission` decimal(10,2) NOT NULL DEFAULT '0.00',
  `goods_desc` varchar(512) NOT NULL DEFAULT '',
  `goods_photos` json DEFAULT NULL,
  `voucher_code` varchar(128) NOT NULL DEFAULT '' COMMENT '第三方券码',
  `voucher_photos` json DEFAULT NULL,
  `goods_receipt_photos` json DEFAULT NULL COMMENT '代购小票照片',
  `pickup_snapshot` json DEFAULT NULL,
  `drop_snapshot` json DEFAULT NULL,
  `distance_km` decimal(10,2) NOT NULL DEFAULT '0.00',
  `expect_minutes` int NOT NULL DEFAULT '0',
  `delivery_order_id` bigint unsigned DEFAULT NULL,
  `pay_expire_at` datetime DEFAULT NULL,
  `cancel_reason` varchar(256) DEFAULT NULL,
  `exception_reason` varchar(512) DEFAULT NULL,
  `remark` varchar(512) DEFAULT NULL,
  `paid_at` datetime DEFAULT NULL,
  `finished_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` tinyint unsigned NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_errand_order_no` (`order_no`),
  KEY `idx_errand_account` (`account_id`),
  KEY `idx_errand_status` (`status`),
  KEY `idx_errand_created` (`created_at`),
  KEY `idx_errand_updated` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='跑腿单';

CREATE TABLE IF NOT EXISTS `errand_settings` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `delivery_fee` decimal(10,2) NOT NULL DEFAULT '1.00',
  `platform_commission_percent` decimal(5,2) NOT NULL DEFAULT '0.00' COMMENT '平台抽成比例（仅跑腿费）',
  `auto_confirm_hours` int NOT NULL DEFAULT '24',
  `goods_max_amount` decimal(10,2) NOT NULL DEFAULT '5000.00' COMMENT '代购商品费上限',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='跑腿配置';

INSERT INTO `errand_settings` (`id`, `delivery_fee`, `platform_commission_percent`, `auto_confirm_hours`, `goods_max_amount`)
VALUES (1, 1.00, 0.00, 24, 5000.00)
ON DUPLICATE KEY UPDATE `id` = VALUES(`id`);

ALTER TABLE `delivery_order`
  ADD COLUMN `errand_order_id` bigint unsigned DEFAULT NULL,
  ADD INDEX `idx_delivery_errand` (`errand_order_id`);

ALTER TABLE `rider_earning`
  ADD COLUMN `errand_order_id` bigint unsigned DEFAULT NULL,
  ADD INDEX `idx_earning_errand` (`errand_order_id`);
