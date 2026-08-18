CREATE TABLE IF NOT EXISTS `home_carousel` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `activity_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `activity_product_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `channel` VARCHAR(16) NOT NULL DEFAULT 'deal' COMMENT 'deal=直购/团购 group=拼团',
  `sort_order` INT NOT NULL DEFAULT 0,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0=停用 1=启用',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_home_carousel_product` (`product_id`),
  KEY `idx_home_carousel_activity_product` (`activity_product_id`),
  KEY `idx_home_carousel_channel` (`channel`),
  KEY `idx_home_carousel_status_sort` (`status`, `sort_order`),
  KEY `idx_home_carousel_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='首页商品轮播';
