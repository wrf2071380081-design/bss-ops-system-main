-- 000072 down：回退到「一客户一条进行中流程 + 一客户一模型一条生效来源」。
--
-- 注意：如果库里已经存在多通道数据（同客户同模型多条 ACTIVE 绑定，或多个进行中流程），
-- 重建唯一索引会失败并整体回滚——这是有意的，避免把多通道数据悄悄丢掉。
-- 回退前先清理冲突数据：
--   SELECT customer_profile_id, standard_model_id, count(*) FROM customer_resource_link
--    WHERE archived = FALSE AND state = 'ACTIVE'
--    GROUP BY 1,2 HAVING count(*) > 1;
--   SELECT owner_id, count(*) FROM onboarding_flow
--    WHERE kind = 'CUSTOMER' AND status = 'PROCESSING' GROUP BY 1 HAVING count(*) > 1;

DROP INDEX IF EXISTS public.uk_flow_customer_quote_processing;
CREATE UNIQUE INDEX uk_flow_customer_processing
  ON public.onboarding_flow(owner_id)
  WHERE kind = 'CUSTOMER' AND status = 'PROCESSING';

CREATE UNIQUE INDEX uk_crl_customer_model
  ON public.customer_resource_link(customer_profile_id, standard_model_id)
  WHERE archived = FALSE AND state = 'ACTIVE';

DROP INDEX IF EXISTS public.idx_todo_flow;
ALTER TABLE public.todo_task DROP COLUMN IF EXISTS flow_id;

DROP INDEX IF EXISTS public.idx_flow_customer_quote;
ALTER TABLE public.onboarding_flow DROP COLUMN IF EXISTS customer_quote_id;
