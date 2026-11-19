-- ============================================================================
-- 000053_demo_leader_staff 失败修复（2026-11-10）
--
-- 现象：schema_migrations 停在 version=53 dirty=true，报
--   duplicate key value violates unique constraint "uk_account_owner"
--   DETAIL: Key (owner_type, owner_id)=(STAFF, 49) already exists.
--
-- 原因：000053 假设手机号 13900002001–05 是空的，会先按 mobile 复用/新建人员，
--   再给同一个人建 INTERNAL 账号。本库这两个前提都不成立：
--     · staff 49–53 已被 10-21/10-30 的演示数据占用：翟龙龙/杜欢/张三/李四/王五；
--     · 每人已有 INTERNAL 账号（zhailonglong/duhuan/zhangsan/lisi/wangwu）。
--   于是迁移按 mobile 命中已有 staff（ON CONFLICT DO NOTHING → RETURNING 空 → 走 SELECT 分支），
--   紧接着 INSERT account(owner_id=49) 撞 uk_account_owner（一个人只能有一个账号）→ 整块回滚。
--   迁移里没有"人员已存在且有账号"的分支，这是 000053 对历史数据不幂等的地方。
--
-- 处理：删掉这 5 个旧 INTERNAL 账号，让 000053 按原型人员（林舟/许澄/陈乐/周宁/沈安）重建。
--   · staff 行保留（不动 id）：工作台待办、报价审批、公告已读、客户归属都按 staff_id 关联，
--     000053 只会把姓名改成原型人员，引用全部继续有效；
--   · login_session 先删（有外键），旧 token 一律失效，重登即可。
--
-- 备份：工作区 _db_backup_before_migrate_20261110.sql（修复前整库 dump）
-- 迁移版本号不在这里改，用 golang-migrate 语义：migrate force 52
-- 执行：psql -v ON_ERROR_STOP=1 --single-transaction -f fix_migration_53_account_conflict_20261110.sql
-- ============================================================================

BEGIN;

-- 1) 旧账号的会话（外键，先删）
DELETE FROM public.login_session
WHERE account_id IN (
    SELECT id FROM public.account
    WHERE portal_type = 'INTERNAL'
      AND owner_type = 'STAFF'
      AND login_id IN ('zhailonglong', 'duhuan', 'zhangsan', 'lisi', 'wangwu')
);

-- 2) 旧账号本体（5 行；按 login_id 精确匹配，不碰其他账号）
DELETE FROM public.account
WHERE portal_type = 'INTERNAL'
  AND owner_type = 'STAFF'
  AND login_id IN ('zhailonglong', 'duhuan', 'zhangsan', 'lisi', 'wangwu');

-- 3) STAFF 角色先于 000053 就存在（本库 id=35「普通员工」，M1–M12 全 :E/:V），
--    而 000053 的 ON CONFLICT (code) DO NOTHING 只补不删，会留下 7 个多余的 :E
--    （M2 价格同步 / M5 成本 / M6 定价策略 / M7 价目表 / M10 财务 / M11 组织权限 / M12 审计），
--    等于「员工」能改成本参数和角色权限。这里先按 000053 的口径收敛，避免迁移后权限过宽。
--    该角色当前 0 人持有（role_grant 里没有 STAFF），收敛不影响任何现有账号。
UPDATE public.role
SET name = '员工', data_scope = 'ALL', is_functional = true, updated_at = now()
WHERE code = 'STAFF';

DELETE FROM public.role_permission rp
USING public.role r, public.permission_point p
WHERE rp.role_id = r.id
  AND rp.permission_point_id = p.id
  AND r.code = 'STAFF'
  AND NOT (
      p.action_code = 'V'
      OR (p.action_code = 'E' AND p.module_code IN ('M1', 'M3', 'M4', 'M8', 'M9'))
  );

COMMIT;

-- 核对：账号已清空、STAFF 权限点数量应为 12 个 :V + 5 个 :E = 17
SELECT count(*) AS remaining_old_accounts
FROM public.account
WHERE portal_type = 'INTERNAL'
  AND login_id IN ('zhailonglong', 'duhuan', 'zhangsan', 'lisi', 'wangwu');

SELECT count(*) AS staff_permission_points
FROM public.role_permission rp
JOIN public.role r ON r.id = rp.role_id
WHERE r.code = 'STAFF';
