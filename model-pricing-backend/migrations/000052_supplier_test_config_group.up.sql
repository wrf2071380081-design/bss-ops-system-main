-- 000052_supplier_test_config_group：供应商准入测试配置**多组**改造。
--
-- 背景（原型对齐）：
--   000046 把测试 URL/Key 收敛成「主体级一条」（supplier_test_credential 主键就是 supplier_id）。
--   但原型申报页是**多个配置组**：
--     · 「准入测试配置」区块卡片式列出多个组（如「原单组配置」），每组一套 URL/Key，可新增；
--     · 模型表格里有「准入配置组」列，每行挂一个组；
--     · 提交门槛是「每组至少一个连通成功 + 整批至少一次有效压测」。
--   000046 只实现了「主体级」那半句，漏了「可以多个组」。
--
-- 本迁移做什么：
--   1. 新建 supplier_test_config_group（组名在同一主体内唯一）；
--   2. submission_row 增加 test_config_group_id（一行挂一个组）；
--   3. **存量回填**：每个有旧凭据的主体生成一条「原单组配置」，并把现有申报行全部指向它。
--      这样升级后所有在途申报立即可用，不需要供应商重新配置。
--
-- 为什么 supplier_test_credential 保留不删：
--   代码本版本起停止读写它，但保留表与数据便于回滚核对；
--   等下一版本确认稳定后再单独迁移清理（写在这里，免得被当成"忘了删"）。

CREATE TABLE IF NOT EXISTS supplier_test_config_group (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  supplier_id  bigint NOT NULL REFERENCES supplier(id) ON DELETE CASCADE,
  name         varchar(64)  NOT NULL,
  base_url     varchar(512) NOT NULL,
  api_key_enc  varchar(512) NULL,
  enabled      boolean NOT NULL DEFAULT true,
  sort_order   int NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   bigint NULL,
  CONSTRAINT uk_stcg_supplier_name UNIQUE (supplier_id, name)
);

COMMENT ON TABLE supplier_test_config_group IS '供应商准入测试配置组：一个主体可有多套 URL/Key，申报行按行挂组（取代 supplier_test_credential）';
COMMENT ON COLUMN supplier_test_config_group.api_key_enc IS '测试 Key 密文；接口永不回显，只返回 has_api_key';

-- 部分索引：只有启用的组参与「按主体列组」的热路径。
CREATE INDEX IF NOT EXISTS idx_stcg_supplier ON supplier_test_config_group(supplier_id) WHERE enabled;

ALTER TABLE submission_row
  ADD COLUMN IF NOT EXISTS test_config_group_id bigint NULL
  REFERENCES supplier_test_config_group(id);

COMMENT ON COLUMN submission_row.test_config_group_id IS '该行使用哪个测试配置组；发单/算指纹/判失效都以它为准（000052 起）';

-- 存量回填 1)：每个有旧凭据的主体建一条「原单组配置」。
INSERT INTO supplier_test_config_group (supplier_id, name, base_url, api_key_enc, sort_order)
SELECT supplier_id, '原单组配置', base_url, api_key_enc, 0
  FROM supplier_test_credential
ON CONFLICT (supplier_id, name) DO NOTHING;

-- 存量回填 2)：现有申报行全部指向它（只填 NULL，重复执行安全）。
UPDATE submission_row r
   SET test_config_group_id = g.id
  FROM submission s
  JOIN supplier_test_config_group g
    ON g.supplier_id = s.supplier_id AND g.name = '原单组配置'
 WHERE r.submission_id = s.id AND r.test_config_group_id IS NULL;
