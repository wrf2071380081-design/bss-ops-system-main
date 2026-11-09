-- 000049_customer_resource_link：客户「接入模型」绑定（G4 地基）。
--
-- 为什么新建独立表，而不是复用 000033 的 customer / customer_link：
--   1. customer_link.customer_id 指向 customer(id)，而客户真身是 customer_profile(id)
--      （000002 主键）。000033 那套 customer/quote 表在 Go 侧零引用，口径对不上，
--      继续往里写只会让「客户到底是谁」这件事继续分裂。
--   2. 客户真正购买的是「某供应商在某部署节点提供的某个标准模型」——即供应域
--      resource（带采购系数、正式配置、测试证据、可报价状态）。只有直挂
--      customer_profile 并记 resource_id，才能和供应域上下架联动。
--
-- 口径（用户 2026-11-03 拍板）：
--   - 一个客户 + 一个标准模型，同时只能有 1 条生效来源（ACTIVE）→ uk_crl_customer_model
--   - 一个客户 + 一条供应资源，同时只能有 1 条未归档绑定 → uk_crl_customer_resource
--   - 供应资源下架不自动停用客户：先打 SUPPLY_OFFLINE_PENDING 标记，人工确认后才归档
--
-- 状态机见 internal/domain/customer/link.go（唯一定义处，灰度靠它而不是靠注释）。

CREATE TABLE public.customer_resource_link (
  id                    bigserial    PRIMARY KEY,
  customer_profile_id   bigint       NOT NULL REFERENCES public.customer_profile(id) ON DELETE RESTRICT,
  standard_model_id     bigint       NOT NULL REFERENCES public.standard_model(id) ON DELETE RESTRICT,
  resource_id           bigint       NOT NULL REFERENCES public.resource(id) ON DELETE RESTRICT,
  -- 来源报价：开通时按哪张客户报价成交（报价可能先于绑定创建，故允许为空，且不做外键，
  -- 避免报价侧生命周期（作废/重出）反过来卡住已生效的客户来源）。
  quote_id              bigint       NULL,
  state                 varchar(24)  NOT NULL,
  -- 进入 SUPPLY_OFFLINE_PENDING 之前的状态（ACTIVE / DISABLED）。
  -- 资源重新上架时要「回原状态」：没有这一列就只能猜，会把本来已被客户禁用的来源
  -- 偷偷恢复成可用。离开待处理态时清空（见 store 的 ApplyTransition）。
  state_before_offline  varchar(24)  NULL,
  -- 人工留痕：开通说明、禁用/恢复原因、以及「曾进入来源待处理」的痕迹（系统自动追加一行）。
  activation_note       text         NULL,
  activated_at          timestamptz  NULL,
  -- 归档 = 该绑定彻底退出（人工确认停用、或重新配置来源时归档旧的）。
  -- 归档行保留作历史，唯一索引都带 archived = FALSE 谓词，所以归档即可释放名额。
  archived              boolean      NOT NULL DEFAULT false,
  created_by            bigint       NOT NULL,
  created_at            timestamptz  NOT NULL DEFAULT now(),
  updated_at            timestamptz  NOT NULL DEFAULT now(),
  revision              integer      NOT NULL DEFAULT 1,
  request_id            varchar(64)  NULL,
  CONSTRAINT ck_crl_state CHECK (state IN (
    'ACTIVE',                -- 服务中：客户可用该来源
    'DISABLE_PENDING',       -- 客户请求禁用，待人工确认（此时仍可用）
    'DISABLED',              -- 已禁用：客户不能用该来源
    'RESTORE_PENDING',       -- 客户请求恢复，待人工确认
    'SUPPLY_OFFLINE_PENDING',-- 供应资源已下架，待人工确认停用（客户仍可用，必须能在查询里识别）
    'SUPPLY_OFFLINE'         -- 已确认停用，随 archived = TRUE 一起出现
  )),
  CONSTRAINT ck_crl_state_before_offline CHECK (
    state_before_offline IS NULL OR state_before_offline IN ('ACTIVE', 'DISABLED')
  ),
  CONSTRAINT ck_crl_revision CHECK (revision > 0),
  CONSTRAINT ck_crl_quote CHECK (quote_id IS NULL OR quote_id > 0)
);

-- 同一客户 + 同一供应资源：只能有 1 条未归档绑定。
CREATE UNIQUE INDEX uk_crl_customer_resource
  ON public.customer_resource_link(customer_profile_id, resource_id)
  WHERE archived = FALSE;

-- 同一客户 + 同一标准模型：只能有 1 条生效来源（用户拍板的硬约束）。
-- 注意谓词同时带 archived = FALSE AND state = 'ACTIVE'：
-- 已归档的历史记录、以及 DISABLED/待处理记录都不占名额，否则「换一个来源」
-- 会因为旧绑定还没删而建不出来。
CREATE UNIQUE INDEX uk_crl_customer_model
  ON public.customer_resource_link(customer_profile_id, standard_model_id)
  WHERE archived = FALSE AND state = 'ACTIVE';

-- 按资源反查：供应资源下架时要能找到受影响的客户绑定（下一轮下架联动用）。
CREATE INDEX idx_crl_resource ON public.customer_resource_link(resource_id);

-- 客户详情「接入模型」Tab 的主查询路径。
CREATE INDEX idx_crl_customer
  ON public.customer_resource_link(customer_profile_id)
  WHERE archived = FALSE;

COMMENT ON TABLE public.customer_resource_link IS
  '客户「接入模型」绑定：客户买到的某个标准模型由哪条供应资源提供。一客户一模型同时只能有一条 ACTIVE（生效来源）。';
COMMENT ON COLUMN public.customer_resource_link.customer_profile_id IS
  '客户档案（customer_profile.id）。注意不是遗留的 customer(id)——000033 那套表 Go 侧未引用。';
COMMENT ON COLUMN public.customer_resource_link.standard_model_id IS
  '标准模型（standard_model.id）。与 resource.standard_model_id 必须一致，由领域层校验后才落库。';
COMMENT ON COLUMN public.customer_resource_link.resource_id IS
  '供应资源（resource.id）：某供应商在某部署节点提供的该模型的实例。';
COMMENT ON COLUMN public.customer_resource_link.quote_id IS
  '开通依据的客户报价（customer_quote.id），可为空；有意不做外键，避免报价作废反压已生效绑定。';
COMMENT ON COLUMN public.customer_resource_link.state IS
  '绑定状态：ACTIVE 服务中 / DISABLE_PENDING 禁用待确认（仍可用）/ DISABLED 已禁用 / RESTORE_PENDING 恢复待确认 / SUPPLY_OFFLINE_PENDING 来源已下架待人工处理（客户仍可用）/ SUPPLY_OFFLINE 已确认停用。';
COMMENT ON COLUMN public.customer_resource_link.state_before_offline IS
  '进入 SUPPLY_OFFLINE_PENDING 前的状态（ACTIVE 或 DISABLED）。资源重新上架时据此「回原状态」，避免把已被禁用的来源误恢复为可用。';
COMMENT ON COLUMN public.customer_resource_link.activation_note IS
  '人工/系统留痕：开通说明、禁用或恢复原因；系统追加「供应资源下架→待处理」与「资源重新上架→回原状态」的痕迹。';
COMMENT ON COLUMN public.customer_resource_link.activated_at IS
  '最近一次进入 ACTIVE（服务中）的时间。';
COMMENT ON COLUMN public.customer_resource_link.archived IS
  '归档标记：TRUE 表示该绑定已退出（人工确认停用 / 换来源时归档旧的）。归档行保留历史，且不占用任何唯一索引。';
COMMENT ON COLUMN public.customer_resource_link.created_by IS
  '创建操作员（internal_staff.id；系统代建时为 0）。';
COMMENT ON COLUMN public.customer_resource_link.revision IS
  '乐观锁版本，每次状态迁移 +1。客户端提交动作必须回传 expected_revision，冲突返回 409。';
