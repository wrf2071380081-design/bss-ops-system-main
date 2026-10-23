-- 000040_submission_row_stress_job（down）：移除压力任务列。
ALTER TABLE submission_row DROP COLUMN IF EXISTS stress_job_id;
