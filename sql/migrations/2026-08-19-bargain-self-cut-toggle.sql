-- 发起人自砍开关 + 随机/固定砍幅（默认关闭自砍）
ALTER TABLE activity_product
  ADD COLUMN enable_bargain_self_cut TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '1=允许发起人自砍' AFTER bargain_help_daily_max,
  ADD COLUMN bargain_self_cut_mode TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1随机 2固定' AFTER enable_bargain_self_cut,
  ADD COLUMN bargain_self_cut_min DECIMAL(10,2) NOT NULL DEFAULT 0.10 COMMENT '自砍最小/固定金额' AFTER bargain_self_cut_mode;
