-- 000043：打通供应商身份链路（supplier ↔ legal_subject）。
--
-- 背景：仓库里并存两套供应商主体模型，彼此没有任何关联列：
--   · 旧模型 legal_subject + supplier_profile —— 门户账号 owner_type='OPERATOR' 时，
--     account.owner_id 存的是 subject_operator.id，链路 subject_operator → subject_id → supplier_profile.id；
--   · 新模型 supplier（000033，A/B 线申报、资源、测试平台都用它）—— 门户账号 owner_type='SUPPLIER' 时，
--     account.owner_id 直接是 supplier.id。
-- 同一个 OperatorID 因此有两套语义，门户接口无法稳定定位供应商。
--
-- 本迁移只做一件事：给 supplier 补 subject_id 并回填，让两条链路能收敛到同一个 supplier 行。

ALTER TABLE supplier ADD COLUMN IF NOT EXISTS subject_id bigint NULL REFERENCES legal_subject(id);

-- ⚠️ 索引名不能叫 uk_supplier_subject：000002 已在 supplier_profile(subject_id) 上占用同名索引，
-- Postgres 的索引名是 schema 级唯一，重名会直接让迁移失败。
CREATE UNIQUE INDEX IF NOT EXISTS uk_supplier_legal_subject ON supplier(subject_id) WHERE subject_id IS NOT NULL;

COMMENT ON COLUMN supplier.subject_id IS '登录主体 legal_subject.id；000043 打通 supplier 与门户身份链路';

-- 回填 1：手机号对齐。手机号是供应商门户的登录键（LoginSupplierByMobile 按 supplier.phone 定位），最可靠。
UPDATE supplier s
   SET subject_id = ls.id
  FROM legal_subject ls
 WHERE s.subject_id IS NULL
   AND s.phone IS NOT NULL AND s.phone <> ''
   AND ls.mobile = s.phone
   AND NOT EXISTS (SELECT 1 FROM supplier x WHERE x.subject_id = ls.id);

-- 回填 2：名称一致（兜底，给没填手机号的历史数据）。
UPDATE supplier s
   SET subject_id = ls.id
  FROM legal_subject ls
 WHERE s.subject_id IS NULL
   AND ls.legal_name = s.name
   AND NOT EXISTS (SELECT 1 FROM supplier x WHERE x.subject_id = ls.id);
