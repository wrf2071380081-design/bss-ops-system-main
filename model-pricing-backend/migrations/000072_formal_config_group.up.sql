-- 000072: 正式配置按「准入配置组」维度复用
--
-- 业务规则（2026-11-20）：正式配置代表一条通道（BaseURL + Key），
-- 同组模型共用一条；不同组即使 URL 相同也要各配一次。
-- 之前只有 supplier+region 维度，导致给一个模型配完，同供应商其他模型也显示"已有配置"。
ALTER TABLE supplier_formal_config
    ADD COLUMN IF NOT EXISTS config_group_id bigint REFERENCES supplier_test_config_group(id);

CREATE INDEX IF NOT EXISTS idx_formal_config_group
    ON supplier_formal_config(supplier_id, region, config_group_id);

COMMENT ON COLUMN supplier_formal_config.config_group_id IS '所属准入配置组；NULL 表示历史数据（未区分组）';
