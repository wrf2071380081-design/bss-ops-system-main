-- 回滚到「一张单里同一资源只能有一条明细」的旧口径。
-- 注意：若库里已存在同资源多价项的明细，回滚会因唯一键冲突失败——需先清理多余明细。
DROP INDEX IF EXISTS uk_cqi_resource_price;
CREATE UNIQUE INDEX IF NOT EXISTS uk_cqi_resource
  ON customer_quote_item (customer_quote_id, resource_id)
  WHERE resource_id IS NOT NULL;
