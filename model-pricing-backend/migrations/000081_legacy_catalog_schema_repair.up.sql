-- Forward repair for legacy catalog databases already at version 78.
-- They applied catalog DDL as 77 and never executed canonical schema_drift_repair.
-- Repeat canonical 77 DDL so both fresh and upgraded installations converge.
ALTER TABLE supplier_formal_config
    ADD COLUMN IF NOT EXISTS config_group_id bigint REFERENCES supplier_test_config_group(id);

CREATE INDEX IF NOT EXISTS idx_formal_config_group
    ON supplier_formal_config(supplier_id, region, config_group_id);

COMMENT ON COLUMN supplier_formal_config.config_group_id IS '所属准入配置组；NULL 表示历史数据（未区分组）';

-- ============================================================
-- 2) 迁移 000073_uploaded_report_meta 的结构（上传报告的元数据）
-- ============================================================
ALTER TABLE test_report
    ADD COLUMN IF NOT EXISTS upload_test_type varchar(32),
    ADD COLUMN IF NOT EXISTS upload_test_name varchar(64),
    ADD COLUMN IF NOT EXISTS upload_tested_at timestamptz,
    ADD COLUMN IF NOT EXISTS upload_tester    varchar(64),
    ADD COLUMN IF NOT EXISTS upload_note      text;

-- ============================================================
-- 3) 迁移 000074_customer_quote_channel 的结构（一个报价一个办理事务）
-- ============================================================
ALTER TABLE public.onboarding_flow
  ADD COLUMN IF NOT EXISTS customer_quote_id bigint NULL;

COMMENT ON COLUMN public.onboarding_flow.customer_quote_id IS
  '流程对应的客户报价单（customer_quote.id）。同一客户可为不同报价各开一条流程，互不影响；NULL = 手工发起/历史流程。';

CREATE INDEX IF NOT EXISTS idx_flow_customer_quote
  ON public.onboarding_flow(customer_quote_id)
  WHERE customer_quote_id IS NOT NULL;

-- 旧约束「一客户一条进行中流程」换成「一客户 + 一报价单一条」。
-- COALESCE(...,0) 让 customer_quote_id 为 NULL 的历史行仍然互相唯一
-- ——PostgreSQL 里 NULL 互不相等，不占位的话历史手工流程会失去并发保护。
DROP INDEX IF EXISTS public.uk_flow_customer_processing;

CREATE UNIQUE INDEX IF NOT EXISTS uk_flow_customer_quote_processing
  ON public.onboarding_flow(owner_id, COALESCE(customer_quote_id, 0))
  WHERE kind = 'CUSTOMER' AND status = 'PROCESSING';

COMMENT ON INDEX public.uk_flow_customer_quote_processing IS
  '一个客户 + 一张报价单同时只能有一条进行中（PROCESSING）的开通流程；不同报价互不影响。仅约束 kind=CUSTOMER。';

ALTER TABLE public.todo_task
  ADD COLUMN IF NOT EXISTS flow_id bigint NULL;

COMMENT ON COLUMN public.todo_task.flow_id IS
  '待办对应的开通流程（onboarding_flow.id）。收件箱据此按「本流程」判完成态，而不是按客户任意流程；NULL = 历史待办。';

CREATE INDEX IF NOT EXISTS idx_todo_flow
  ON public.todo_task(flow_id)
  WHERE flow_id IS NOT NULL;

-- 真正要防的是「同一客户 + 同一资源被重复接入」（uk_crl_customer_resource 保留）；
-- 同模型不同资源 = 多通道，属于业务要支持的正常形态。
DROP INDEX IF EXISTS public.uk_crl_customer_model;
