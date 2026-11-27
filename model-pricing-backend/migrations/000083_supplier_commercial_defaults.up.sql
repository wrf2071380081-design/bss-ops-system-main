-- 000083：供应商档案补齐 v0.2.1 原型的联系人信息与「主体默认商务与合规条款」。
--
-- 来源：Token-Ops v0.2.1 原型 b1ContactFields / b1CommercialDefaultFields，
--       内部「档案与办理」与供应商门户「账户设置」共用同一份主体资料。
--
-- 口径（产品 2026-11-27 确认）：
--   1. 全部自由文本，不做枚举、不做格式校验（邮箱暂不校验），空值表示「未填写」；
--   2. 原型里的示例（预付 / 月结 / T+30 等）只是 placeholder，不入库；
--   3. 企业证明、结算币种、税率、备注维持现状，不在本迁移内改动。
--
-- ⚠️ 纪律：本迁移只加列，**绝不回填**，也不设非空 DEFAULT。
--   档案读取会把 supplier 主行与 operation_log 里最新一条快照逐字段比对
--   （supplypg.verifySupplierAuditSnapshot）。历史快照里没有这 9 个键，
--   解码后是空串；只要库里也是空（NULL），两边就相等，档案保持可信。
--   一旦 UPDATE 回填或给列设非空默认值，快照缺键 vs 主行有值 → 判定不一致
--   → audit_status=INCOMPLETE、revision 归 0，该供应商档案会变成只读。
--   需要给老数据填值，只能走界面保存（会同时续写快照）。
ALTER TABLE supplier
  ADD COLUMN IF NOT EXISTS contact_title         varchar(64)  NULL,
  ADD COLUMN IF NOT EXISTS contact_email         varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS default_settlement    varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS default_payment       varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS invoice_preference    varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS min_consumption       varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS data_training_policy  varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS log_retention_policy  varchar(128) NULL,
  ADD COLUMN IF NOT EXISTS default_sla           varchar(128) NULL;

COMMENT ON COLUMN supplier.contact_title        IS '联系人职务（主体档案，自由文本）';
COMMENT ON COLUMN supplier.contact_email        IS '联系人邮箱（主体档案，自由文本，暂不校验）';
COMMENT ON COLUMN supplier.default_settlement   IS '主体默认账期（自由文本，如 预付 / 月结 30 天 / T+30）';
COMMENT ON COLUMN supplier.default_payment      IS '主体默认付款方式（自由文本）';
COMMENT ON COLUMN supplier.invoice_preference   IS '发票偏好（自由文本）';
COMMENT ON COLUMN supplier.min_consumption      IS '最低消费（自由文本，可含金额+币种+周期）';
COMMENT ON COLUMN supplier.data_training_policy IS '数据训练政策（自由文本）';
COMMENT ON COLUMN supplier.log_retention_policy IS '日志留存与删除策略（自由文本）';
COMMENT ON COLUMN supplier.default_sla          IS '主体默认 SLA（自由文本，与合同/授信/采购价相互独立）';
