-- 000049_customer_resource_link 回滚：整表删除。
--
-- 说明：本表是本轮新增的客户接入模型绑定，没有任何既有表引用它，
-- 因此可以干净回滚。回滚会丢掉绑定历史（含「来源已下架待处理」的痕迹），
-- 这是有意的：不静默把绑定关系塞进别处，也不留半张表。
DROP INDEX IF EXISTS public.idx_crl_customer;
DROP INDEX IF EXISTS public.idx_crl_resource;
DROP INDEX IF EXISTS public.uk_crl_customer_model;
DROP INDEX IF EXISTS public.uk_crl_customer_resource;

DROP TABLE IF EXISTS public.customer_resource_link;
