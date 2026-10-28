-- 「发起测试」的重复保护：同一资源 + 同一类别 + 同一受测目标，
-- 同一时刻只允许存在一个排队/运行中的任务。
--
-- 为什么必须落在库上：业务层 shouldSkipRetest 是「读 → 判断 → 再发」，
-- 两个并发请求（连点、多标签页、客户端重试）会同时读到「还没有任务」，
-- 于是各自往测试平台发一单，白烧一轮资源（2026-10-28 审计复现）。
-- 唯一索引把它变成原子判定：后到的那条只会拿到已有任务号，不会真的再发。
--
-- 指纹为空的历史行不参与约束：老数据没有指纹，无法判定是不是「同一个目标」，
-- 强行约束会把正常发起误伤成冲突。
--
-- 建索引前先清理僵尸：本地任务建好、平台调用失败的旧实现把任务留在 QUEUED，
-- 而轮询又因为 external_job_id 为空直接跳过它，于是它永远卡着这个槽位。
UPDATE test_job
SET status = 'FAILED', finished_at = now()
WHERE status IN ('QUEUED', 'RUNNING')
  AND external_job_id IS NULL
  AND created_at < now() - interval '10 minutes';

CREATE UNIQUE INDEX uk_test_job_active_target
    ON test_job (resource_id, category, NULLIF(target_fingerprint, ''))
    WHERE status IN ('QUEUED', 'RUNNING');
