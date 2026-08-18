ALTER TABLE `activity`
  ADD COLUMN `user_daily_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '活动内每人每天最多购买件数（跨商品累计），0=不限' AFTER `user_max_qty`,
  ADD COLUMN `user_daily_refresh_time` TIME NOT NULL DEFAULT '00:00:00' COMMENT '活动每人每天限购刷新时刻' AFTER `user_daily_max`;
