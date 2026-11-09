-- 000067 (down)：只撤掉默认值。
--
-- 不回滚数据回填：把已经可用的币种改回 NULL 会重现 2026-11-08 的
-- 「合同授信办不完 / 待办不消失」故障，回滚不应制造故障。

ALTER TABLE supplier ALTER COLUMN settlement_currency DROP DEFAULT;
