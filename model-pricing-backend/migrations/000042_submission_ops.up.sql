-- 000042_submission_ops：申报列表运营字段（原型对齐 Day1）。
--
-- 背景：原型申报记录需要「内部编号」「提交人」「模型数」，
-- 且压力测试改为行内勾选（participate_test），此前该列写多读零。
--
-- 兼容：PostgreSQL 14+（生成列 STORED 自 12 起支持）。

ALTER TABLE submission
  ADD COLUMN IF NOT EXISTS submission_no varchar(32)
    GENERATED ALWAYS AS ('S' || lpad(id::text, 8, '0')) STORED,
  ADD COLUMN IF NOT EXISTS submitter_name varchar(64) NULL,
  ADD COLUMN IF NOT EXISTS submitter_account_id bigint NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uk_submission_no ON submission (submission_no);
CREATE INDEX IF NOT EXISTS idx_submission_supplier_status
  ON submission (supplier_id, status, created_at DESC);

COMMENT ON COLUMN submission.submission_no IS '对外内部编号，形如 S00000003（由 id 生成，无需发号）';
COMMENT ON COLUMN submission.submitter_name IS '提交人姓名快照：提交时写入，不随改名漂移';
COMMENT ON COLUMN submission.submitter_account_id IS '提交账号 account.id，用于按提交人筛选';
COMMENT ON COLUMN submission_row.participate_test IS '压力测试勾选：true=该行参与压力测试（原型「压力测试」列）';

-- 数据修正：旧 AttachTestJob 会把任何测试都置 participate_test=true，
-- 这里把「没勾选、也没跑过压力」的行归位。带 stress_job_id 条件，重复执行安全。
UPDATE submission_row
SET participate_test = false
WHERE participate_test = true AND stress_job_id IS NULL;
