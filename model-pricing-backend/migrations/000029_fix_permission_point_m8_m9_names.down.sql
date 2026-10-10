-- 000029_fix_permission_point_m8_m9_names (down)：把 M8 / M9 显示名还原为 000007_seed 的原始（错误）取值。
-- 说明：原始取值本身是错的，但 down 必须能对称回滚到迁移前状态，故按 seed 原文还原。

UPDATE permission_point SET name = '客户报价·查看', updated_at = now() WHERE module_code = 'M8' AND action_code = 'V';
UPDATE permission_point SET name = '客户报价·编辑', updated_at = now() WHERE module_code = 'M8' AND action_code = 'E';
UPDATE permission_point SET name = '客户报价·审批', updated_at = now() WHERE module_code = 'M8' AND action_code = 'A';
UPDATE permission_point SET name = '客户报价·配置', updated_at = now() WHERE module_code = 'M8' AND action_code = 'C';
UPDATE permission_point SET name = '客户报价·特权', updated_at = now() WHERE module_code = 'M8' AND action_code = 'P';

UPDATE permission_point SET name = '客户管理·查看', updated_at = now() WHERE module_code = 'M9' AND action_code = 'V';
UPDATE permission_point SET name = '客户管理·编辑', updated_at = now() WHERE module_code = 'M9' AND action_code = 'E';
UPDATE permission_point SET name = '客户管理·审批', updated_at = now() WHERE module_code = 'M9' AND action_code = 'A';
UPDATE permission_point SET name = '客户管理·配置', updated_at = now() WHERE module_code = 'M9' AND action_code = 'C';
UPDATE permission_point SET name = '客户管理·特权', updated_at = now() WHERE module_code = 'M9' AND action_code = 'P';
