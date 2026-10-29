-- 000048_customer_quote_resource：客户报价明细支持「资源口径」。
--
-- 背景：客户报价此前只认 model_sku（定价域口径）。但我们真正卖出去的是
-- 「某供应商在某区域提供的某个标准模型」——即供应域的 resource（带采购系数、
-- 正式配置、测试证据、可报价状态）。两侧模型集当前不重叠，resource 无法反推 sku，
-- 因此明细要直接记 resource_id。
--
-- 口径：一条明细要么是 SKU 口径（老链路：套价目表/克隆/临时 SKU），
-- 要么是资源口径（新链路：报价候选=已接入资源），二者互斥。
--
-- 唯一约束：原 uk_cqi(customer_quote_id, sku_id) 在 sku_id 可空后不再约束资源行，
-- 拆成两个部分唯一索引（PostgreSQL 中 NULL 互不相等，故必须显式分区唯一）。
ALTER TABLE public.customer_quote_item
  ALTER COLUMN sku_id DROP NOT NULL;

ALTER TABLE public.customer_quote_item
  ADD COLUMN resource_id bigint NULL REFERENCES public.resource(id) ON DELETE RESTRICT;

-- 价项键：资源口径下这条明细对应官方价的哪一项（text_input / text_output …）。
-- 老 SKU 口径明细为 NULL。
ALTER TABLE public.customer_quote_item
  ADD COLUMN price_item_key varchar(32) NULL;

ALTER TABLE public.customer_quote_item
  ADD CONSTRAINT ck_cqi_source CHECK (
    (sku_id IS NOT NULL AND resource_id IS NULL) OR
    (sku_id IS NULL AND resource_id IS NOT NULL)
  );

ALTER TABLE public.customer_quote_item DROP CONSTRAINT uk_cqi;

CREATE UNIQUE INDEX uk_cqi_sku
  ON public.customer_quote_item(customer_quote_id, sku_id)
  WHERE sku_id IS NOT NULL;

CREATE UNIQUE INDEX uk_cqi_resource
  ON public.customer_quote_item(customer_quote_id, resource_id)
  WHERE resource_id IS NOT NULL;

CREATE INDEX idx_cqi_resource ON public.customer_quote_item(resource_id);

-- 报价单类型新增 RESOURCE：按供应资源出单（候选=已接入资源），与 APPLY/CLONE/TEMP 并列。
ALTER TABLE public.customer_quote DROP CONSTRAINT ck_customer_quote_type;
ALTER TABLE public.customer_quote
  ADD CONSTRAINT ck_customer_quote_type CHECK (quote_type::text = ANY (
    ARRAY['APPLY'::character varying, 'CLONE'::character varying, 'TEMP'::character varying,
          'RESOURCE'::character varying, 'SPECIAL'::character varying, 'CONTRACT'::character varying]::text[]
  ));

COMMENT ON COLUMN public.customer_quote_item.resource_id IS
  '供应资源口径明细：这条报价卖的是哪条已接入资源（供应商×标准模型×区域）。与 sku_id 二选一。';
COMMENT ON COLUMN public.customer_quote_item.price_item_key IS
  '资源口径明细对应的官方价项键（standard_model.price_items[].item_key）；SKU 口径为 NULL。';
