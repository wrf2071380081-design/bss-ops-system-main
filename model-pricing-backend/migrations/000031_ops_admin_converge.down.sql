-- 000031_ops_admin_converge (down)：还原到 000031 之前的形态。
--
-- 还原目标（以 000007_seed 为准）：
--   OPS_ADMIN 持 M11 全部 5 个点（V/E/A/C/P）—— 即本批删掉的 E/A/C/P 要补回
--   role.name = 「运维管理员」
--
-- 说明：E/A/C/P 是历史越权授权，但 down 的职责是回到迁移前状态，故原样补回。
-- 其余权限（M10:V / M12:V / M1–M9:V）与 data_scope 本批未动，down 亦不动。

-- 1) 补回 OPS_ADMIN 对 M11 的写权限（E/A/C/P）
INSERT INTO role_permission(role_id, permission_point_id, created_at, updated_at, request_id, created_by, updated_by)
SELECT r.id, pp.id, now(), now(), NULL, 1, 1
  FROM role r, permission_point pp
 WHERE r.code = 'OPS_ADMIN'
   AND pp.module_code = 'M11'
   AND pp.action_code IN ('E', 'A', 'C', 'P')
   AND NOT EXISTS (
         SELECT 1 FROM role_permission x WHERE x.role_id = r.id AND x.permission_point_id = pp.id
       );

-- 2) 角色显示名还原为「运维管理员」
UPDATE role
   SET name = '运维管理员', updated_at = now()
 WHERE code = 'OPS_ADMIN';
