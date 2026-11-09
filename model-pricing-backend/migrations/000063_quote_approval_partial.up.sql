-- 000063_quote_approval_partial：报价审批对齐原型（部分批准 + 审批即派发 + 处理意见 + 撤回原因）。
--
-- 原型（Token Ops）的报价审批不是「审一张单」，而是「超管把这次接入申请逐模型分派出去」：
--   待超管处理 --全部批准--> 已分派   （批准模型立刻进办理）
--              --部分批准--> 部分批准 （批准的先办，驳回的可重报）
--              --全部驳回--> 已驳回
--              --撤回-----> 已撤回（原因必填）
-- 提交结论的同一个动作里，还要指定「技术办理人」和「必要商务办理人」并写处理意见。
--
-- 000057 建的两张表只够「全部批准 / 任一驳回」二选一，本迁移补三样东西：
--   1. 审批头上的两个办理人 + 撤回原因（处理意见复用已有的 note 列）；
--   2. 逐模型行的价目明细快照（原型审批详情里每个模型都能看到"卖多少钱"）；
--   3. PARTIAL 结论的语义说明（status 是 varchar(24)，无 CHECK，不用改约束）。
--
-- 注意：customer_quote.status 新增取值 'PARTIAL'（部分批准），同样无 CHECK 约束，
-- 只在这里把语义写进注释；Go 侧见 customer.QuoteStatusPartial。

-- ============================================================
-- 1. 审批头：两个办理人 + 撤回原因
-- ============================================================
ALTER TABLE public.customer_quote_approval
  ADD COLUMN IF NOT EXISTS ops_assignee_id      bigint NULL REFERENCES public.internal_staff(id),
  ADD COLUMN IF NOT EXISTS business_assignee_id bigint NULL REFERENCES public.internal_staff(id),
  ADD COLUMN IF NOT EXISTS withdrawn_reason     varchar(200) NULL;

COMMENT ON COLUMN public.customer_quote_approval.ops_assignee_id IS
  '技术办理人（原型「有批准模型时的技术办理人」必填）：审批通过后「客户模型开通」待办的负责人。';
COMMENT ON COLUMN public.customer_quote_approval.business_assignee_id IS
  '商务办理人（原型「必要商务办理人」）：客户合同未归档时必填，审批通过后「合同与授信」待办的负责人。';
COMMENT ON COLUMN public.customer_quote_approval.withdrawn_reason IS
  '撤回原因（原型撤回弹窗必填），仅在 status=WITHDRAWN 时有值。';

COMMENT ON COLUMN public.customer_quote_approval.status IS
  'PENDING 待审批 / APPROVED 已分派（全部批准）/ PARTIAL 部分批准 / REJECTED 已驳回 / WITHDRAWN 已撤回';

-- 审批记录按处理时间倒序翻（原型「审批记录」子页签），补一个覆盖索引。
CREATE INDEX IF NOT EXISTS idx_cqa_decided_at
  ON public.customer_quote_approval(decided_at DESC NULLS LAST);

-- ============================================================
-- 2. 逐模型行：价目明细快照
-- ============================================================
-- 原型审批详情每个模型右侧列「报价明细」（输入：USD 1.3 / 百万 tokens …）。
-- 我们的报价明细是「资源 × 官方价项」一行（customer_quote_item.price_item_key），
-- 一个模型有多行；审批行按模型聚合，所以这里存一份当次提交的价目快照
-- [{key,price,currency}]，审批人不用再去翻报价单。
-- 存快照而不是读时联表：审批要能回答"当时批的是哪些价格"，报价单后续改价不能改写历史。
ALTER TABLE public.customer_quote_approval_row
  ADD COLUMN IF NOT EXISTS price_detail jsonb NULL;

COMMENT ON COLUMN public.customer_quote_approval_row.price_detail IS
  '提交时的价目快照：[{key: text_input, price: 1.30, currency: CNY}]；老数据/无资源口径为 NULL。';
