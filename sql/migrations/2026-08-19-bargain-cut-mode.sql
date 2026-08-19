-- 砍价：新/老用户砍幅支持随机区间或固定金额
ALTER TABLE activity_product
  ADD COLUMN bargain_new_cut_mode TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=随机区间 2=固定金额' AFTER bargain_help_daily_max,
  ADD COLUMN bargain_old_cut_mode TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=随机区间 2=固定金额' AFTER bargain_new_max;
