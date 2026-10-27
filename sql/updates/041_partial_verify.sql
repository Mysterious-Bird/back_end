-- 部分核销：使用记录已核销件数 + 核销记录本次件数
ALTER TABLE user_inventory_usage
  ADD COLUMN verified_quantity INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '已核销件数';

ALTER TABLE verification_record
  ADD COLUMN quantity INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '本次核销件数';

-- 历史已完成核销：整条数量视为已核销
UPDATE user_inventory_usage SET verified_quantity = quantity WHERE status = 3 AND verified_quantity = 0;

-- 历史核销记录：回填为当时整条核销的数量
UPDATE verification_record vr
JOIN verification_code vc ON vc.id = vr.verification_code_id AND vc.is_deleted = 0
JOIN user_inventory_usage u ON u.id = vc.inventory_usage_id
SET vr.quantity = u.quantity
WHERE vr.quantity = 0;

UPDATE verification_record SET quantity = 1 WHERE quantity = 0;
