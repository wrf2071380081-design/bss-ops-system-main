-- 000034_test_report_cache：测试报告补缓存统计字段（原型 v0.22）。
--
-- 背景：v0.22 模型详情的供应来源表新增「输入缓存命中率」列。
-- 展示口径：命中输入 Token ÷ 总输入 Token × 100，保留 2 位小数加 %；
-- 缺数据或非法（hit < 0、hit > input）显示"未提供"。
-- v0.20 共享规则：旧快照不改写，只新增字段，不回填历史。
--
-- 兼容：PostgreSQL 14+（线上 14.24）。

ALTER TABLE test_report
  ADD COLUMN IF NOT EXISTS cache_hit_tokens   bigint NULL,
  ADD COLUMN IF NOT EXISTS cache_input_tokens bigint NULL;

COMMENT ON COLUMN test_report.cache_hit_tokens   IS '命中输入 Token（v0.22 缓存统计；缺失/非法显示未提供）';
COMMENT ON COLUMN test_report.cache_input_tokens IS '总输入 Token（v0.22 缓存统计；缺失/非法显示未提供）';
