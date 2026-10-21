-- 000036_staff_approval_flag：员工审批能力（供应商审批）。
--
-- 2026-10-21 口径：只有 2-3 个领导有供应商审批权；员工只分领导/普通员工。
-- 采用布尔字段（不建 capability 表）：当前只有一个能力，简单、快照直读、无 JOIN。
-- 审批接口服务端仍需再查一次本字段，不能只依赖前端菜单。

ALTER TABLE internal_staff
  ADD COLUMN IF NOT EXISTS can_approve_supplier boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN internal_staff.can_approve_supplier IS '是否具有供应商审批权（领导=true，普通员工=false）';
