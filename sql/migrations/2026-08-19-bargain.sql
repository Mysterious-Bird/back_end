-- 砍一刀：活动商品配置列 + 会话/帮砍表
ALTER TABLE activity_product
  ADD COLUMN enable_bargain TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '1=砍价商品' AFTER enable_group_buy,
  ADD COLUMN bargain_floor_price DECIMAL(10,2) NULL DEFAULT NULL COMMENT '砍价底价' AFTER enable_bargain,
  ADD COLUMN bargain_duration_hours INT UNSIGNED NOT NULL DEFAULT 24 COMMENT '会话时长小时' AFTER bargain_floor_price,
  ADD COLUMN bargain_new_user_hours INT UNSIGNED NOT NULL DEFAULT 48 COMMENT '新用户窗口小时' AFTER bargain_duration_hours,
  ADD COLUMN bargain_help_daily_max INT UNSIGNED NOT NULL DEFAULT 20 COMMENT '每账号每日帮砍上限' AFTER bargain_new_user_hours,
  ADD COLUMN bargain_self_cut_max DECIMAL(10,2) NOT NULL DEFAULT 1.00 COMMENT '发起人自砍上限' AFTER bargain_help_daily_max,
  ADD COLUMN bargain_new_min DECIMAL(10,2) NOT NULL DEFAULT 1.00 AFTER bargain_self_cut_max,
  ADD COLUMN bargain_new_max DECIMAL(10,2) NOT NULL DEFAULT 5.00 AFTER bargain_new_min,
  ADD COLUMN bargain_old_min DECIMAL(10,2) NOT NULL DEFAULT 0.10 AFTER bargain_new_max,
  ADD COLUMN bargain_old_max DECIMAL(10,2) NOT NULL DEFAULT 1.00 AFTER bargain_old_min;

CREATE TABLE IF NOT EXISTS bargain_session (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  activity_id BIGINT UNSIGNED NOT NULL,
  activity_product_id BIGINT UNSIGNED NOT NULL,
  product_id BIGINT UNSIGNED NOT NULL,
  merchant_id BIGINT UNSIGNED NOT NULL,
  initiator_account_id BIGINT UNSIGNED NOT NULL,
  origin_price DECIMAL(10,2) NOT NULL,
  floor_price DECIMAL(10,2) NOT NULL,
  current_price DECIMAL(10,2) NOT NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1进行中 2已下单 3已过期 4已取消',
  self_cut_done TINYINT UNSIGNED NOT NULL DEFAULT 0,
  expire_at DATETIME(3) NOT NULL,
  order_id BIGINT UNSIGNED NULL DEFAULT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  is_deleted TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_bargain_ap_initiator (activity_product_id, initiator_account_id, status),
  KEY idx_bargain_expire (status, expire_at),
  KEY idx_bargain_order (order_id),
  KEY idx_bargain_session_is_deleted (is_deleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='砍价会话';

CREATE TABLE IF NOT EXISTS bargain_help (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  session_id BIGINT UNSIGNED NOT NULL,
  helper_account_id BIGINT UNSIGNED NOT NULL,
  cut_amount DECIMAL(10,2) NOT NULL,
  is_new_user TINYINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_bargain_help (session_id, helper_account_id),
  KEY idx_bargain_help_helper_day (helper_account_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='砍价帮砍记录';
