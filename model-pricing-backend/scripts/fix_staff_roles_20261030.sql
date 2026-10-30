-- ============================================================================
-- 演示账号补角色（2026-10-30）
--
-- 背景：13900002001–05（翟龙龙/杜欢/张三/李四/王五）在 role_grant 里没有记录，
--       登录快照 roles=[] / perms=[]，除工作台外所有业务接口 403。
-- 口径：
--   翟龙龙  平台管理员（领导，can_approve_supplier，验收全量界面用）
--   杜欢    模型运营 + 定价运营（多角色并集：permissions 取并集、data_scope 取较宽）
--   张三    销售（同时是客户归属人 owner_sales_operator_id=其 staff_id）
--   李四    运营管理员
--   王五    财务
-- 幂等：按 internal_staff.mobile 定位（内部门户 login_id 线上是手机号、本地是拼音，不可依赖），
--       已存在同 (staff_id, role_id) 的不重复插。
-- 注意：改角色后必须重新登录，权限快照是登录时注入会话的。
-- 执行：psql -v ON_ERROR_STOP=1 --single-transaction -f fix_staff_roles_20261030.sql
-- ============================================================================

BEGIN;

-- 1) 清掉兜底角色 STAFF 的授权。该角色只存在于部分环境（不在 pkg/perm 里），
--    权限矩阵是 M1–M12 全 E/V，比任何职能角色都宽，留着会把「只有工作台」修成「什么都能改」。
DELETE FROM role_grant
WHERE staff_id IN (
    SELECT id FROM internal_staff
    WHERE mobile IN ('13900002001','13900002002','13900002003','13900002004','13900002005')
  )
  AND role_id IN (SELECT id FROM role WHERE code = 'STAFF');

-- 2) 按职能授予角色
INSERT INTO role_grant (staff_id, role_id, granted_by, granted_at, created_at, updated_at)
SELECT s.id,
       r.id,
       COALESCE(
           (SELECT a.owner_id FROM account a WHERE a.login_id = 'smoke_admin' LIMIT 1),
           (SELECT min(x.id) FROM internal_staff x)
       ),
       now(), now(), now()
FROM (VALUES
        ('13900002001', 'PLATFORM_ADMIN'),
        ('13900002002', 'MODEL_OPS'),
        ('13900002002', 'PRICING_OP'),
        ('13900002003', 'SALES'),
        ('13900002004', 'OPS_ADMIN'),
        ('13900002005', 'FINANCE')
     ) AS m(mobile, role_code)
JOIN internal_staff s ON s.mobile = m.mobile
JOIN role r ON r.code = m.role_code
WHERE NOT EXISTS (
    SELECT 1 FROM role_grant g WHERE g.staff_id = s.id AND g.role_id = r.id
);

COMMIT;

-- 核对：五个人的角色并集
SELECT s.name, s.mobile, array_agg(r.code ORDER BY r.code) AS roles
FROM internal_staff s
LEFT JOIN role_grant g ON g.staff_id = s.id
LEFT JOIN role r ON r.id = g.role_id
WHERE s.mobile IN ('13900002001','13900002002','13900002003','13900002004','13900002005')
GROUP BY s.id, s.name, s.mobile
ORDER BY s.id;
