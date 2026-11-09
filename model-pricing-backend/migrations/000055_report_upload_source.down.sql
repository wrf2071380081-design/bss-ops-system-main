-- 回滚前先清掉没有测试任务的上传记录，否则 test_job_id 无法恢复 NOT NULL。
DELETE FROM public.test_report WHERE test_job_id IS NULL;
ALTER TABLE public.test_report ALTER COLUMN test_job_id SET NOT NULL;
ALTER TABLE public.test_report DROP COLUMN IF EXISTS source;
