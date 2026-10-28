-- 000046_supplier_test_credential：测试凭据上移主体（原型对齐，2026-10-27）。
--
-- 背景：原型申报页把「测试接口 URL / 测试 API Key」放在**主体级**，一句说明
-- 「此 URL／Key 仅由供应商本人维护，用于本主体准入测试、供申报模型共用」；
-- 代码里凭据一直是**行级**（submission_row.test_base_url / test_api_key_enc），
-- 供应商每加一个模型都要重复填一遍，且同主体容易出现多套不一致的地址。
--
-- 本迁移只新增主体凭据表；submission_row 的两列**保留**，语义改为一句话：
--   「保存草稿 / 发起测试时由主体凭据写入的快照」，用于报告留痕与历史回放，
--   供应商不再直接编辑它们。
--
-- 正式运行配置不受影响：resource.test_base_url / test_api_key_enc 那套
-- （supplier_formal_config 线）继续单独维护，原型明确「不用于平台运营测试」。
--
-- 兼容：PostgreSQL 14+。

CREATE TABLE IF NOT EXISTS supplier_test_credential (
  supplier_id  bigint       PRIMARY KEY REFERENCES supplier(id) ON DELETE CASCADE,
  base_url     varchar(512) NOT NULL,
  api_key_enc  varchar(512) NULL,
  updated_at   timestamptz  NOT NULL DEFAULT now(),
  updated_by   bigint       NULL
);

COMMENT ON TABLE  supplier_test_credential             IS '供应商主体级准入测试凭据：一主体一套 URL/Key，申报模型共用';
COMMENT ON COLUMN supplier_test_credential.base_url    IS '测试接口 URL（主体级，供本主体所有申报模型共用）';
COMMENT ON COLUMN supplier_test_credential.api_key_enc IS '测试 API Key；列名沿用 _enc 历史命名，当前按原样存储（路线 A）';
COMMENT ON COLUMN supplier_test_credential.updated_by  IS '最后修改账号 account.id（门户账号）';

-- 回填：让存量主体切过来就能继续测试，不必逐个重新登记。
-- 取该主体**最近一条**非空行级凭据；同一主体历史上可能存在多套地址，
-- 这里只取最新的一条（与「主体共用」的新语义最接近），其余丢弃。
INSERT INTO supplier_test_credential (supplier_id, base_url, api_key_enc, updated_at)
SELECT DISTINCT ON (s.supplier_id)
       s.supplier_id,
       r.test_base_url,
       r.test_api_key_enc,
       now()
  FROM submission_row r
  JOIN submission s ON s.id = r.submission_id
 WHERE COALESCE(r.test_base_url, '') <> ''
 ORDER BY s.supplier_id, r.id DESC
ON CONFLICT (supplier_id) DO NOTHING;

COMMENT ON COLUMN submission_row.test_base_url    IS '主体测试凭据快照（000046 起由 supplier_test_credential 写入，供应商不再直接编辑）';
COMMENT ON COLUMN submission_row.test_api_key_enc IS '主体测试 Key 快照（同上，仅用于报告留痕与历史回放）';
