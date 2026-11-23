-- 000076: 记录平台任务中止/失败原因
-- 现象：提交时发的 protocolprobe 任务在平台上 FAILED 且没有报告，页面只显示"未提供结论"，
-- 定位只能靠猜（2026-11-23）。
ALTER TABLE test_job ADD COLUMN IF NOT EXISTS failure_reason text;
