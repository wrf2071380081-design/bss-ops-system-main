-- 000040_submission_row_stress_job：申报行区分「连通」与「压力」两个测试任务。
--
-- 背景：submission_row 只有一个 test_job_id，而前端每行要同时展示
-- connectivity_job 与 stress_job。先测连通再测压力时，压力任务会覆盖连通任务，
-- 导致已完成的连通结果从页面上消失。
--
-- 兼容：PostgreSQL 14+。
ALTER TABLE submission_row
  ADD COLUMN IF NOT EXISTS stress_job_id bigint NULL
    REFERENCES test_job(id);

COMMENT ON COLUMN submission_row.test_job_id IS '连通测试（baseline）任务';
COMMENT ON COLUMN submission_row.stress_job_id IS '压力测试（stress）任务';
