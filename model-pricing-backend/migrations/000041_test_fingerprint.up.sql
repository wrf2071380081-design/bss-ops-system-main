-- 000041_test_fingerprint：测试目标指纹（Q6），用于判断旧报告是否仍有效。
--
-- 口径：标准模型 + 接口模型名 + 测试地址 + 凭据（密文）。
-- 不含采购系数——改价不代表测试目标变了，不应该逼供应商重测。
--
-- submission_row.test_fingerprint 记录"当前行描述的目标"，
-- test_job.target_fingerprint 记录"发单时的目标"；两者不等即报告失效（不删除历史）。
--
-- 兼容：PostgreSQL 14+。
ALTER TABLE submission_row ADD COLUMN IF NOT EXISTS test_fingerprint varchar(64) NULL;
ALTER TABLE test_job ADD COLUMN IF NOT EXISTS target_fingerprint varchar(64) NULL;

COMMENT ON COLUMN submission_row.test_fingerprint IS '当前测试目标指纹（sha256 hex）：标准模型+接口模型名+测试地址+凭据密文';
COMMENT ON COLUMN test_job.target_fingerprint IS '发单时的测试目标指纹；与行上不一致表示报告已失效';
