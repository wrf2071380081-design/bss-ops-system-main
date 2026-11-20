-- 000072_customer_quote_channel：一个客户同一模型可以有多条通道（多张报价，各接一个来源）。
--
-- 背景（2026-11-20 业务确认）：领导要的是「多通道并行销售」——同一客户同一模型可以有多个报价，
-- 每个报价对应一条供应来源；审批通过后各自派办理待办，**每个报价就是一个独立事务**，
-- A 单卡住不影响 B 单。
--
-- 旧模型有三处把粒度钉在「客户」上，这里一起换成「报价单」：
--   1. uk_flow_customer_processing 保证一客户同时只有一条进行中流程
--      → 第二张报价审批时必然撞索引，ensureCustomerFlow 静默跳过，新批次无流程可用；
--   2. todo_task 没有流程维度，收件箱按「该客户任意流程的节点完成」判待办完成
--      → 第一批办完就把第二批的待办一起刷成 DONE（静默失败）；
--   3. uk_crl_customer_model 保证一客户一模型只有一条生效来源
--      → 第二个通道完成后写绑定整批回滚。
--
-- 老数据兼容：
--   · customer_quote_id 为 NULL 的历史流程用 COALESCE(...,0) 占位，
--     继续享受「一客户一条进行中流程」的原约束，不需要回填；
--   · todo_task.flow_id 为 NULL 的历史待办，收件箱仍按客户判完成态
--     （见 internal/repo/workbench_inbox.go 的 CUSTOMER_MODELS 分支）。

-- ============================================================
-- 1. 开通流程按「报价单」拆分
-- ============================================================
ALTER TABLE public.onboarding_flow
  ADD COLUMN IF NOT EXISTS customer_quote_id bigint NULL;

COMMENT ON COLUMN public.onboarding_flow.customer_quote_id IS
  '流程对应的客户报价单（customer_quote.id）。同一客户可为不同报价各开一条流程，互不影响；NULL = 手工发起/历史流程。';

CREATE INDEX IF NOT EXISTS idx_flow_customer_quote
  ON public.onboarding_flow(customer_quote_id)
  WHERE customer_quote_id IS NOT NULL;

DROP INDEX IF EXISTS public.uk_flow_customer_processing;

-- 同一客户 + 同一报价单：同时只能有一条进行中流程（原「一客户一条」的按报价版本）。
-- COALESCE 是为了让历史 NULL 行仍互相唯一——PostgreSQL 里 NULL 互不相等，
-- 不占位的话历史手工流程会失去并发保护。
-- IF NOT EXISTS 是 2026-11-20 补的：撞号期间有的库已经把这份内容当 000072 跑过，
-- 记账却停在 72，重跑到这里会因索引已存在而整条迁移失败（并置 dirty）。
CREATE UNIQUE INDEX IF NOT EXISTS uk_flow_customer_quote_processing
  ON public.onboarding_flow(owner_id, COALESCE(customer_quote_id, 0))
  WHERE kind = 'CUSTOMER' AND status = 'PROCESSING';

COMMENT ON INDEX public.uk_flow_customer_quote_processing IS
  '一个客户 + 一张报价单同时只能有一条进行中（PROCESSING）的开通流程；不同报价互不影响。仅约束 kind=CUSTOMER。';

-- ============================================================
-- 2. 待办记住自己属于哪条流程
-- ============================================================
ALTER TABLE public.todo_task
  ADD COLUMN IF NOT EXISTS flow_id bigint NULL;

COMMENT ON COLUMN public.todo_task.flow_id IS
  '待办对应的开通流程（onboarding_flow.id）。收件箱据此按「本流程」判完成态，而不是按客户任意流程；NULL = 历史待办。';

CREATE INDEX IF NOT EXISTS idx_todo_flow
  ON public.todo_task(flow_id)
  WHERE flow_id IS NOT NULL;

-- ============================================================
-- 3. 放开「同客户同模型只能一条生效来源」
-- ============================================================
-- 真正要防的是「同一客户 + 同一资源被重复接入」，那条约束是 uk_crl_customer_resource（保留）。
-- 同模型不同资源 = 多通道，属于业务要支持的正常形态。
DROP INDEX IF EXISTS public.uk_crl_customer_model;
