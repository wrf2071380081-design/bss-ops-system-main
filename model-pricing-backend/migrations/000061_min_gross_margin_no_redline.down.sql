-- 回滚到 15% 毛利率红线。
-- 注意：若库里已存在毛利率 <15% 的报价单，回滚后它们一旦被重算/刷新就会命中 floor，属于预期副作用。
UPDATE sys_config SET config_value = '0.15' WHERE config_key = 'min_gross_margin';
