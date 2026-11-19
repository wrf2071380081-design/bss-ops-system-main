-- 000071: 申报行的连通性探测结果
--
-- 背景（2026-11-19）：提交申报时会对每个模型做一次真实探测
-- （GET {该行配置组的 BaseURL}/models，Bearer 该组 Key）。
-- 之前结果只写日志，供应商在「查看申报」看不到逐行状态、审批也无法按行过滤。
--
-- 语义：
--   NULL      = 未探测过（历史数据，按"视为通过"处理，避免旧单无法审批）
--   PASS      = 探测通过
--   FAIL      = 探测失败，具体原因在 connectivity_note
ALTER TABLE submission_row
    ADD COLUMN IF NOT EXISTS connectivity_status varchar(16),
    ADD COLUMN IF NOT EXISTS connectivity_note   text;

COMMENT ON COLUMN submission_row.connectivity_status IS '连通性探测结果：NULL 未探测 / PASS 通过 / FAIL 未通过';
COMMENT ON COLUMN submission_row.connectivity_note   IS '探测失败原因（人读），PASS 时为空';
