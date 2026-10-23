-- 000038_test_report_cache_ratio（down）：移除缓存命中率比率字段。
-- 注意：回滚会丢掉比率数据；000034 的绝对 Token 列不受影响。
ALTER TABLE test_report
  DROP COLUMN IF EXISTS cache_ratio_available,
  DROP COLUMN IF EXISTS cache_hit_ratio;
