-- 000069：供应商「生产平台服务商」关联（2026-11 版本：供应商主体与平台/部署解耦）
--
-- 背景：旧模型把供应商固定归属国内或海外（supplier.region），于是"国内供应商→国内部署→
-- 国内平台上架→国内客户"四件事被绑死。新口径：供应商是**统一主体**，它在某个生产平台
-- 上"是不是服务商"由人工登记的关联记录决定，一个主体可同时有国内、海外两条；
-- 部署节点由每次申报自行选择（submission.region），客户注册地不参与匹配。
--
-- 纪律：
--   1. 同平台同标识全局唯一 —— 一个生产平台账号不能被两家供应商认领（原型有这条校验）；
--   2. 记录只是"人工登记的事实"，不代表模型接入，也不触发任何审批或资源状态变化；
--   3. 不物理删除历史：移除用 deleted_at 软删，保证操作记录能回溯。
CREATE TABLE IF NOT EXISTS supplier_platform_service (
    id            bigint generated always as identity primary key,
    supplier_id   bigint       not null references supplier (id) on delete cascade,
    platform      varchar(16)  not null,
    service_name  varchar(128) not null,
    identifier    varchar(128) not null,
    status        varchar(16)  not null default 'PENDING',
    note          varchar(500),
    linked_by     bigint       references internal_staff (id),
    confirmed_by  bigint       references internal_staff (id),
    confirmed_at  timestamptz,
    deleted_at    timestamptz,
    created_at    timestamptz  not null default now(),
    updated_at    timestamptz  not null default now(),
    constraint ck_sps_platform check (platform in ('CN','OVERSEAS')),
    constraint ck_sps_status check (status in ('PENDING','CONFIRMED'))
);

-- 同平台同标识只能挂一家（软删的不算占用）
CREATE UNIQUE INDEX IF NOT EXISTS uk_sps_platform_identifier
    ON supplier_platform_service (platform, identifier)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_sps_supplier
    ON supplier_platform_service (supplier_id)
    WHERE deleted_at IS NULL;
