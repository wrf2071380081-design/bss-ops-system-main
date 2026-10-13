-- 000032_role_account_seed (down)：撤销本迁移建的 8 个账号。
--
-- 删除顺序遵循外键依赖（自底向上）：
--   login_session（→ account）
--   → role_grant（→ internal_staff）
--   → account
--   → internal_staff
--
-- ⚠️ 000027_admin_seed.down 漏了 login_session，只要有会话残留就必然报
--    `update or delete on table "account" violates foreign key constraint
--     "login_session_account_id_fkey"`，并把库留在 dirty 状态。
--    本批显式补上；历史迁移不回头改（约定：只新增 up/down 对）。
--
-- ⚠️ 说明：up 对「已存在的同名 login_id」会整体跳过，因此 down 按 login_id 无条件删除时，
--    可能删掉**人工预先种过的同名账号**。本批 8 个 login_id 均为本迁移引入的新名字，
--    可接受；若将来有人工账号撞名需先人工核对。

DELETE FROM login_session
 WHERE account_id IN (
       SELECT a.id FROM account a
        WHERE a.portal_type = 'INTERNAL'
          AND a.login_id IN ('model_ops','pricing_op','finance_op','sales_op',
                             'procure_op','ops_admin','retro_op','audit_view')
     );

DELETE FROM role_grant
 WHERE staff_id IN (
       SELECT s.id FROM internal_staff s
        WHERE s.mobile IN ('13900001001','13900001002','13900001003','13900001004',
                           '13900001005','13900001006','13900001007','13900001008')
     );

DELETE FROM account
 WHERE portal_type = 'INTERNAL'
   AND login_id IN ('model_ops','pricing_op','finance_op','sales_op',
                    'procure_op','ops_admin','retro_op','audit_view');

DELETE FROM internal_staff
 WHERE mobile IN ('13900001001','13900001002','13900001003','13900001004',
                  '13900001005','13900001006','13900001007','13900001008');
