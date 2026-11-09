-- 原型客户详情「档案与办理 → 主体资料」：联系人 / 网关用户名 / 备注。
-- 客户名称、区域、税率、合同状态此前已有，这里只补缺失的三列。
ALTER TABLE public.customer_profile ADD COLUMN IF NOT EXISTS contact_name varchar(64);
ALTER TABLE public.customer_profile ADD COLUMN IF NOT EXISTS gateway_username varchar(128);
ALTER TABLE public.customer_profile ADD COLUMN IF NOT EXISTS remark varchar(500);

COMMENT ON COLUMN public.customer_profile.contact_name IS '客户侧对接联系人';
COMMENT ON COLUMN public.customer_profile.gateway_username IS '网关用户名（客户调用侧账号）';
COMMENT ON COLUMN public.customer_profile.remark IS '主体资料备注';
