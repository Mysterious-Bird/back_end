ALTER TABLE `activity`
  ADD COLUMN `user_max_qty` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '活动内每人最多购买件数（跨商品累计），0=不限' AFTER `enable_coupon`;
