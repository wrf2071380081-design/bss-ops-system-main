-- 000070：内部人员的业务岗位（2026-11 版本）
--
-- 口径：业务岗位（运营/商务/销售）用于**展示与职责沟通**，与系统权限（角色/权限点）分开建模。
-- 多岗位不会自动授予超管、也不改变审批与办理权限——权限仍按 role_permission 与指派关系算。
CREATE TABLE IF NOT EXISTS internal_staff_position (
    staff_id   bigint      not null references internal_staff (id) on delete cascade,
    position   varchar(16) not null,
    created_at timestamptz not null default now(),
    primary key (staff_id, position),
    constraint ck_staff_position check (position in ('OPS','BIZ','SALES'))
);
CREATE INDEX IF NOT EXISTS idx_staff_position_position ON internal_staff_position (position);
