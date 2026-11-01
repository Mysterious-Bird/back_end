-- 跑腿：订单新增总件数（按件计费：2 件以内按基础价，第 3 件起每件加 1 元）
ALTER TABLE `errand_order`
  ADD COLUMN `item_count` int NOT NULL DEFAULT '1' COMMENT '总件数' AFTER `delivery_fee`;
