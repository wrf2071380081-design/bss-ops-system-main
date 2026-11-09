-- 回滚：删掉我方公司列（历史值一并丢弃）。
ALTER TABLE customer_quote DROP COLUMN IF EXISTS our_company;
