-- 000029_fix_permission_point_m8_m9_names：修正 M8 / M9 权限点显示名写反。
--
-- 背景：000007_seed 把 M8 的名称写成「客户报价·*」、M9 写成「客户管理·*」，与实际代码行为相反。
-- 事实（以代码为准）：
--   M8 = 客户管理   → router.go 的 GET /customers 挂 M8:V、POST /customers/:id/transfer 挂 M8:E；
--                     pkg/perm/perm.go 的 M8View 注释亦为「客户管理·查看」
--   M9 = 客户报价   → /customer-quotes、/customer-quotes/:id/special-price 挂 M9:*；
--                     pkg/perm/perm.go 的 M9View 注释亦为「客户报价·查看」
--
-- 影响范围：仅 permission_point.name 显示名。鉴权按 module_code 走，功能不受影响。
-- 不改 000007_seed（已提交的历史迁移），不动任何 role_permission 授权。

UPDATE permission_point SET name = '客户管理·查看', updated_at = now() WHERE module_code = 'M8' AND action_code = 'V';
UPDATE permission_point SET name = '客户管理·编辑', updated_at = now() WHERE module_code = 'M8' AND action_code = 'E';
UPDATE permission_point SET name = '客户管理·审批', updated_at = now() WHERE module_code = 'M8' AND action_code = 'A';
UPDATE permission_point SET name = '客户管理·配置', updated_at = now() WHERE module_code = 'M8' AND action_code = 'C';
UPDATE permission_point SET name = '客户管理·特权', updated_at = now() WHERE module_code = 'M8' AND action_code = 'P';

UPDATE permission_point SET name = '客户报价·查看', updated_at = now() WHERE module_code = 'M9' AND action_code = 'V';
UPDATE permission_point SET name = '客户报价·编辑', updated_at = now() WHERE module_code = 'M9' AND action_code = 'E';
UPDATE permission_point SET name = '客户报价·审批', updated_at = now() WHERE module_code = 'M9' AND action_code = 'A';
UPDATE permission_point SET name = '客户报价·配置', updated_at = now() WHERE module_code = 'M9' AND action_code = 'C';
UPDATE permission_point SET name = '客户报价·特权', updated_at = now() WHERE module_code = 'M9' AND action_code = 'P';
