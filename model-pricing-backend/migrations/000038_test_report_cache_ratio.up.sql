-- 000038_test_report_cache_ratio：测试报告补「缓存命中率」比率字段（Q4 决策）。
--
-- 背景：000034 存的是绝对 Token（cache_hit_tokens / cache_input_tokens），
-- 但测试平台只上报比率（cache_hit_ratio_pct）。两者不能互相换算：
-- 缺总量时反算 token 数等于编造数据，所以页面上的值是永远算不出来的。
--
-- 本次改为直接存比率：
--   cache_hit_ratio       0-100 的百分数，NULL 表示未知
--   cache_ratio_available 是否真的上报了比率 —— 用来区分「0.00%」与「不适用/未提供」三态
--
-- 000034 的绝对列保留不动（历史行可能已有值），但不再作为页面展示来源。
-- 兼容：PostgreSQL 14+（线上 14.24）。

ALTER TABLE test_report
  ADD COLUMN IF NOT EXISTS cache_hit_ratio       numeric(5,2) NULL,
  ADD COLUMN IF NOT EXISTS cache_ratio_available boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN test_report.cache_hit_ratio IS '缓存命中率百分比（0-100，2 位小数）；NULL 表示未知';
COMMENT ON COLUMN test_report.cache_ratio_available IS '是否上报了比率：用于区分 0% 与未提供（Q4 三态口径）';
