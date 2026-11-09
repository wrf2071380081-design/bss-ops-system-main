-- scripts/demo/suspend-others.sql
--
-- 用途：让「工作台 → 人员」下拉里只剩原型那 5 个人（林舟 / 许澄 / 陈乐 / 周宁 / 沈安）。
--
-- 背景：迁移 000053 只是**新增** 5 个演示人员，老账号（model_ops / buyer_a / smoke_admin …）仍是
-- ACTIVE，所以人员下拉会有 20 项。演示时想"只有 5 个人"，可临时把其余账号挂起；
-- 演示完用下面的 restore 语句恢复。
--
-- ⚠️ 影响：internal_staff.status='SUSPENDED' 的账号**无法登录**，
--    还会影响 worker / 定时任务的办理人解析。**只有确认当前没人用这些账号时才执行。**
--    smoke_admin 是本地联调常用账号，脚本默认保留它，避免把自己锁在门外。
--
-- 执行：
--   docker exec -i model_bss_pg psql -U app -d model_bss < scripts/demo/suspend-others.sql

BEGIN;

-- 1) 挂起非演示人员（保留 5 个演示人员 + smoke_admin）
UPDATE internal_staff
   SET status = 'SUSPENDED', updated_at = now()
 WHERE status = 'ACTIVE'
   AND name NOT IN ('林舟', '许澄', '陈乐', '周宁', '沈安')
   AND id NOT IN (
     SELECT owner_id FROM account
      WHERE portal_type = 'INTERNAL' AND owner_type = 'STAFF' AND login_id = 'smoke_admin'
   );

COMMIT;

-- 恢复（需要时单独执行）：
-- BEGIN;
-- UPDATE internal_staff SET status = 'ACTIVE', updated_at = now() WHERE status = 'SUSPENDED';
-- COMMIT;

-- 自检：
-- SELECT s.name, s.status, a.login_id FROM internal_staff s
--   LEFT JOIN account a ON a.owner_type='STAFF' AND a.owner_id=s.id
--  WHERE s.status='ACTIVE' ORDER BY s.id;
