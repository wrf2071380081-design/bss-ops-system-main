-- 000042_submission_ops（down）：移除运营字段。
-- 注意：participate_test 的数据修正不回滚（原始脏数据无保留价值，回滚反而会制造脏数据）。
DROP INDEX IF EXISTS idx_submission_supplier_status;
DROP INDEX IF EXISTS uk_submission_no;

ALTER TABLE submission
  DROP COLUMN IF EXISTS submitter_account_id,
  DROP COLUMN IF EXISTS submitter_name,
  DROP COLUMN IF EXISTS submission_no;
