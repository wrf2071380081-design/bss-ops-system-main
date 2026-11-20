DROP INDEX IF EXISTS idx_formal_config_group;
ALTER TABLE supplier_formal_config DROP COLUMN IF EXISTS config_group_id;
