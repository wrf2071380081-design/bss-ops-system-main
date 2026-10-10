-- 000030_split_m5_permission：拆分被 6 个角色共享的 M5:E 权限点。
--
-- 背景（06-cost.md §11-1 / 待修复清单 B2）：`M5:E`（成本管理·编辑）在 000007_seed 里被
-- PRICING_OP / PROCUREMENT / FINANCE / MODEL_OPS / RETRO_OP / PLATFORM_ADMIN 六个角色同时持有，
-- 但它实际承载了**三个完全不同的动作**：
--   ① 成本参数配置（PUT /cost/params）        → 只应是 PRICING_OP
--   ② 主供应商锁定（POST /cost/baselines/:sku/lock-primary）→ 只应是 PROCUREMENT
--   ③ 汇率锁定（批次 6 待建）                  → 只应是 FINANCE
-- 中间件 RequirePerm(M5:E) 只看权限点、无法区分角色，服务层虽然用
-- pkg/perm.CanEditCostParam / CanLockPrimary / CanLockFx 二次收敛住了，但**权限点层面
-- 的授权是过宽的**（MODEL_OPS / RETRO_OP 持 M5:E 属历史遗留越权，无对应职责）。
--
-- 裁决（B2=甲）：不新增操作码，沿用字典内既有的 C / E，按动作重新挂靠：
--   成本参数配置   M5:E  → **M5:C**（仅 PRICING_OP）
--   主供应商锁定   保留    **M5:E**（仅 PROCUREMENT）
--   汇率锁定       M5:E  → **M10:E**（仅 FINANCE，批次 6 建接口时挂此点）
--
-- 影响面：只动 role_permission（角色↔权限点授权），**不动 permission_point 字典本身**，
-- 故 ValidateInDB（代码 60 个常量 ↔ 库内权限点）不受影响，启动不会报 mismatch。
--
-- ⚠️ 生效条件：会话快照在登录时生成（perms = 角色并集），已登录的会话仍持旧 perms，
--    **必须重新登录**才生效。发布说明要写这一条。
--
-- ⚠️ 残留（本批不处理，见批次 3a 报告）：MODEL_OPS 仍持 M5:C（000007 给了 M5 除 A 外全权限）。
--    当前 M5:C 只挂了成本参数一个接口，且该接口服务层要求 PRICING_OP 角色，故 MODEL_OPS 实际
--    仍改不了参数；但后续若按 B1 建议在其它接口挂 :C，MODEL_OPS 会自动获得，需届时一并收敛。
--
-- 幂等：DELETE 天然幂等；INSERT 用 NOT EXISTS 兜底。

-- 1) 收回 M5:E：只保留 PROCUREMENT（主供应商锁定）与 PLATFORM_ADMIN（全权限角色）
DELETE FROM role_permission rp
 USING role r, permission_point pp
 WHERE rp.role_id = r.id
   AND rp.permission_point_id = pp.id
   AND pp.module_code = 'M5'
   AND pp.action_code = 'E'
   AND r.code IN ('PRICING_OP', 'FINANCE', 'MODEL_OPS', 'RETRO_OP');

-- 2) 成本参数配置改挂 M5:C：PRICING_OP 补 M5:C
--    （PLATFORM_ADMIN 本就持 M5:C；MODEL_OPS 的 M5:C 属历史遗留，见上方「残留」）
INSERT INTO role_permission(role_id, permission_point_id, created_at, updated_at, request_id, created_by, updated_by)
SELECT r.id, pp.id, now(), now(), NULL, 1, 1
  FROM role r, permission_point pp
 WHERE r.code = 'PRICING_OP'
   AND pp.module_code = 'M5'
   AND pp.action_code = 'C'
   AND NOT EXISTS (
         SELECT 1 FROM role_permission x WHERE x.role_id = r.id AND x.permission_point_id = pp.id
       );

-- 3) 配置类归 PRICING_OP：PROCUREMENT 若有 M5:C 则移除（本库当前无，防御其它环境）
DELETE FROM role_permission rp
 USING role r, permission_point pp
 WHERE rp.role_id = r.id
   AND rp.permission_point_id = pp.id
   AND r.code = 'PROCUREMENT'
   AND pp.module_code = 'M5'
   AND pp.action_code = 'C';

-- 4) 汇率锁定归 M10:E：FINANCE 补 M10:E（000007 已给 FINANCE 全部 M10，此处仅幂等兜底）
INSERT INTO role_permission(role_id, permission_point_id, created_at, updated_at, request_id, created_by, updated_by)
SELECT r.id, pp.id, now(), now(), NULL, 1, 1
  FROM role r, permission_point pp
 WHERE r.code = 'FINANCE'
   AND pp.module_code = 'M10'
   AND pp.action_code = 'E'
   AND NOT EXISTS (
         SELECT 1 FROM role_permission x WHERE x.role_id = r.id AND x.permission_point_id = pp.id
       );
