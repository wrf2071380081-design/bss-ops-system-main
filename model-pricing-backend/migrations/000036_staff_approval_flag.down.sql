-- 000036_staff_approval_flag（down）：移除审批能力字段。
ALTER TABLE internal_staff
  DROP COLUMN IF EXISTS can_approve_supplier;
