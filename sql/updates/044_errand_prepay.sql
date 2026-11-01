-- 跑腿：改为先付跑腿费再下单
-- 1) 新增商品费线上支付状态（0 未付 / 1 已付 / 2 已退款，复用 pay_status 取值）
-- 2) 状态新增 8=待支付（无需改列类型，仅补充注释）
ALTER TABLE `errand_order`
  ADD COLUMN `goods_pay_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '代购商品费线上支付状态' AFTER `pay_status`,
  MODIFY COLUMN `status` tinyint unsigned NOT NULL DEFAULT '1' COMMENT '1待接单 2已接单 3配送中 4待确认 5已完成 6已取消 7异常 8待支付';
