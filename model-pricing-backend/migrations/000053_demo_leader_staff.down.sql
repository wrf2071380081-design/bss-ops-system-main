-- 000053 down：撤掉 5 个演示人员与「领导 / 员工」两个角色。
-- 注意：如果这 5 个账号已经被真实业务引用（角色之外的引用不会存在，
-- 但 work_item.assignee / 审计日志会留下姓名快照），删除后历史记录仍保留姓名文本。

DO $$
DECLARE
  v_logins text[] := ARRAY['linzhou','xucheng','chenle','zhouning','shenan'];
BEGIN
  DELETE FROM role_grant
   WHERE staff_id IN (
     SELECT s.id FROM internal_staff s
       JOIN account a ON a.owner_type = 'STAFF' AND a.owner_id = s.id
      WHERE a.portal_type = 'INTERNAL' AND a.login_id = ANY (v_logins)
   );

  DELETE FROM account
   WHERE portal_type = 'INTERNAL' AND owner_type = 'STAFF' AND login_id = ANY (v_logins);

  DELETE FROM internal_staff
   WHERE mobile IN ('13900002001','13900002002','13900002003','13900002004','13900002005');
END $$;

DELETE FROM role_permission
 WHERE role_id IN (SELECT id FROM role WHERE code IN ('LEADER','STAFF'));

DELETE FROM role WHERE code IN ('LEADER','STAFF');
