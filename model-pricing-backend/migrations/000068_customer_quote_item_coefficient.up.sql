-- 000068_customer_quote_item_coefficient：把「对外报价系数」存进明细。
--
-- 背景：报价编辑器里销售填的是毛利率或报价系数，提交时被换算成单价（官方价 × 系数），
-- 系数本身没有落库。于是导出「对外报价」只能反过来用 单价 ÷ 当前官方价 倒推——
-- 这在官方价变动后会算出完全错误的系数（客户当初谈 0.7，官方价涨了导出来变成 0.5），
-- 而历史数据里也确实倒推出过 3.33 这种不可能的值。
--
-- 业务定性（2026-11-10 确认）：**系数是主数据，单价是派生值**。
-- 后续改价操作改的也是报价系数 / 采购系数，所以系数必须原样存下来。
--
-- 历史单据：无法还原用户当时填的值，本迁移**不回填**（不猜数）。
-- 这些行导出时会被跳过，重新保存一次报价单即可补上系数。

ALTER TABLE customer_quote_item
    ADD COLUMN IF NOT EXISTS quotation_coefficient numeric(10,6);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_cqi_quotation_coefficient'
    ) THEN
        ALTER TABLE customer_quote_item
            ADD CONSTRAINT ck_cqi_quotation_coefficient
            CHECK (quotation_coefficient IS NULL OR quotation_coefficient > 0);
    END IF;
END $$;

COMMENT ON COLUMN customer_quote_item.quotation_coefficient IS
    '对外报价系数（客户单价 ÷ 官方价）：报价编辑器填写的原值，导出直接使用；NULL = 历史单据未记录。';
