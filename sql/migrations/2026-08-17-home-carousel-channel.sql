-- 首页轮播支持活动直购 / 活动拼团 / 普通拼团深链
ALTER TABLE `home_carousel`
  ADD COLUMN `activity_id` BIGINT UNSIGNED NULL DEFAULT NULL AFTER `product_id`,
  ADD COLUMN `activity_product_id` BIGINT UNSIGNED NULL DEFAULT NULL AFTER `activity_id`,
  ADD COLUMN `channel` VARCHAR(16) NOT NULL DEFAULT 'deal' COMMENT 'deal=直购/团购 group=拼团' AFTER `activity_product_id`;

ALTER TABLE `home_carousel`
  ADD KEY `idx_home_carousel_activity_product` (`activity_product_id`),
  ADD KEY `idx_home_carousel_channel` (`channel`);
