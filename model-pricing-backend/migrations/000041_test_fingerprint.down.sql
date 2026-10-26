-- 000041_test_fingerprint（down）：移除测试目标指纹列。
ALTER TABLE test_job DROP COLUMN IF EXISTS target_fingerprint;
ALTER TABLE submission_row DROP COLUMN IF EXISTS test_fingerprint;
