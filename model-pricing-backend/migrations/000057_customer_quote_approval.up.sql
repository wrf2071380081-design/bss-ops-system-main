-- 000057_customer_quote_approval：客户报价单审批（原型「提交审批 → 超管逐模型批准/驳回」）。
--
-- 背景：customer_quote.status 里一直留着 PENDING/APPROVED 两个值，但从来没有代码写过它们
-- （见 000005 DDL 注释与 internal/domain/customer/service.go 的状态常量）——设计留了口子，
-- 实现没跟上。原型里销售「提交审批」后由超管逐模型批准/驳回，驳回要填原因，
-- 批准后这批模型才能进入接入办理。
--
-- 与 customer_quote_status 的关系：
--   DRAFT --submit--> PENDING --全部批准--> APPROVED
--                              --任一驳回--> DRAFT（可改价后重新提交）
--                              --撤回-----> DRAFT
--
-- 与「特价审批」（change_request[SPECIAL_PRICE]，两步 PRICING_OP → FINANCE）**不是一回事**：
-- 特价批的是「允许破线」，这里是「这张报价单这批模型可以对外」。两者并存。

CREATE TABLE public.customer_quote_approval (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_quote_id bigint       NOT NULL REFERENCES public.customer_quote(id),
  -- PENDING / APPROVED / REJECTED / WITHDRAWN
  status            varchar(16)  NOT NULL DEFAULT 'PENDING',
  model_count       integer      NOT NULL DEFAULT 0,
  note              varchar(200) NULL,
  submitted_by      bigint       NOT NULL,
  submitted_at      timestamptz  NOT NULL DEFAULT now(),
  decided_by        bigint       NULL,
  decided_at        timestamptz  NULL,
  request_id        varchar(64)  NULL,
  created_at        timestamptz  NOT NULL DEFAULT now(),
  updated_at        timestamptz  NOT NULL DEFAULT now()
);

-- 同一张报价单同时只能有一个待审批单（重复提交 → 唯一冲突，服务层翻成业务文案）。
CREATE UNIQUE INDEX uk_cqa_pending
  ON public.customer_quote_approval(customer_quote_id) WHERE status = 'PENDING';
CREATE INDEX idx_cqa_quote ON public.customer_quote_approval(customer_quote_id, submitted_at DESC);
CREATE INDEX idx_cqa_submitted_by ON public.customer_quote_approval(submitted_by);

-- 逐模型决定行。粒度是「标准模型」，不是「价项」：
-- 一个模型可能有 text_input / text_output 多个价项，审批人看的是一个模型的可售结论。
CREATE TABLE public.customer_quote_approval_row (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  approval_id       bigint        NOT NULL REFERENCES public.customer_quote_approval(id) ON DELETE CASCADE,
  -- 资源口径明细能 join 出标准模型；老 SKU 口径明细没有，记 0 并在前端显示「未关联标准模型」。
  standard_model_id bigint        NOT NULL DEFAULT 0,
  model_name        varchar(128)  NOT NULL DEFAULT '',
  official_id       varchar(128)  NOT NULL DEFAULT '',
  item_count        integer       NOT NULL DEFAULT 0,
  price_min         numeric(20,8) NULL,
  price_max         numeric(20,8) NULL,
  -- PENDING / APPROVED / REJECTED
  result            varchar(16)   NOT NULL DEFAULT 'PENDING',
  reason            varchar(200)  NULL,
  decided_by        bigint        NULL,
  decided_at        timestamptz   NULL,
  sort_no           integer       NOT NULL DEFAULT 0,
  CONSTRAINT ck_cqar_result CHECK (result IN ('PENDING', 'APPROVED', 'REJECTED'))
);

CREATE INDEX idx_cqar_approval ON public.customer_quote_approval_row(approval_id, sort_no);
