-- ============================================================================
-- 客户自助门户下线（2026-10-30，产品裁决：平台不提供客户登录）
--
-- 三处保持一致：
--   ① 账号：把 CUSTOMER 门户的存量账号置为 SUSPENDED（登录被拒，见
--      internal/domain/auth/service.go 的 IsSubjectFrozen / Status != ACTIVE 两道闸）；
--   ② 接口：features.customer_portal=false 时，/api/customer 的 7 条业务路由统一 404
--      （登录/登出保留，不在 middleware/feature.go 的拦截清单里）；
--   ③ 前端：已摘掉 /customer 路由挂载（src/router/index.ts），登录页本来就没有客户入口。
--
-- 不删账号、不删代码：客户登录下一期若要开放，把 status 改回 ACTIVE、
-- features.customer_portal 置 true、恢复前端路由挂载即可。
--
-- ⚠️ 连带影响（已知并接受）：POST /api/customer/quotes/:id/accept 在拦截清单里，
--    关掉后客户报价无法由客户侧接受，只能停在「已通过（APPROVED）」。
--    「内部代客接受报价」接口尚未实现，属待补产品缺口。
--
-- 执行：psql -v ON_ERROR_STOP=1 -f suspend_customer_portal_20261030.sql
-- ============================================================================

BEGIN;

UPDATE public.account
SET status = 'SUSPENDED', updated_at = now()
WHERE portal_type = 'CUSTOMER'
  AND status = 'ACTIVE';

COMMIT;

-- 核对：不应再有 ACTIVE 的客户门户账号
SELECT id, login_id, portal_type, status
FROM public.account
WHERE portal_type = 'CUSTOMER'
ORDER BY id;
