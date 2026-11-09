-- 资源口径报价：一条明细 = (资源, 官方价项)。
--
-- 迁移 000048 建的唯一索引 uk_cqi_resource 只按 (customer_quote_id, resource_id)，
-- 但一个供应资源天然有多个官方价项（input / output / cache …）。前端按价项逐条提交，
-- 同一张单里第二条就撞唯一键 → 接口 500「系统错误」，资源口径报价根本出不了多价项的模型。
--
-- 改成把 price_item_key 一起纳入唯一键：仍然禁止「同一资源同一价项」重复，
-- 但允许同一资源的多个价项落在同一张报价单里（与 domain 层
-- GenerateResourceQuote 的注释「同资源不同价项算两条」一致）。
DROP INDEX IF EXISTS uk_cqi_resource;
CREATE UNIQUE INDEX IF NOT EXISTS uk_cqi_resource_price
  ON customer_quote_item (customer_quote_id, resource_id, price_item_key)
  WHERE resource_id IS NOT NULL;
