ALTER TABLE `activity_product`
  ADD COLUMN `weekly_refresh_weekday` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=Mon..7=Sun' AFTER `daily_refresh_time`,
  ADD COLUMN `weekly_refresh_time` TIME NOT NULL DEFAULT '00:00:00' AFTER `weekly_refresh_weekday`,
  ADD COLUMN `monthly_refresh_day` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1-31 clamp EOM' AFTER `weekly_refresh_time`,
  ADD COLUMN `monthly_refresh_time` TIME NOT NULL DEFAULT '00:00:00' AFTER `monthly_refresh_day`;
