-- 原型「上传专项报告」：外部测试报告没有平台测试任务，需要允许 test_job_id 为空，
-- 并用 source 标明来源（'' 历史数据 / UPLOADED 外部上传）。
ALTER TABLE public.test_report ADD COLUMN IF NOT EXISTS source varchar(16) NOT NULL DEFAULT '';
ALTER TABLE public.test_report ALTER COLUMN test_job_id DROP NOT NULL;

COMMENT ON COLUMN public.test_report.source IS '报告来源：空=平台测试任务，UPLOADED=外部上传的专项报告';
COMMENT ON COLUMN public.test_report.test_job_id IS '平台测试任务；外部上传的专项报告为空';
