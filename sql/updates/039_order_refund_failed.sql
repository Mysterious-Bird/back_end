-- 订单退款发起失败标记（微信 CreateRefund 失败时置位，管理端可重试）
ALTER TABLE `order`
  ADD COLUMN refund_failed TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '退款发起失败待重试 0否 1是',
  ADD COLUMN refund_fail_reason VARCHAR(191) NULL COMMENT '失败原因',
  ADD COLUMN refund_failed_at DATETIME NULL COMMENT '失败时间';
