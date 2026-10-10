-- 000030_split_m5_permission (down)：把 M5 / M10 授权还原到 000030 之前的形态。
--
-- 还原目标（以 000007_seed 的授权为准）：
--   M5:E = {PLATFORM_ADMIN, PROCUREMENT, PRICING_OP, FINANCE, MODEL_OPS, RETRO_OP}
--   M5:C = {PLATFORM_ADMIN, MODEL_OPS}          （PRICING_OP 的 M5:C 是本批新增，down 要撤掉）
--   M10:E = {PLATFORM_ADMIN, FINANCE}           （本来就有，本批只是幂等兜底，down 不动）
--
-- 说明：PROCUREMENT 在迁移前**不持** M5:C，故 down 不补 M5:C（对称：up 只是防御性删除）。
-- MODEL_OPS / RETRO_OP 的 M5:E 是历史遗留越权，但 down 的职责是回到迁移前状态，故原样补回。

-- 1) 补回被收回的 M5:E（PRICING_OP / FINANCE / MODEL_OPS / RETRO_OP）
INSERT INTO role_permission(role_id, permission_point_id, created_at, updated_at, request_id, created_by, updated_by)
SELECT r.id, pp.id, now(), now(), NULL, 1, 1
  FROM role r, permission_point pp
 WHERE r.code IN ('PRICING_OP', 'FINANCE', 'MODEL_OPS', 'RETRO_OP')
   AND pp.module_code = 'M5'
   AND pp.action_code = 'E'
   AND NOT EXISTS (
         SELECT 1 FROM role_permission x WHERE x.role_id = r.id AND x.permission_point_id = pp.id
       );

-- 2) 撤掉本批给 PRICING_OP 新增的 M5:C
DELETE FROM role_permission rp
 USING role r, permission_point pp
 WHERE rp.role_id = r.id
   AND rp.permission_point_id = pp.id
   AND r.code = 'PRICING_OP'
   AND pp.module_code = 'M5'
   AND pp.action_code = 'C';
