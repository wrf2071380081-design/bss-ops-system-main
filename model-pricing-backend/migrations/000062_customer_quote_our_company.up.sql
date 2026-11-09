-- 报价单落「我方公司」：原型「报价信息」里这是销售手填的签约主体，
-- 之前只在前端输入框里存着，出单/导出都带不上，回看历史报价也丢了。
-- 存到单头上（一张单一个签约主体），避免以后导出/对账时无从追溯。
ALTER TABLE customer_quote ADD COLUMN IF NOT EXISTS our_company varchar(128) NOT NULL DEFAULT '';

COMMENT ON COLUMN customer_quote.our_company IS '我方签约主体（报价信息里手填，可为空：老单没有记录）';
