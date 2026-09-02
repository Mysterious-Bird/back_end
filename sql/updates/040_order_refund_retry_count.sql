-- 退款失败自动重试计数（超过上限后需管理端手动「重新退款」，手动重试会清零）
ALTER TABLE `order`
  ADD COLUMN refund_retry_count TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '退款自动重试次数';
