-- 000039_supplier_submission_draft（down）：移除草稿与乐观锁字段。
-- 注意：回滚会把 submitted_at 恢复为 NOT NULL；若库里已有草稿行，回滚会失败，
-- 需先处理草稿数据。这是有意的保护——避免静默丢掉草稿。
DROP INDEX IF EXISTS idx_submission_supplier_created;

UPDATE submission SET submitted_at = created_at WHERE submitted_at IS NULL;
ALTER TABLE submission ALTER COLUMN submitted_at SET NOT NULL;

ALTER TABLE submission
  DROP COLUMN IF EXISTS updated_at,
  DROP COLUMN IF EXISTS revision;
