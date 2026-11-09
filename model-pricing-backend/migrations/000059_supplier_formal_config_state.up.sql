-- 正式配置的状态落表（000059）
--
-- 背景：000033 建 supplier_formal_config 时只落了「内容」字段
-- （scope/base_url/api_key_enc/created_by/updated_by/时间戳），
-- 状态类字段（status/revision/confirmed_*）与 region 一直只存在于
-- operation_log 的审计快照里，读取时靠 FormalConfigStore.Current 拼回来再逐字段比对。
-- 结果是：事实被拆成两半，任一半的精度差异都退化成 ErrReadContract（对外 500），
-- 且这条链路从来没有通过接口成功创建过配置（全库仅 1 条种子数据）。
--
-- 本迁移把这些字段补进表，让表成为唯一事实源；operation_log 继续保留全量快照做审计留痕，
-- 不再参与字段级判定。

ALTER TABLE supplier_formal_config
  ADD COLUMN IF NOT EXISTS region             varchar(16),
  ADD COLUMN IF NOT EXISTS status             varchar(16) NOT NULL DEFAULT 'ACTIVE',
  ADD COLUMN IF NOT EXISTS revision           bigint      NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS confirmed_by       bigint,
  ADD COLUMN IF NOT EXISTS confirmed_at       timestamptz,
  ADD COLUMN IF NOT EXISTS confirmed_revision bigint      NOT NULL DEFAULT 1;

-- region 与所属主体保持一致（一主体一区域）
UPDATE supplier_formal_config c
SET region = s.region
FROM supplier s
WHERE s.id = c.supplier_id AND c.region IS DISTINCT FROM s.region;

-- 存量行的状态从最新一条审计快照回填；没有快照的保持默认值（ACTIVE / 1）。
WITH latest AS (
  SELECT DISTINCT ON (entity_id) entity_id, "after" -> 'view' AS view
  FROM operation_log
  WHERE entity_type = 'supplier_formal_config'
    AND action = 'FORMAL_CONFIG_REVISION'
  ORDER BY entity_id, id DESC
)
UPDATE supplier_formal_config c
SET status             = COALESCE(l.view ->> 'status', 'ACTIVE'),
    revision           = COALESCE((l.view ->> 'revision')::bigint, 1),
    confirmed_by       = NULLIF(l.view ->> 'confirmed_by', '')::bigint,
    confirmed_at       = NULLIF(l.view ->> 'confirmed_at', '')::timestamptz,
    confirmed_revision = COALESCE((l.view ->> 'confirmed_revision')::bigint, 1)
FROM latest l
WHERE l.entity_id = c.id;

-- 自洽兜底：有效配置必须「已确认到当前修订」，否则读取判定会判它不可用。
UPDATE supplier_formal_config
SET confirmed_revision = revision
WHERE status = 'ACTIVE' AND confirmed_revision <> revision;

ALTER TABLE supplier_formal_config
  ALTER COLUMN region SET NOT NULL;

ALTER TABLE supplier_formal_config
  ADD CONSTRAINT ck_supplier_formal_config_status
    CHECK (status IN ('ACTIVE', 'INACTIVE')),
  ADD CONSTRAINT ck_supplier_formal_config_revision
    CHECK (revision > 0 AND confirmed_revision > 0 AND confirmed_revision <= revision),
  ADD CONSTRAINT ck_supplier_formal_config_region
    CHECK (region IN ('CN', 'OVERSEAS'));

CREATE INDEX IF NOT EXISTS idx_supplier_formal_config_status
  ON supplier_formal_config (supplier_id, status);
