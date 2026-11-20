-- 000075_schema_drift_repair：把 72/73/74 的结构再补齐一次（幂等）。
--
-- 为什么需要这条"补丁迁移"（2026-11-20）：
--   golang-migrate 只按数字记账，不存文件校验和。撞号期间（两个分支都取了
--   000072）任何先跑过 up 的库，都会留下一条"记账 72、内容却不是现在 72 文件"
--   的记录。之后把我们的那份改名成 000074，只解决了"仓库里编号唯一"，
--   解决不了"已经错位的库"——migrate 认为 72 做完了，永久跳过
--   000072_formal_config_group，而它不会报错，只在运行期表现为列不存在（500）。
--
--   实际存在的三种错位形态：
--     A. 记账 72（当年跑的是 customer_quote_channel）→ 缺 config_group_id
--     B. 记账 72/73（跑的是 formal_config_group + 报告元数据）→ 缺 customer_quote_id
--     C. 记账被 force 强推到头，结构却没跟上
--
-- 处置思路：不靠人去每个库看、也不靠手改 schema_migrations / force，
-- 而是让所有人都往前跑一条「把结构补齐」的迁移。本文件逐条重复 72/73/74
-- 的 DDL，全部带 IF NOT EXISTS / IF EXISTS，因此：
--   · 结构已存在的库 → 全部空操作；
--   · 缺哪条补哪条；
--   · 任何库跑完 up 后版本都统一前进到 75，三处（本地/同事/测试服务器）自洽。
--
-- 纪律：以后新增迁移取号前先 `git fetch` 看 main 的最大编号。
--       本文件只为修历史遗留，不要拿它当"以后随便改结构"的模板。

-- ============================================================
-- 1) 迁移 000072_formal_config_group 的结构（正式配置按准入配置组复用）
-- ============================================================
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
