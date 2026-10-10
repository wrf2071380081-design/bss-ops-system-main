-- 000031_ops_admin_converge：收敛 OPS_ADMIN（运营管理员）越权的 M11 写权限 + 统一角色显示名。
--
-- 背景（待修复清单 B3 / 46-待办）：`000007_seed` 给 OPS_ADMIN 授予了 M11 的**全部 5 个权限点**
-- （V/E/A/C/P），即运营管理员**能改权限树**——而需求文档 §1.2 给它的职责是
-- 「M1–M9 查看 + 工作台全部」，数据域 ALL（决策 D34：只读为主）。属实质越权。
--
-- 裁决口径（2026-10-09）：需求文档那行是「核心职责概览」、有省略。严格照字面收敛会把
-- M10:V / M11:V / M12:V 一起砍掉，导致运营管理员看不到财务 / 权限 / 审计三个模块。
-- 故**只删写权限（E/A/C/P），保留全部查看权限**，数据域仍为 ALL、不动。
--
-- 「运营管理员」与「运维管理员」是**同一个角色**（role.code 都是 OPS_ADMIN），
-- 只是 000007_seed 把名称写成了「运维管理员」、需求文档写「运营管理员」——命名漂移，本批统一为「运营管理员」。
--
-- 影响面：只动 role_permission（OPS_ADMIN × M11 的 4 行）与 role.name（1 行），
-- **不动其它角色的任何授权**，也不动 permission_point 字典本身（故 ValidateInDB 不受影响）。
--
-- ⚠️ 生效条件：会话快照在登录时生成（perms = 角色并集），已登录会话仍持旧 perms，
--    **必须重新登录**才生效（与批次 3a 同一口径）。
--
-- ⚠️ 当前 M11 在后端**零接口挂载**（组织与权限接口属批次 5，尚未实现），
--    故本批是**预防性收敛**——收掉未授权的能力，避免批次 5 实现接口时直接暴露。
--
-- 幂等：DELETE 与 UPDATE 均天然幂等。

-- 1) 只删 M11 的写权限，保留 M11:V（以及 M10:V / M12:V / M1–M9:V 全部不动）
DELETE FROM role_permission rp
 USING role r, permission_point pp
 WHERE rp.role_id = r.id
   AND rp.permission_point_id = pp.id
   AND r.code = 'OPS_ADMIN'
   AND pp.module_code = 'M11'
   AND pp.action_code IN ('E', 'A', 'C', 'P');

-- 2) 角色显示名统一为需求文档口径「运营管理员」（原「运维管理员」）
UPDATE role
   SET name = '运营管理员', updated_at = now()
 WHERE code = 'OPS_ADMIN';
