-- 2026-11-16：停用「对应员工已停用」的内网账号（线上已执行，保留以便追溯）
--
-- 背景：收敛演示人员时只改了 internal_staff.status，没同步 account.status，
-- 导致 14 个幽灵账号仍可登录、且带着各自角色（领导在「账号管理」里也能看到它们）。
--
-- 规则：停用满足以下全部条件的账号
--   ① portal_type = 'INTERNAL'
--   ② account.status = 'ACTIVE'
--   ③ 对应 internal_staff.status <> 'ACTIVE'
--   ④ 不是技术验证账号 smoke_admin
-- 保留结果（线上实际）：smoke_admin + linzhou/xucheng/chenle/zhouning/shenan 共 6 个。
--
-- 幂等：重复执行只会命中已经被停用的零行。
BEGIN;

CREATE TEMP TABLE stale ON COMMIT DROP AS
SELECT a.id
FROM account a
JOIN internal_staff s ON s.id = a.owner_id
WHERE a.portal_type = 'INTERNAL'
  AND a.status = 'ACTIVE'
  AND s.status <> 'ACTIVE'
  AND a.login_id <> 'smoke_admin';

UPDATE account SET status = 'DISABLED', updated_at = now() WHERE id IN (SELECT id FROM stale);
-- 会话一并清掉，避免已登录的人继续拿着旧 token
DELETE FROM login_session WHERE account_id IN (SELECT id FROM stale);

SELECT count(*) AS disabled_now FROM stale;

COMMIT;

-- 核对：应只剩 smoke_admin 与 5 位演示人员
SELECT a.login_id, s.name, a.status
FROM account a JOIN internal_staff s ON s.id = a.owner_id
WHERE a.portal_type = 'INTERNAL' AND a.status = 'ACTIVE'
ORDER BY a.login_id;
