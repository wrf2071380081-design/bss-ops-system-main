-- 000033_target_model：按 Token Ops v0.19 原型重做的目标数据模型（新表，不动旧表）。
--
-- 依据：PM 原型 v0.19（真实渲染逐屏走查）+ 测试平台 open-api v1 + 已确认口径。
--   · 三层主线：standard_model → resource → customer_link
--   · 读取全员可见，操作按绑定人（sales_id/buyer_id/assignee_id），无角色权限校验
--   · 无审批；客户无账号；税率不参与报价/毛利
--   · 成本 = 官方价 × 采购系数；毛利 g = 1 − c/s；<3% 二次确认留痕
--   · 正式接口配置与测试凭据分开；测试报告保留多条
--   · 操作记录一等公民：每个写接口同事务写 operation_log
--
-- 命名避让：不复用旧表（supplier_profile/customer_profile/customer_quote/quote_item/audit_log 等）。
-- 人员主数据沿用 internal_staff，本迁移不重建 staff。
-- 兼容：PostgreSQL 14+（不使用 PG15 专有语法）。

-- ============================================================
-- A. 主数据：标准模型库 + 官方资料拉取
-- ============================================================

CREATE TABLE standard_model (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  company            varchar(64)  NOT NULL,                 -- 厂商：DeepSeek/阿里云/OpenAI/Google…
  model_type         varchar(16)  NOT NULL,                 -- TEXT/IMAGE/VIDEO/AUDIO
  name               varchar(128) NOT NULL,                 -- 官方标准名，如 DeepSeek-V3
  official_id        varchar(128) NOT NULL,                 -- 官方 ID，如 deepseek-chat
  input_modalities   text[]       NOT NULL DEFAULT '{}',   -- 输入：文本/图片/音频/视频
  output_modalities  text[]       NOT NULL DEFAULT '{}',
  capacity           jsonb        NOT NULL DEFAULT '{}'::jsonb,  -- 上下文/最大输入输出
  abilities          jsonb        NOT NULL DEFAULT '{}'::jsonb,  -- thinking/FC/SO/streaming/cache
  price_items        jsonb        NOT NULL DEFAULT '[]'::jsonb,  -- [{label,rate,unit,currency,conditions}]
  api_access         jsonb        NOT NULL DEFAULT '{}'::jsonb,  -- 端点/限流/访问条件
  lifecycle          jsonb        NOT NULL DEFAULT '{}'::jsonb,  -- 发布/版本/生命周期
  sources            jsonb        NOT NULL DEFAULT '[]'::jsonb,  -- [{label,url}] 官方来源
  checked_at         date         NULL,                     -- 资料核验日期
  status             varchar(16)  NOT NULL DEFAULT 'ACTIVE',-- DRAFT/ACTIVE/DEPRECATED
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now(),
  created_by         bigint       NULL,
  updated_by         bigint       NULL,
  request_id         varchar(64)  NULL
);
CREATE UNIQUE INDEX uk_standard_model_company_official ON standard_model(company, official_id);
CREATE INDEX idx_standard_model_type ON standard_model(model_type);
CREATE INDEX idx_standard_model_status ON standard_model(status);

CREATE TABLE standard_model_version (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  standard_model_id  bigint      NOT NULL REFERENCES standard_model(id) ON DELETE CASCADE,
  version_no         integer     NOT NULL,
  snapshot           jsonb       NOT NULL,                  -- 当时完整官方资料
  source             varchar(255) NULL,
  checked_at         date        NULL,
  created_by         bigint      NULL,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_standard_model_version ON standard_model_version(standard_model_id, version_no);

CREATE TABLE crawl_job (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  standard_model_id  bigint      NOT NULL REFERENCES standard_model(id) ON DELETE CASCADE,
  source_url         varchar(512) NOT NULL,
  status             varchar(16) NOT NULL DEFAULT 'PENDING', -- PENDING/RUNNING/SUCCESS/FAILED
  attempt_at         timestamptz NULL,
  success_at         timestamptz NULL,
  error              text        NULL,
  raw_hash           varchar(64) NULL,                      -- 页面 hash，未变则不重复抽取
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_crawl_job_model ON crawl_job(standard_model_id, created_at DESC);
CREATE INDEX idx_crawl_job_status ON crawl_job(status);

CREATE TABLE crawl_diff (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  standard_model_id  bigint      NOT NULL REFERENCES standard_model(id) ON DELETE CASCADE,
  field              varchar(64) NOT NULL,
  old_value          jsonb       NULL,
  new_value          jsonb       NULL,
  source_url         varchar(512) NULL,
  quote              text        NULL,                      -- 原文片段（审计可追溯）
  confidence         numeric(4,3) NULL,                     -- 0~1，LLM 抽取置信度
  status             varchar(16) NOT NULL DEFAULT 'PENDING',-- PENDING/ACCEPTED/REJECTED
  reviewed_by        bigint      NULL,
  reviewed_at        timestamptz NULL,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_crawl_diff_model ON crawl_diff(standard_model_id, status);

-- ============================================================
-- B. 供应商
-- ============================================================

CREATE TABLE supplier (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name               varchar(128) NOT NULL,
  region             varchar(16)  NOT NULL,                 -- CN/OVERSEAS（一资源一地，主体也一地）
  registration_no    varchar(64)  NULL,
  contact            varchar(64)  NULL,
  phone              varchar(32)  NULL,
  established        boolean      NOT NULL DEFAULT false,
  tax_rate           numeric(6,2) NULL,                     -- 仅资料，不参与报价/毛利
  settlement_currency varchar(8)  NULL,                     -- CNY/USD…
  credit_amount      numeric(20,2) NULL,                    -- 供应商→我方 授信额度
  website            varchar(255) NULL,
  account            varchar(64)  NULL,
  password_enc       varchar(255) NULL,                     -- 加密存储
  status             varchar(16)  NOT NULL DEFAULT 'ACTIVE',-- ACTIVE/FROZEN
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now(),
  created_by         bigint       NULL,
  updated_by         bigint       NULL,
  request_id         varchar(64)  NULL
);
CREATE UNIQUE INDEX uk_supplier_name_region ON supplier(name, region);

CREATE TABLE supplier_formal_config (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  supplier_id        bigint       NOT NULL REFERENCES supplier(id) ON DELETE CASCADE,
  scope              varchar(255) NULL,                     -- 适用模型/范围说明
  base_url           varchar(512) NOT NULL,
  api_key_enc        varchar(512) NOT NULL,                 -- 加密；与测试凭据分开
  note               text         NULL,
  created_by         bigint       NULL,
  updated_by         bigint       NULL,
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_supplier_formal_config_supplier ON supplier_formal_config(supplier_id);

CREATE TABLE supplier_qualification (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  supplier_id        bigint       NOT NULL REFERENCES supplier(id) ON DELETE CASCADE,
  name               varchar(128) NOT NULL,
  file_ref           varchar(512) NULL,
  uploaded_at        timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_supplier_qualification_supplier ON supplier_qualification(supplier_id);

-- ============================================================
-- C. 供应来源（resource）与测试
-- ============================================================

CREATE TABLE resource (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  standard_model_id  bigint       NOT NULL REFERENCES standard_model(id),
  supplier_id        bigint       NOT NULL REFERENCES supplier(id),
  supplier_model_name varchar(128) NOT NULL,                -- 供方模型名称（供应商自己的叫法）
  standard_model_name_snapshot varchar(128) NOT NULL,       -- 官方名快照
  region             varchar(16)  NOT NULL,                 -- CN/OVERSEAS（一资源一地）
  test_base_url      varchar(512) NULL,                     -- 测试凭据
  test_api_key_enc   varchar(512) NULL,
  coefficient        numeric(8,4) NOT NULL,                 -- 采购系数，成本=官方价×系数
  state              varchar(24)  NOT NULL DEFAULT 'PENDING_TEST',
                     -- PENDING_TEST/PENDING_ADMIT/ADMITTED/OFFLINE/PLAN_OFFLINE/PLAN_ONLINE
  ever_admitted      boolean      NOT NULL DEFAULT false,
  buyer_id           bigint       NULL REFERENCES internal_staff(id),  -- 归属采购人（绑定式操作）
  latest_test_report_id bigint    NULL,                     -- FK 后置（见文末 ALTER）
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now(),
  created_by         bigint       NULL,
  updated_by         bigint       NULL,
  request_id         varchar(64)  NULL,
  CONSTRAINT ck_resource_coefficient CHECK (coefficient > 0 AND coefficient <= 1)
);
CREATE UNIQUE INDEX uk_resource_supplier_model_region ON resource(supplier_id, standard_model_id, region);
CREATE INDEX idx_resource_model ON resource(standard_model_id);
CREATE INDEX idx_resource_state ON resource(state);
CREATE INDEX idx_resource_buyer ON resource(buyer_id);

CREATE TABLE test_job (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  resource_id        bigint       NOT NULL REFERENCES resource(id) ON DELETE CASCADE,
  category           varchar(24)  NOT NULL,                 -- health/baseline/functional/evaluation/intelligence/stress/imagespec
  external_job_id    varchar(64)  NULL,                     -- 测试平台 job_id
  idempotency_key    varchar(128) NULL,
  status             varchar(24)  NOT NULL DEFAULT 'QUEUED',-- QUEUED/SCHEDULED/RUNNING/PAUSED/SUCCEEDED/FAILED/ABORTED/CANCELED
  verdict            varchar(32)  NULL,                     -- PASS/PASS_WITH_WARNINGS/FAIL/ABORT/…
  request_payload    jsonb        NULL,                     -- target/params（密钥脱敏后）
  response_summary   jsonb        NULL,                     -- 平台 summary
  review_of_job_id   bigint       NULL REFERENCES test_job(id), -- 复测来源
  created_by         bigint       NULL,
  created_at         timestamptz  NOT NULL DEFAULT now(),
  finished_at        timestamptz  NULL
);
CREATE UNIQUE INDEX uk_test_job_external ON test_job(external_job_id) WHERE external_job_id IS NOT NULL;
CREATE INDEX idx_test_job_resource ON test_job(resource_id, created_at DESC);
CREATE INDEX idx_test_job_status ON test_job(status);

CREATE TABLE test_report (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  test_job_id        bigint       NOT NULL REFERENCES test_job(id) ON DELETE CASCADE,
  resource_id        bigint       NOT NULL REFERENCES resource(id) ON DELETE CASCADE,
  connection_ms      numeric(10,2) NULL,                    -- 连通耗时
  connection_passed  boolean      NULL,
  stress_passed      boolean      NULL,
  rps                numeric(12,2) NULL,                    -- 压力 RPS
  tps                numeric(14,2) NULL,                    -- 压力 TPS
  availability       numeric(6,4) NULL,
  error_rate         numeric(6,4) NULL,
  ttft_ms            numeric(10,2) NULL,
  tpot_ms            numeric(10,2) NULL,
  sample_count       integer      NULL,
  verdict            varchar(32)  NULL,
  report_url         varchar(1024) NULL,
  report_format      varchar(16)  NULL,                     -- json/md/csv/xlsx
  report_file        varchar(512) NULL,
  scene              text         NULL,
  tested_by          bigint       NULL,
  tested_at          timestamptz  NOT NULL DEFAULT now(),
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_test_report_resource ON test_report(resource_id, tested_at DESC);
CREATE INDEX idx_test_report_job ON test_report(test_job_id);

CREATE TABLE webhook_event (
  event_id           varchar(128) PRIMARY KEY,              -- evt_<job_id>_<status>
  job_id             varchar(64)  NULL,
  payload            jsonb        NOT NULL,
  received_at        timestamptz  NOT NULL DEFAULT now(),
  processed_at       timestamptz  NULL
);

-- resource.latest_test_report_id 后置外键
ALTER TABLE resource
  ADD CONSTRAINT fk_resource_latest_report
  FOREIGN KEY (latest_test_report_id) REFERENCES test_report(id) ON DELETE SET NULL;

-- ============================================================
-- D. 申报与办理（无审批）
-- ============================================================

CREATE TABLE submission (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  supplier_id        bigint       NOT NULL REFERENCES supplier(id),
  kind               varchar(24)  NOT NULL,                 -- SUPPLIER_NEW/MODEL_NEW/PRICE_CHANGE
  region             varchar(16)  NOT NULL,
  status             varchar(24)  NOT NULL DEFAULT 'PROCESSING', -- PROCESSING/COMPLETED/WITHDRAWN（无审批）
  reason             text         NULL,
  submitted_by       bigint       NULL,
  submitted_at       timestamptz  NOT NULL DEFAULT now(),
  completed_at       timestamptz  NULL,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_submission_supplier ON submission(supplier_id, submitted_at DESC);
CREATE INDEX idx_submission_status ON submission(status);

CREATE TABLE submission_row (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  submission_id      bigint       NOT NULL REFERENCES submission(id) ON DELETE CASCADE,
  standard_model_id  bigint       NOT NULL REFERENCES standard_model(id),
  supplier_model_name varchar(128) NOT NULL,
  test_base_url      varchar(512) NULL,
  test_api_key_enc   varchar(512) NULL,
  coefficient        numeric(8,4) NOT NULL,
  participate_test   boolean      NOT NULL DEFAULT false,   -- 参与测试勾选
  test_job_id        bigint       NULL REFERENCES test_job(id),
  test_report_id     bigint       NULL REFERENCES test_report(id),
  row_status         varchar(24)  NOT NULL DEFAULT 'PENDING',
  resource_id        bigint       NULL REFERENCES resource(id), -- 通过后生成资源
  CONSTRAINT ck_submission_row_coefficient CHECK (coefficient > 0 AND coefficient <= 1)
);
CREATE INDEX idx_submission_row_submission ON submission_row(submission_id);
CREATE INDEX idx_submission_row_model ON submission_row(standard_model_id);

CREATE TABLE submission_version (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  submission_id      bigint       NOT NULL REFERENCES submission(id) ON DELETE CASCADE,
  version_no         integer      NOT NULL,
  snapshot           jsonb        NOT NULL,
  created_by         bigint       NULL,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_submission_version ON submission_version(submission_id, version_no);

CREATE TABLE onboarding_flow (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  kind               varchar(16)  NOT NULL,                 -- SUPPLIER/CUSTOMER
  owner_id           bigint       NOT NULL,                 -- supplier.id / customer.id
  status             varchar(24)  NOT NULL DEFAULT 'PROCESSING',
  current_node       varchar(16)  NULL,
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_onboarding_flow_owner ON onboarding_flow(kind, owner_id);

CREATE TABLE onboarding_node (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  flow_id            bigint       NOT NULL REFERENCES onboarding_flow(id) ON DELETE CASCADE,
  node_key           varchar(16)  NOT NULL,                 -- PREP/CONFIG/TEST/CONTRACT/CREDIT/ACCOUNT/EXECUTE
  enabled            boolean      NOT NULL DEFAULT true,
  status             varchar(24)  NOT NULL DEFAULT 'WAITING_PREDECESSOR',
                     -- DISABLED/IN_PROGRESS/DONE/WAITING_PREDECESSOR
  assignee_id        bigint       NULL REFERENCES internal_staff(id), -- 办理人（绑定式）
  completed_at       timestamptz  NULL,
  note               text         NULL
);
CREATE UNIQUE INDEX uk_onboarding_node ON onboarding_node(flow_id, node_key);

CREATE TABLE onboarding_batch (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  flow_id            bigint       NOT NULL REFERENCES onboarding_flow(id) ON DELETE CASCADE,
  supplier_id        bigint       NOT NULL REFERENCES supplier(id),
  models             jsonb        NOT NULL DEFAULT '[]'::jsonb,
  config_done        boolean      NOT NULL DEFAULT false,
  status             varchar(24)  NOT NULL DEFAULT 'PROCESSING',
  completed_at       timestamptz  NULL
);
CREATE INDEX idx_onboarding_batch_flow ON onboarding_batch(flow_id);

-- ============================================================
-- E. 客户与开通
-- ============================================================

CREATE TABLE customer (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name               varchar(128) NOT NULL,
  region             varchar(16)  NOT NULL,                 -- CN/OVERSEAS
  platform           varchar(32)  NULL,                     -- 国内B端/海外B端
  service_status     varchar(16)  NOT NULL DEFAULT 'ONBOARDING', -- SERVING/ONBOARDING
  sales_id           bigint       NULL REFERENCES internal_staff(id), -- 销售归属（绑定式操作）
  settlement_currency varchar(8)  NULL,
  tax_rate           numeric(6,2) NULL,                     -- 仅资料，不参与报价/毛利
  credit_amount      numeric(20,2) NULL,                    -- 我方→客户 授信额度
  contract_status    varchar(16)  NOT NULL DEFAULT 'PROCESSING', -- PROCESSING/ARCHIVED
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now(),
  created_by         bigint       NULL,
  updated_by         bigint       NULL,
  request_id         varchar(64)  NULL
);
CREATE UNIQUE INDEX uk_customer_name_region ON customer(name, region);
CREATE INDEX idx_customer_sales ON customer(sales_id);

CREATE TABLE customer_link (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id        bigint       NOT NULL REFERENCES customer(id),
  resource_id        bigint       NOT NULL REFERENCES resource(id),
  standard_model_id  bigint       NOT NULL REFERENCES standard_model(id),
  state              varchar(24)  NOT NULL DEFAULT 'ACTIVE',
                     -- ACTIVE/PENDING_DISABLE/DISABLE_PENDING/SUPPLY_OFFLINE_STOP
  sale_coefficient   numeric(8,4) NULL,
  quote_snapshot     jsonb        NULL,                     -- 报价快照，历史不重写
  requirements       jsonb        NULL,                     -- 客户要求（rps/accuracy 等）
  activated_at       timestamptz  NULL,
  archived           boolean      NOT NULL DEFAULT false,
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_customer_link_active
  ON customer_link(customer_id, resource_id) WHERE archived = false;
CREATE INDEX idx_customer_link_resource ON customer_link(resource_id);
CREATE INDEX idx_customer_link_state ON customer_link(state);

-- ============================================================
-- F. 报价与合同授信
-- ============================================================

CREATE TABLE quote (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id        bigint       NOT NULL REFERENCES customer(id),  -- 报价单客户不可为空
  customer_name      varchar(128) NOT NULL,
  seller_company     varchar(128) NOT NULL,
  region             varchar(16)  NOT NULL,
  currency           varchar(8)   NOT NULL,
  quote_no           varchar(64)  NOT NULL,
  quote_date         date         NOT NULL,
  contact            varchar(64)  NULL,
  note               text         NULL,
  operator_id        bigint       NULL REFERENCES internal_staff(id),
  status             varchar(16)  NOT NULL DEFAULT 'DRAFT', -- DRAFT/ISSUED/ARCHIVED
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_quote_no ON quote(quote_no);
CREATE INDEX idx_quote_customer ON quote(customer_id, quote_date DESC);

CREATE TABLE quote_line (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  quote_id           bigint       NOT NULL REFERENCES quote(id) ON DELETE CASCADE,
  standard_model_id  bigint       NOT NULL REFERENCES standard_model(id),
  resource_id        bigint       NOT NULL REFERENCES resource(id),
  coefficient_snapshot numeric(8,4) NOT NULL,               -- 采购系数快照
  margin             numeric(8,6) NULL,                     -- 毛利率
  sale_coefficient   numeric(8,4) NULL,                     -- 销售系数
  discount           numeric(6,3) NULL,                     -- 折扣 0~10
  price_items_snapshot jsonb      NULL,
  CONSTRAINT ck_quote_line_margin CHECK (margin IS NULL OR margin < 1),
  CONSTRAINT ck_quote_line_discount CHECK (discount IS NULL OR (discount > 0 AND discount <= 10))
);
CREATE INDEX idx_quote_line_quote ON quote_line(quote_id);
CREATE INDEX idx_quote_line_resource ON quote_line(resource_id);

CREATE TABLE quote_version (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  quote_id           bigint       NOT NULL REFERENCES quote(id) ON DELETE CASCADE,
  version_no         integer      NOT NULL,
  snapshot           jsonb        NOT NULL,
  created_by         bigint       NULL,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_quote_version ON quote_version(quote_id, version_no);

CREATE TABLE contract (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  subject_type       varchar(16)  NOT NULL,                 -- SUPPLIER/CUSTOMER
  subject_id         bigint       NOT NULL,
  status             varchar(16)  NOT NULL DEFAULT 'PROCESSING', -- PROCESSING/ARCHIVED
  signed_at          timestamptz  NULL,
  version            integer      NOT NULL DEFAULT 1,
  file_ref           varchar(512) NULL,
  note               text         NULL,
  created_at         timestamptz  NOT NULL DEFAULT now(),
  updated_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_contract_subject ON contract(subject_type, subject_id);

CREATE TABLE credit_line (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  subject_type       varchar(16)  NOT NULL,                 -- SUPPLIER/CUSTOMER
  subject_id         bigint       NOT NULL,
  direction          varchar(24)  NOT NULL,                 -- SUPPLIER_TO_US/US_TO_CUSTOMER
  amount             numeric(20,2) NOT NULL,
  currency           varchar(8)   NOT NULL,
  version            integer      NOT NULL DEFAULT 1,
  effective_at       timestamptz  NULL,
  note               text         NULL,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_credit_line_subject ON credit_line(subject_type, subject_id, created_at DESC);

-- ============================================================
-- G. 变更计划与公告
-- ============================================================

CREATE TABLE plan (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  resource_id        bigint       NOT NULL REFERENCES resource(id) ON DELETE CASCADE,
  kind               varchar(24)  NOT NULL,                 -- ONLINE/OFFLINE/PRICE_CHANGE
  reason             text         NULL,
  due_at             timestamptz  NULL,
  before             jsonb        NULL,
  after              jsonb        NULL,
  status             varchar(16)  NOT NULL DEFAULT 'PENDING', -- PENDING/DONE/CANCELED
  created_by         bigint       NULL,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_plan_resource ON plan(resource_id, created_at DESC);

CREATE TABLE notice (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  type               varchar(32)  NOT NULL,                 -- ONLINE_OFFLINE_PLAN/RESOURCE_CHANGE/COST_CHANGE
  target_type        varchar(32)  NULL,
  target_id          bigint       NULL,
  payload            jsonb        NOT NULL DEFAULT '{}'::jsonb,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_notice_type ON notice(type, created_at DESC);

CREATE TABLE notice_read (
  notice_id          bigint       NOT NULL REFERENCES notice(id) ON DELETE CASCADE,
  staff_id           bigint       NOT NULL REFERENCES internal_staff(id),
  read_at            timestamptz  NOT NULL DEFAULT now(),
  PRIMARY KEY (notice_id, staff_id)
);

-- ============================================================
-- H. 操作留痕（一等公民）
-- ============================================================

CREATE TABLE operation_log (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  entity_type        varchar(32)  NOT NULL,
  entity_id          bigint       NOT NULL,
  action             varchar(32)  NOT NULL,
  operator_id        bigint       NULL REFERENCES internal_staff(id),
  operator_name_snapshot varchar(64) NULL,
  before             jsonb        NULL,
  after              jsonb        NULL,
  reason             varchar(500) NULL,
  request_id         varchar(64)  NULL,
  trace_id           varchar(64)  NULL,
  created_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_operation_log_entity ON operation_log(entity_type, entity_id, created_at DESC);
CREATE INDEX idx_operation_log_operator ON operation_log(operator_id, created_at DESC);
CREATE INDEX idx_operation_log_created ON operation_log(created_at DESC);

CREATE TABLE assignment_log (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  entity_type        varchar(32)  NOT NULL,                 -- CUSTOMER/RESOURCE/FLOW_NODE
  entity_id          bigint       NOT NULL,
  field              varchar(32)  NOT NULL,                 -- sales_id/buyer_id/assignee_id
  old_staff_id       bigint       NULL,
  new_staff_id       bigint       NULL,
  changed_by         bigint       NULL REFERENCES internal_staff(id),
  changed_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_assignment_log_entity ON assignment_log(entity_type, entity_id, changed_at DESC);

CREATE TABLE margin_override (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  quote_line_id      bigint       NOT NULL REFERENCES quote_line(id) ON DELETE CASCADE,
  margin             numeric(8,6) NOT NULL,
  sale_coefficient   numeric(8,4) NULL,
  confirmed_by       bigint       NOT NULL REFERENCES internal_staff(id),
  confirmed_at       timestamptz  NOT NULL DEFAULT now(),
  reason             varchar(500) NOT NULL
);
CREATE INDEX idx_margin_override_line ON margin_override(quote_line_id);
