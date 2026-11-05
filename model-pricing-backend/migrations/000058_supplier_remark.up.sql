-- 供应商基础资料的「备注」字段：原型「档案与办理 → 基础资料」最后一行。
-- 与 contract/credit 的备注无关，这里只是主体档案的自述说明。
ALTER TABLE supplier ADD COLUMN IF NOT EXISTS remark text;
