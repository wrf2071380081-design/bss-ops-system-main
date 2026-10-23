-- 000039_supplier_submission_draft：供应商申报的草稿与乐观锁支持（B-1）。
--
-- 背景：供应商门户申报需要「存草稿 → 继续编辑 → 提交」，前端契约里带有
-- expected_revision（乐观锁）与 updated_at（列表展示），但现有表两者都没有；
-- 且 submitted_at 是 NOT NULL，草稿根本插不进去。
--
-- 编号说明：000038 已被「测试平台」分支预留（test_report_cache_ratio），
-- 本迁移顺延到 39，两支合并后按 37 → 38 → 39 顺序应用，互不依赖。
--
-- 兼容：PostgreSQL 14+（线上 14.24）。

ALTER TABLE submission
  ADD COLUMN IF NOT EXISTS revision   bigint      NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

-- 草稿没有提交时间：允许为空，提交时再写入。
ALTER TABLE submission ALTER COLUMN submitted_at DROP NOT NULL;

-- 供应商门户列表固定按「自己 + 时间倒序」翻页。
CREATE INDEX IF NOT EXISTS idx_submission_supplier_created
  ON submission (supplier_id, created_at DESC);

COMMENT ON COLUMN submission.revision IS '乐观锁版本号：每次写操作 +1，客户端提交 expected_revision 比对';
COMMENT ON COLUMN submission.updated_at IS '最后修改时间（草稿保存也会更新）';
