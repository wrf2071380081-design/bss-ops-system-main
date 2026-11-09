DROP INDEX IF EXISTS idx_supplier_formal_config_status;

ALTER TABLE supplier_formal_config
  DROP CONSTRAINT IF EXISTS ck_supplier_formal_config_status,
  DROP CONSTRAINT IF EXISTS ck_supplier_formal_config_revision,
  DROP CONSTRAINT IF EXISTS ck_supplier_formal_config_region;

ALTER TABLE supplier_formal_config
  DROP COLUMN IF EXISTS region,
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS revision,
  DROP COLUMN IF EXISTS confirmed_by,
  DROP COLUMN IF EXISTS confirmed_at,
  DROP COLUMN IF EXISTS confirmed_revision;
