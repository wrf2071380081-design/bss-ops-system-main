-- 000052 down：撤掉多配置组。
--
-- 顺序要求：先删 submission_row 的外键列，再 drop 表（否则外键挡住 DROP TABLE）。
-- 刻意不动 supplier_test_credential：本迁移没改它，回滚也不需要恢复它。

ALTER TABLE submission_row DROP COLUMN IF EXISTS test_config_group_id;

DROP INDEX IF EXISTS idx_stcg_supplier;

DROP TABLE IF EXISTS supplier_test_config_group;
