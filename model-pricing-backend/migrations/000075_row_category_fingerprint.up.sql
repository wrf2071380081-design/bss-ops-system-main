-- 000075: 申报行按测试门类分别记录目标指纹
--
-- 原来只有一列 test_fingerprint，连通性与压测共用：跑一次压测就把连通性的指纹覆盖，
-- 供应商侧随即显示「需重测 · 测试目标已变更」（2026-11-20 反馈，实际什么都没改）。
ALTER TABLE submission_row
    ADD COLUMN IF NOT EXISTS connectivity_fingerprint varchar(64),
    ADD COLUMN IF NOT EXISTS stress_fingerprint       varchar(64);

COMMENT ON COLUMN submission_row.connectivity_fingerprint IS '连通性测试目标的指纹（BaseURL/Key/模型）';
COMMENT ON COLUMN submission_row.stress_fingerprint       IS '压力测试目标的指纹；与连通性分开，互不覆盖';
