-- 砍价：必须砍到底价才可购买（默认关闭）
ALTER TABLE `activity_product`
  ADD COLUMN `bargain_require_floor` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '1=必须砍到底价才可购买' AFTER `enable_bargain_self_cut`;
