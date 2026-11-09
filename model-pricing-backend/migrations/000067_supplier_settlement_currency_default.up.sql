-- 000067_supplier_settlement_currency_default：补上供应商结算币种的默认值 + 回填历史空值。
--
-- 背景（2026-11-08 故障）：
--   000028 只给 supplier_profile 加了 NOT NULL DEFAULT 'CNY'，而 F12「合同与授信」
--   读的是 **supplier** 表（internal/infra/supplypg/f12_native_flow.go 的 flow()）。
--   supplier.settlement_currency 既 nullable 又无默认值，11-06 之后新建的供应商
--   （id 256–260）全部落成 NULL，导致：
--     F12 业务上下文校验失败 → readiness 算不出 → 前端判定「资料未齐备」
--     → 「登记合同授信完成」被拦 → 工作台待办永远不结束。
--
-- 本迁移做两件事：
--   1. 按 region 回填历史空值（CN→CNY、OVERSEAS→USD，与申报页同一口径）；
--   2. 给列加 DEFAULT 'CNY'，让后续任何漏写该字段的插入路径都有一个合法兜底，
--      而不是再落 NULL 把整条办理链卡死。
--
-- 幂等：UPDATE 只命中空值；SET DEFAULT 重复执行无副作用。

UPDATE supplier
   SET settlement_currency = CASE WHEN region = 'OVERSEAS' THEN 'USD' ELSE 'CNY' END,
       updated_at = now()
 WHERE settlement_currency IS NULL
    OR btrim(settlement_currency) = '';

ALTER TABLE supplier ALTER COLUMN settlement_currency SET DEFAULT 'CNY';
