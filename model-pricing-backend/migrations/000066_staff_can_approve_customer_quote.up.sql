-- 客户报价审批权：把「谁能审客户报价」从权限点 M9:A 收窄成一个显式标记位。
--
-- 为什么不在 M9:A 上收：M9:A 是「客户报价」这个功能域的审批权限点，种子数据里
-- 平台管理员、领导、销售都持着它（销售也能看到审批队列，甚至能点批）。
-- 但业务口径是「报价审批只有超管处理」——审批人看到别人的报价、批价格，
-- 不该由销售角色顺带获得。所以照抄供应商侧的 can_approve_supplier 做法，
-- 单独一个标记位：谁能审，是配置出来的，不是角色推导出来的。
--
-- 回填口径：平台管理员（PLATFORM_ADMIN）与领导（LEADER）。
-- 这两个角色才是「超管/管理岗」，销售、普通员工即使有 M9:A 也不在此列。
ALTER TABLE public.internal_staff
    ADD COLUMN IF NOT EXISTS can_approve_customer_quote boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN public.internal_staff.can_approve_customer_quote IS
    '客户报价审批权：true 才能看到报价审批待办并提交逐模型结论（原型「超管审批」）。';

UPDATE public.internal_staff st
   SET can_approve_customer_quote = true
 WHERE st.status = 'ACTIVE'
   AND EXISTS (
        SELECT 1
          FROM public.role_grant rg
          JOIN public.role r ON r.id = rg.role_id
         WHERE rg.staff_id = st.id
           AND r.code IN ('PLATFORM_ADMIN', 'LEADER')
   );

-- 已登录的会话不会重新装配快照：它们的 role_snapshot 里没有这个新字段，
-- 前端会一直按 false 处理（页签消失），直到用户重新登录。
-- 这里把现存会话的快照补上，避免"改完要所有人重登一次"。
UPDATE public.login_session s
   SET role_snapshot = jsonb_set(
           s.role_snapshot,
           '{can_approve_customer_quote}',
           to_jsonb(COALESCE(st.can_approve_customer_quote, FALSE)),
           TRUE)
  FROM public.internal_staff st
 WHERE s.operator_type = 'STAFF'
   AND st.id = NULLIF(s.role_snapshot ->> 'staff_id', '')::bigint
   AND s.expires_at > now();
