-- 000034_test_report_cache（down）：删除缓存统计字段。
ALTER TABLE test_report
  DROP COLUMN IF EXISTS cache_input_tokens,
  DROP COLUMN IF EXISTS cache_hit_tokens;
