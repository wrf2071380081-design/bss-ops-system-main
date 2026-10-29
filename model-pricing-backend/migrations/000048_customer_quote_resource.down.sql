-- 000048_customer_quote_resource 回滚：去掉资源口径明细。
--
-- 注意：若已产生资源口径明细行（resource_id 非空），回滚会因 ck_cqi_source 或
-- 外键失败——这是有意的，不静默丢数据。
ALTER TABLE public.customer_quote DROP CONSTRAINT ck_customer_quote_type;
ALTER TABLE public.customer_quote
  ADD CONSTRAINT ck_customer_quote_type CHECK (quote_type::text = ANY (
    ARRAY['APPLY'::character varying, 'CLONE'::character varying, 'TEMP'::character varying,
          'SPECIAL'::character varying, 'CONTRACT'::character varying]::text[]
  ));

DROP INDEX IF EXISTS public.idx_cqi_resource;
DROP INDEX IF EXISTS public.uk_cqi_resource;
DROP INDEX IF EXISTS public.uk_cqi_sku;

ALTER TABLE public.customer_quote_item DROP CONSTRAINT IF EXISTS ck_cqi_source;
ALTER TABLE public.customer_quote_item DROP COLUMN IF EXISTS resource_id;

-- sku_id 恢复 NOT NULL 前先确认没有资源口径行（否则会失败而不是静默丢数据）
ALTER TABLE public.customer_quote_item ALTER COLUMN sku_id SET NOT NULL;

ALTER TABLE public.customer_quote_item
  ADD CONSTRAINT uk_cqi UNIQUE (customer_quote_id, sku_id);
