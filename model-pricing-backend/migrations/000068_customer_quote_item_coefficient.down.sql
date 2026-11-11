-- 000068 (down)：撤掉对外报价系数列。
--
-- 说明：down 会丢弃已录入的系数（无法从单价无损还原），且需要重新保存报价单才能补回。
-- 仅在明确不需要该字段时使用。

ALTER TABLE customer_quote_item DROP CONSTRAINT IF EXISTS ck_cqi_quotation_coefficient;
ALTER TABLE customer_quote_item DROP COLUMN IF EXISTS quotation_coefficient;
