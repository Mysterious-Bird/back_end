-- 砍价全局设置（单行 id=1）：每人每天帮砍上限 + 刷新时刻
CREATE TABLE IF NOT EXISTS bargain_settings (
  id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
  help_daily_max INT UNSIGNED NOT NULL DEFAULT 20 COMMENT '每账号每个刷新周期内帮砍上限',
  help_daily_refresh_time TIME NOT NULL DEFAULT '00:00:00' COMMENT '帮砍次数刷新时刻',
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='砍价全局设置';

INSERT INTO bargain_settings (id, help_daily_max, help_daily_refresh_time)
VALUES (1, 20, '00:00:00')
ON DUPLICATE KEY UPDATE id = id;
