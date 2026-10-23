-- 客户管理列表/报价单对齐交互原型所需的档案字段。
-- 说明：原型列里有「国内/海外」「结算币种」「税率」「合同状态」，
-- 而 customer_profile 原本没有这四列；不编造派生值，直接落库，缺省按原型默认值。
ALTER TABLE customer_profile
    ADD COLUMN region varchar(16) NOT NULL DEFAULT 'CN',
    ADD COLUMN settlement_currency varchar(8) NOT NULL DEFAULT 'CNY',
    ADD COLUMN tax_rate numeric(6, 4),
    ADD COLUMN contract_status varchar(16) NOT NULL DEFAULT '待记录';

-- 取值约束与原型枚举一致（合同状态：待记录 / 办理中 / 已归档）。
ALTER TABLE customer_profile
    ADD CONSTRAINT ck_customer_region CHECK (region IN ('CN', 'OVERSEAS')),
    ADD CONSTRAINT ck_customer_contract_status CHECK (contract_status IN ('待记录', '办理中', '已归档'));

COMMENT ON COLUMN customer_profile.region IS '国内/海外主体（原型：国内 / 海外）';
COMMENT ON COLUMN customer_profile.settlement_currency IS '结算币种（原型：人民币 CNY / 美元 USD）';
COMMENT ON COLUMN customer_profile.tax_rate IS '税率（百分比数值，NULL 表示原型里的“未填写”）';
COMMENT ON COLUMN customer_profile.contract_status IS '合同状态（待记录 / 办理中 / 已归档）';
