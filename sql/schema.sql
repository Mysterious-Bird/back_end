-- =============================================================================
-- 豫记信疆 · 生产基线 schema（空库一键建表）
-- =============================================================================
-- 本文件已包含 sql/updates/012–038 以及 migrations/ 下全部变更的最终形态
-- （含 20260802 购物车规格、20260803 适用店/usage_merchant_id）。
-- 新环境只需执行本文件一次，无需再跑历史 ALTER / updates / migrations。
--
-- 软删除统一使用 is_deleted（0=正常，1=已删除），无 deleted_at。
-- 字符集 utf8mb4，引擎 InnoDB。主键与列类型对齐 internal/model/*.go（GORM tags）。
-- =============================================================================

CREATE DATABASE IF NOT EXISTS `yujixinjiang`
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

USE `yujixinjiang`;

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- -----------------------------------------------------------------------------
-- account 账号
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `account` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `type` TINYINT UNSIGNED NOT NULL COMMENT '1用户 2商家 3管理员（4骑手类型已废弃，见 is_rider）',
  `openid` VARCHAR(64) NULL DEFAULT NULL,
  `unionid` VARCHAR(64) NULL DEFAULT NULL,
  `phone` VARCHAR(20) NULL DEFAULT NULL,
  `email` VARCHAR(128) NULL DEFAULT NULL,
  `password_hash` VARCHAR(255) NULL DEFAULT NULL,
  `nickname` VARCHAR(64) NULL DEFAULT NULL,
  `avatar_url` VARCHAR(512) NULL DEFAULT NULL,
  `gender` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0禁用 1正常',
  `is_rider` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0否 1是骑手',
  `last_login_at` DATETIME NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_account_openid` (`openid`),
  KEY `idx_account_phone` (`phone`),
  KEY `idx_account_type_status` (`type`, `status`),
  KEY `idx_account_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='账号';

-- -----------------------------------------------------------------------------
-- user_profile 用户资料
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `user_profile` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `real_name` VARCHAR(32) NULL DEFAULT NULL,
  `birthday` DATE NULL DEFAULT NULL,
  `bio` VARCHAR(256) NULL DEFAULT NULL,
  `province` VARCHAR(32) NULL DEFAULT NULL,
  `city` VARCHAR(32) NULL DEFAULT NULL,
  `district` VARCHAR(32) NULL DEFAULT NULL,
  `points` INT UNSIGNED NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_user_profile_account` (`account_id`),
  KEY `idx_user_profile_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户资料';

-- -----------------------------------------------------------------------------
-- user_address 收货地址
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `user_address` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `contact_name` VARCHAR(32) NOT NULL,
  `contact_phone` VARCHAR(20) NOT NULL,
  `province` VARCHAR(32) NOT NULL,
  `city` VARCHAR(32) NOT NULL,
  `district` VARCHAR(32) NOT NULL,
  `detail` VARCHAR(256) NOT NULL,
  `latitude` DECIMAL(10,7) NULL DEFAULT NULL,
  `longitude` DECIMAL(10,7) NULL DEFAULT NULL,
  `location_name` VARCHAR(128) NULL DEFAULT NULL,
  `is_default` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_user_address_account` (`account_id`),
  KEY `idx_user_address_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户地址';

-- -----------------------------------------------------------------------------
-- merchant_profile 商家资料
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `merchant_profile` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `shop_name` VARCHAR(128) NOT NULL,
  `shop_logo` VARCHAR(512) NULL DEFAULT NULL,
  `images` JSON NULL,
  `contact_phone` VARCHAR(20) NULL DEFAULT NULL,
  `address` VARCHAR(256) NULL DEFAULT NULL,
  `latitude` DECIMAL(10,7) NULL DEFAULT NULL,
  `longitude` DECIMAL(10,7) NULL DEFAULT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0打烊 1营业',
  `allow_reservation` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0不可订座 1可订座',
  `auto_approve` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '1=支付后自动审核入背包',
  `open_time` TIME NULL DEFAULT NULL COMMENT '营业开始，NULL=未设置',
  `close_time` TIME NULL DEFAULT NULL COMMENT '营业结束，NULL=未设置',
  `delivery_fee` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '向用户收取的配送费',
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '骑手每单收益',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_merchant_profile_account` (`account_id`),
  KEY `idx_merchant_profile_status` (`status`),
  KEY `idx_merchant_profile_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商家资料';

-- -----------------------------------------------------------------------------
-- product_category 商品分类
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `product_category` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `parent_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `name` VARCHAR(64) NOT NULL,
  `icon_url` VARCHAR(512) NULL DEFAULT NULL,
  `sort_order` INT NOT NULL DEFAULT 0,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_product_category_merchant` (`merchant_id`),
  KEY `idx_product_category_parent` (`parent_id`),
  KEY `idx_product_category_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品分类';

-- -----------------------------------------------------------------------------
-- product 商品（含三通道库存 / 自取配送开关 / 拼团配置）
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `product` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `category_id` BIGINT UNSIGNED NOT NULL,
  `name` VARCHAR(128) NOT NULL,
  `description` TEXT NULL,
  `cover_url` VARCHAR(512) NOT NULL,
  `images` JSON NULL,
  `price` DECIMAL(10,2) NOT NULL,
  `original_price` DECIMAL(10,2) NULL DEFAULT NULL,
  `stock` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '遗留总库存字段',
  `enable_deal` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '特价/常规通道',
  `enable_group` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '拼团通道',
  `enable_takeout` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '外卖通道',
  `deal_stock` INT UNSIGNED NOT NULL DEFAULT 0,
  `group_stock` INT UNSIGNED NOT NULL DEFAULT 0,
  `takeout_stock` INT UNSIGNED NOT NULL DEFAULT 0,
  `sales_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `is_hot` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `enable_group_buy` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `enable_coupon` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `allow_pickup` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0不支持自取 1支持',
  `allow_delivery` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0不支持配送 1支持',
  `group_buy_target_count` INT UNSIGNED NULL DEFAULT NULL,
  `group_buy_price` DECIMAL(10,2) NULL DEFAULT NULL,
  `group_buy_allow_repeat` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '同团是否允许同一用户多笔',
  `group_buy_max_concurrent_teams` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '同时进行中团数上限，0=不限',
  `deal_expire_days` INT UNSIGNED NULL DEFAULT NULL COMMENT '团购入待核销后过期天数，NULL/0=永不',
  `group_expire_days` INT UNSIGNED NULL DEFAULT NULL COMMENT '拼团入待核销后过期天数，NULL/0=永不',
  `item_type` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1实物 2虚拟 3套餐',
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0下架 1上架',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_product_merchant` (`merchant_id`),
  KEY `idx_product_category` (`category_id`),
  KEY `idx_product_status` (`status`),
  KEY `idx_product_item_type` (`item_type`),
  KEY `idx_product_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品';

-- -----------------------------------------------------------------------------
-- product_applicable_merchant 商品适用店面（多店核销/履约）
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `product_applicable_merchant` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_product_merchant` (`product_id`, `merchant_id`),
  KEY `idx_merchant_product` (`merchant_id`, `product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品适用店面';

-- -----------------------------------------------------------------------------
-- product_option_group / product_option_item 商品规格选项
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `product_option_group` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `title` VARCHAR(64) NOT NULL,
  `sort_order` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_pog_product` (`product_id`, `is_deleted`, `sort_order`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品规格组';

CREATE TABLE IF NOT EXISTS `product_option_item` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `group_id` BIGINT UNSIGNED NOT NULL,
  `label` VARCHAR(64) NOT NULL,
  `sort_order` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_poi_group` (`group_id`, `is_deleted`, `sort_order`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品规格项';

-- -----------------------------------------------------------------------------
-- product_package_group / product_package_item 套餐分组与候选
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `product_package_group` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `package_product_id` BIGINT UNSIGNED NOT NULL COMMENT '套餐商品ID',
  `name` VARCHAR(64) NOT NULL DEFAULT '',
  `group_type` TINYINT UNSIGNED NOT NULL DEFAULT 2 COMMENT '1=固定包含 2=可选N选M',
  `select_count` INT UNSIGNED NOT NULL DEFAULT 1 COMMENT '组内须选总份数',
  `sort_order` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_pkg_group_product` (`package_product_id`),
  KEY `idx_product_package_group_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='套餐分组';

CREATE TABLE IF NOT EXISTS `product_package_item` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `group_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `max_qty` INT UNSIGNED NOT NULL DEFAULT 1,
  `sort_order` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_pkg_item_group` (`group_id`),
  KEY `idx_pkg_item_product` (`product_id`),
  KEY `idx_product_package_item_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='套餐分组候选商品';

-- -----------------------------------------------------------------------------
-- merchant_delivery_zone 商家配送范围
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `merchant_delivery_zone` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `enabled` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `mode` VARCHAR(16) NOT NULL DEFAULT 'polygon' COMMENT 'polygon|spots',
  `points` JSON NOT NULL COMMENT '多边形顶点',
  `spots` JSON NULL COMMENT '配送点+半径 [{name,latitude,longitude,radius_m}]',
  `poi_landmarks` JSON NULL COMMENT '地标快照',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_merchant_delivery_zone_merchant` (`merchant_id`),
  KEY `idx_merchant_delivery_zone_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商家配送范围';

-- -----------------------------------------------------------------------------
-- activity / activity_product 活动与活动商品
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `activity` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `name` VARCHAR(128) NOT NULL,
  `description` TEXT NULL,
  `cover_url` VARCHAR(512) NULL DEFAULT NULL,
  `banner_images` JSON NULL,
  `start_at` DATETIME NOT NULL,
  `end_at` DATETIME NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 2 COMMENT '0下架 1上架 2草稿',
  `enable_coupon` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `user_max_qty` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '活动内每人最多购买件数（跨商品累计），0=不限',
  `user_daily_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '活动内每人每天最多购买件数（跨商品累计），0=不限',
  `user_daily_refresh_time` TIME NOT NULL DEFAULT '00:00:00' COMMENT '活动每人每天限购刷新时刻',
  `sort_order` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_activity_merchant` (`merchant_id`),
  KEY `idx_activity_status_time` (`status`, `start_at`, `end_at`),
  KEY `idx_activity_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='活动';

CREATE TABLE IF NOT EXISTS `activity_product` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `activity_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `activity_price` DECIMAL(10,2) NOT NULL,
  `activity_stock` INT UNSIGNED NOT NULL DEFAULT 0,
  `sold_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `per_user_max_qty` INT UNSIGNED NOT NULL DEFAULT 0,
  `per_user_max_orders` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'legacy 全程限购',
  `daily_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '每天限购单数，0=关闭',
  `weekly_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '每周限购单数，0=关闭',
  `monthly_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '每月限购单数，0=关闭',
  `activity_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '活动全程限购单数，0=关闭',
  `register_hours` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '注册后有效小时，0=关闭',
  `register_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '新用户窗内限购单数',
  `platform_daily_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '平台每日限购件数，0=不启用',
  `daily_refresh_time` TIME NOT NULL DEFAULT '00:00:00' COMMENT '平台日限刷新时刻',
  `weekly_refresh_weekday` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=Mon..7=Sun',
  `weekly_refresh_time` TIME NOT NULL DEFAULT '00:00:00',
  `monthly_refresh_day` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1-31 clamp EOM',
  `monthly_refresh_time` TIME NOT NULL DEFAULT '00:00:00',
  `platform_daily_sold` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '当前刷新周期已售',
  `platform_daily_bucket` VARCHAR(32) NOT NULL DEFAULT '' COMMENT '当前刷新周期桶 YYYY-MM-DD',
  `enable_group_buy` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `group_buy_price` DECIMAL(10,2) NULL DEFAULT NULL,
  `group_buy_target_count` INT UNSIGNED NULL DEFAULT NULL,
  `group_buy_allow_repeat` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `group_buy_max_joins_per_user` INT UNSIGNED NOT NULL DEFAULT 1,
  `group_buy_max_concurrent_teams` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '同时进行中团数上限，0=不限',
  `expire_days` INT UNSIGNED NULL DEFAULT NULL COMMENT '覆盖商品过期；NULL=沿用商品对应通道',
  `enable_coupon` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `sort_order` INT NOT NULL DEFAULT 0,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_activity_product_activity` (`activity_id`),
  -- 非唯一：同一活动允许同一 product_id 多条活动商品（不同价/通道）
  KEY `idx_activity_product_product` (`product_id`),
  KEY `idx_activity_product_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='活动商品';

-- -----------------------------------------------------------------------------
-- coupon / user_coupon 优惠券
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `coupon` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` VARCHAR(64) NOT NULL,
  `type` TINYINT UNSIGNED NOT NULL COMMENT '1满减 2折扣',
  `merchant_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `min_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `discount_amount` DECIMAL(10,2) NULL DEFAULT NULL,
  `discount_rate` TINYINT UNSIGNED NULL DEFAULT NULL,
  `max_discount` DECIMAL(10,2) NULL DEFAULT NULL,
  `total_quota` INT UNSIGNED NOT NULL DEFAULT 0,
  `received_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `scope_type` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0全部 1分类 2商品',
  `scope_ids` JSON NULL,
  `start_at` DATETIME NOT NULL,
  `end_at` DATETIME NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_coupon_merchant` (`merchant_id`),
  KEY `idx_coupon_status` (`status`),
  KEY `idx_coupon_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='优惠券模板';

CREATE TABLE IF NOT EXISTS `user_coupon` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `coupon_id` BIGINT UNSIGNED NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0未用 1已用 2过期',
  `order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `received_at` DATETIME NOT NULL,
  `used_at` DATETIME NULL DEFAULT NULL,
  `expired_at` DATETIME NOT NULL,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_user_coupon_account` (`account_id`),
  KEY `idx_user_coupon_coupon` (`coupon_id`),
  KEY `idx_user_coupon_status` (`status`),
  KEY `idx_user_coupon_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户优惠券';

-- -----------------------------------------------------------------------------
-- cart_item 购物车
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `cart_item` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `purchase_type` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1单买 2拼团',
  `group_buy_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `group_buy_team_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `spec` VARCHAR(128) NULL DEFAULT NULL,
  `option_selections` JSON NULL COMMENT '规格选配 JSON',
  `option_text` VARCHAR(512) NULL COMMENT '规格摘要文案',
  `option_key` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '规格合并键',
  `quantity` INT UNSIGNED NOT NULL DEFAULT 1,
  `selected` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_cart_item_account` (`account_id`),
  KEY `idx_cart_item_product` (`product_id`),
  KEY `idx_cart_option_key` (`account_id`, `product_id`, `purchase_type`, `option_key`),
  KEY `idx_cart_item_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='购物车';

-- -----------------------------------------------------------------------------
-- group_buy / group_buy_team / group_buy_member 拼团
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `group_buy` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `target_count` INT UNSIGNED NOT NULL,
  `group_price` DECIMAL(10,2) NOT NULL,
  `start_at` DATETIME NOT NULL,
  `end_at` DATETIME NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_group_buy_product` (`product_id`),
  KEY `idx_group_buy_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='拼团活动配置';

CREATE TABLE IF NOT EXISTS `group_buy_team` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `group_buy_id` BIGINT UNSIGNED NOT NULL,
  `leader_id` BIGINT UNSIGNED NOT NULL,
  `target_count` INT UNSIGNED NOT NULL,
  `current_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0进行中 1成功 2失败',
  `expire_at` DATETIME NOT NULL,
  `success_at` DATETIME NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_group_buy_team_group` (`group_buy_id`),
  KEY `idx_group_buy_team_status` (`status`, `expire_at`),
  KEY `idx_group_buy_team_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='拼团团实例';

CREATE TABLE IF NOT EXISTS `group_buy_member` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `team_id` BIGINT UNSIGNED NOT NULL,
  `order_id` BIGINT UNSIGNED NOT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `is_leader` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `joined_at` DATETIME NOT NULL,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_team_order` (`team_id`, `order_id`),
  KEY `idx_team_account` (`team_id`, `account_id`),
  KEY `idx_group_buy_member_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='拼团成员（同团可多单）';

-- -----------------------------------------------------------------------------
-- order / order_item 订单（含套餐父子单、退款累计、配送费快照）
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `order` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `parent_order_id` BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '父订单ID（套餐子单）',
  `order_no` VARCHAR(32) NOT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `usage_merchant_id` BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '实际使用店',
  `package_product_id` BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '套餐商品ID（父单）',
  `activity_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `merchant_review_stage` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `delivery_type` TINYINT UNSIGNED NOT NULL COMMENT '1自取 2配送',
  `address_snapshot` JSON NULL,
  `total_amount` DECIMAL(10,2) NOT NULL,
  `discount_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `user_coupon_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `pay_amount` DECIMAL(10,2) NOT NULL,
  `refunded_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '已退款金额',
  `refund_pending_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '退款中预留金额',
  `delivery_fee` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '下单时配送费快照',
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '下单时骑手收益快照',
  `pay_status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `paid_at` DATETIME NULL DEFAULT NULL,
  `pay_expire_at` DATETIME NULL DEFAULT NULL COMMENT '支付超时时间',
  `prepay_id` VARCHAR(64) NULL DEFAULT NULL COMMENT '微信预支付ID',
  `remark` VARCHAR(256) NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_order_no` (`order_no`),
  KEY `idx_order_parent` (`parent_order_id`),
  KEY `idx_order_package_product` (`package_product_id`),
  KEY `idx_order_account` (`account_id`),
  KEY `idx_order_merchant_status` (`merchant_id`, `status`),
  KEY `idx_order_pay_status_expire` (`pay_status`, `pay_expire_at`),
  KEY `idx_order_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订单';

CREATE TABLE IF NOT EXISTS `order_item` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `activity_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `activity_product_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `platform_daily_bucket` VARCHAR(32) NULL DEFAULT NULL COMMENT '下单时平台日限桶',
  `purchase_type` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `group_buy_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `group_buy_team_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `product_name` VARCHAR(128) NOT NULL,
  `product_image` VARCHAR(512) NULL DEFAULT NULL,
  `spec` VARCHAR(128) NULL DEFAULT NULL,
  `unit_price` DECIMAL(10,2) NOT NULL,
  `quantity` INT UNSIGNED NOT NULL,
  `subtotal` DECIMAL(10,2) NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_order_item_order` (`order_id`),
  KEY `idx_order_item_product` (`product_id`),
  KEY `idx_order_item_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订单明细';

-- -----------------------------------------------------------------------------
-- payment_transaction 支付流水（无 order 外键，支持 takeout/delivery_fee）
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `payment_transaction` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `subject_type` VARCHAR(32) NOT NULL DEFAULT 'order' COMMENT 'order|takeout|delivery_fee',
  `subject_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `order_id` BIGINT UNSIGNED NOT NULL COMMENT '关联业务单ID；外卖/配送费可为0',
  `order_no` VARCHAR(32) NOT NULL COMMENT '业务订单号=微信 out_trade_no',
  `prepay_id` VARCHAR(64) NULL DEFAULT NULL,
  `transaction_id` VARCHAR(64) NULL DEFAULT NULL COMMENT '微信支付订单号',
  `pay_amount` DECIMAL(10,2) NOT NULL,
  `status` TINYINT NOT NULL DEFAULT 0 COMMENT '0预支付 1已支付 2已退款 3失败',
  `wechat_raw` JSON NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_prepay_id` (`prepay_id`),
  UNIQUE KEY `uk_transaction_id` (`transaction_id`),
  KEY `idx_order` (`order_id`),
  KEY `idx_order_no` (`order_no`),
  KEY `idx_pt_subject` (`subject_type`, `subject_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='支付流水';

CREATE TABLE IF NOT EXISTS `payment_refund` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_no` VARCHAR(32) NOT NULL COMMENT '业务订单号=微信 out_trade_no',
  `out_refund_no` VARCHAR(64) NOT NULL COMMENT '商户退款单号',
  `refund_id` VARCHAR(64) NOT NULL COMMENT '微信退款单号',
  `subject_type` VARCHAR(32) NOT NULL DEFAULT 'order' COMMENT 'order|takeout|delivery_fee',
  `subject_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `refund_amount` DECIMAL(10,2) NOT NULL COMMENT '本次退款金额（元）',
  `status` TINYINT NOT NULL DEFAULT 1 COMMENT '1=成功',
  `wechat_raw` JSON NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_refund_id` (`refund_id`),
  UNIQUE KEY `uk_out_refund_no` (`out_refund_no`),
  KEY `idx_pr_order_no` (`order_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='退款回调幂等';

-- -----------------------------------------------------------------------------
-- user_inventory / user_inventory_log / user_inventory_usage 背包
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `user_inventory` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `spec` VARCHAR(128) NOT NULL DEFAULT '',
  `quantity` INT UNSIGNED NOT NULL DEFAULT 0,
  `last_order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_user_inventory_account` (`account_id`),
  KEY `idx_user_inventory_product` (`product_id`),
  KEY `idx_user_inventory_account_product` (`account_id`, `product_id`, `spec`),
  KEY `idx_user_inventory_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户背包库存';

CREATE TABLE IF NOT EXISTS `user_inventory_log` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `inventory_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `spec` VARCHAR(128) NOT NULL DEFAULT '',
  `order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `usage_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `event_type` VARCHAR(32) NOT NULL,
  `delta_qty` INT NOT NULL,
  `before_qty` INT UNSIGNED NOT NULL DEFAULT 0,
  `after_qty` INT UNSIGNED NOT NULL DEFAULT 0,
  `remark` VARCHAR(256) NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_uil_account` (`account_id`),
  KEY `idx_uil_order` (`order_id`),
  KEY `idx_uil_usage` (`usage_id`),
  KEY `idx_uil_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='背包流水';

CREATE TABLE IF NOT EXISTS `user_inventory_usage` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `inventory_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `usage_merchant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '实际使用店',
  `source_order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `quantity` INT UNSIGNED NOT NULL,
  `delivery_type` TINYINT UNSIGNED NOT NULL COMMENT '1自取 2配送',
  `address_snapshot` JSON NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `delivery_order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `cancel_reason` VARCHAR(256) NULL DEFAULT NULL,
  `remark` VARCHAR(256) NULL DEFAULT NULL,
  `package_selections` JSON NULL COMMENT '套餐选配快照',
  `package_select_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0不适用 1待选配 2已确认 3用户已选',
  `option_selections` JSON NULL COMMENT '规格选配快照',
  `option_select_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0无需 1待选 2已选',
  `expire_at` DATETIME NULL DEFAULT NULL COMMENT '待核销过期时间快照，NULL=永不',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_uiu_account` (`account_id`),
  KEY `idx_uiu_inventory` (`inventory_id`),
  KEY `idx_uiu_merchant_status` (`merchant_id`, `status`),
  KEY `idx_usage_package_select` (`merchant_id`, `package_select_status`, `status`),
  KEY `idx_uiu_delivery_order` (`delivery_order_id`),
  KEY `idx_uiu_expire_at` (`status`, `delivery_type`, `expire_at`),
  KEY `idx_uiu_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='背包使用记录';

-- -----------------------------------------------------------------------------
-- takeout_order / takeout_order_item 外卖主单
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `takeout_order` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_no` VARCHAR(32) NOT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `usage_merchant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '实际使用店',
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0待支付 1配餐中 2待骑手/配送中 3已完成 8已取消',
  `goods_amount` DECIMAL(10,2) NOT NULL,
  `delivery_fee` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `pay_amount` DECIMAL(10,2) NOT NULL,
  `pay_status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `paid_at` DATETIME NULL DEFAULT NULL,
  `pay_expire_at` DATETIME NULL DEFAULT NULL,
  `refunded_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `address_snapshot` JSON NULL,
  `delivery_time_remark` VARCHAR(128) NOT NULL DEFAULT '',
  `package_selections` JSON NULL,
  `option_selections` JSON NULL,
  `delivery_order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `remark` VARCHAR(512) NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_takeout_order_no` (`order_no`),
  KEY `idx_takeout_account` (`account_id`),
  KEY `idx_takeout_merchant_status` (`merchant_id`, `status`),
  KEY `idx_takeout_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='外卖订单';

CREATE TABLE IF NOT EXISTS `takeout_order_item` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `takeout_order_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `product_name` VARCHAR(128) NOT NULL,
  `product_image` VARCHAR(512) NULL DEFAULT NULL,
  `unit_price` DECIMAL(10,2) NOT NULL,
  `quantity` INT UNSIGNED NOT NULL,
  `subtotal` DECIMAL(10,2) NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_toi_order` (`takeout_order_id`),
  KEY `idx_toi_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='外卖订单明细';

-- -----------------------------------------------------------------------------
-- delivery_fee_order 背包跑腿配送费预支付单
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `delivery_fee_order` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_no` VARCHAR(32) NOT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending_pay 1=fulfilled 8=cancelled',
  `amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `pay_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `pay_status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `paid_at` DATETIME NULL DEFAULT NULL,
  `pay_expire_at` DATETIME NULL DEFAULT NULL,
  `refunded_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `payload` JSON NULL COMMENT 'draft use-batch input',
  `delivery_order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_delivery_fee_order_no` (`order_no`),
  KEY `idx_delivery_fee_account` (`account_id`),
  KEY `idx_delivery_fee_delivery_order` (`delivery_order_id`),
  KEY `idx_delivery_fee_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='配送费预支付单';

-- -----------------------------------------------------------------------------
-- delivery_order 配送单
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `delivery_order` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `inventory_usage_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `takeout_order_id` BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '外卖主单',
  `rider_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `user_confirmed` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `accepted_at` DATETIME NULL DEFAULT NULL,
  `started_at` DATETIME NULL DEFAULT NULL,
  `delivered_at` DATETIME NULL DEFAULT NULL,
  `deliver_remark` VARCHAR(512) NULL DEFAULT NULL,
  `deliver_photos` JSON NULL,
  `delivery_fee` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '下单时配送费快照',
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '下单时骑手收益快照',
  `pickup_code` VARCHAR(8) NULL DEFAULT NULL COMMENT '出餐号',
  `merchant_prepared` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0备餐中 1已出餐',
  `prepared_at` DATETIME NULL DEFAULT NULL,
  `exception_reason` VARCHAR(512) NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_delivery_order_id` (`order_id`),
  KEY `idx_delivery_usage` (`inventory_usage_id`),
  KEY `idx_delivery_takeout` (`takeout_order_id`),
  KEY `idx_delivery_rider_status` (`rider_id`, `status`),
  KEY `idx_pickup_code` (`pickup_code`),
  KEY `idx_delivery_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='配送单';

-- -----------------------------------------------------------------------------
-- rider_earning / rider_settlement 骑手收益与结账
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `rider_earning` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `rider_id` BIGINT UNSIGNED NOT NULL COMMENT '骑手 account_id',
  `delivery_order_id` BIGINT UNSIGNED NOT NULL,
  `order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `amount` DECIMAL(10,2) NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0待结账 1已结账 2已取消',
  `settlement_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL,
  `settled_at` DATETIME NULL DEFAULT NULL,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_rider_status` (`rider_id`, `status`),
  KEY `idx_delivery` (`delivery_order_id`),
  KEY `idx_settlement` (`settlement_id`),
  KEY `idx_rider_earning_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='骑手收益记录';

CREATE TABLE IF NOT EXISTS `rider_settlement` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `rider_id` BIGINT UNSIGNED NOT NULL,
  `amount` DECIMAL(10,2) NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0待审批 1通过 2拒绝',
  `source` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0骑手申请 1管理员主动',
  `operator_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `applicant_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `reviewed_at` DATETIME NULL DEFAULT NULL,
  `reject_reason` VARCHAR(256) NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL,
  `updated_at` DATETIME NOT NULL,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_rider_status` (`rider_id`, `status`),
  KEY `idx_status` (`status`),
  KEY `idx_rider_settlement_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='骑手结账/提现';

-- -----------------------------------------------------------------------------
-- rider_application 骑手申请
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `rider_application` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `real_name` VARCHAR(32) NOT NULL,
  `id_card_no` VARCHAR(32) NULL DEFAULT NULL,
  `phone` VARCHAR(20) NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0待审 1通过 2拒绝',
  `reviewer_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `reviewed_at` DATETIME NULL DEFAULT NULL,
  `reject_reason` VARCHAR(256) NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_rider_application_account` (`account_id`),
  KEY `idx_rider_application_status` (`status`),
  KEY `idx_rider_application_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='骑手申请';

-- -----------------------------------------------------------------------------
-- verification_code / verification_record 核销
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `verification_code` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `inventory_usage_id` BIGINT UNSIGNED NULL DEFAULT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `code` VARCHAR(32) NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0未用 1已用 2过期',
  `expired_at` DATETIME NULL DEFAULT NULL,
  `used_at` DATETIME NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_verification_code_code` (`code`),
  KEY `idx_verification_code_order` (`order_id`),
  KEY `idx_verification_code_usage` (`inventory_usage_id`),
  KEY `idx_verification_code_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='核销码';

CREATE TABLE IF NOT EXISTS `verification_record` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `verification_code_id` BIGINT UNSIGNED NOT NULL,
  `order_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `operator_id` BIGINT UNSIGNED NOT NULL,
  `verified_at` DATETIME NOT NULL,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_verification_record_code` (`verification_code_id`),
  KEY `idx_verification_record_merchant` (`merchant_id`),
  KEY `idx_verification_record_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='核销记录';

-- -----------------------------------------------------------------------------
-- announcement 公告
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `announcement` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=平台公告',
  `title` VARCHAR(128) NOT NULL,
  `content` TEXT NOT NULL,
  `cover_url` VARCHAR(512) NULL DEFAULT NULL,
  `sort_order` INT NOT NULL DEFAULT 0,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0隐藏 1发布',
  `publish_at` DATETIME NULL DEFAULT NULL,
  `expire_at` DATETIME NULL DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_announcement_merchant` (`merchant_id`),
  KEY `idx_announcement_status` (`status`),
  KEY `idx_announcement_is_deleted` (`is_deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='公告';

-- -----------------------------------------------------------------------------
-- fulfillment_event 履约时间线
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `fulfillment_event` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `subject_type` VARCHAR(32) NOT NULL COMMENT 'order|takeout|delivery|usage|delivery_fee',
  `subject_id` BIGINT UNSIGNED NOT NULL,
  `event_code` VARCHAR(64) NOT NULL,
  `actor_role` VARCHAR(16) NOT NULL DEFAULT 'system',
  `actor_id` BIGINT UNSIGNED NULL,
  `title` VARCHAR(128) NOT NULL,
  `detail` JSON NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_fe_subject` (`subject_type`, `subject_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='履约事件时间线';

-- -----------------------------------------------------------------------------
-- home_carousel 首页商品轮播
-- -----------------------------------------------------------------------------
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='首页商品轮播';

SET FOREIGN_KEY_CHECKS = 1;
