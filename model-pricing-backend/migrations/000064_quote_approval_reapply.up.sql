-- 000064_quote_approval_reapply：关联驳回模型重报（原型「关联驳回模型新申请」）。
--
-- 原型里销售可以对「已驳回 / 部分批准」的申请再来一次，但只能针对**被驳回的那几个模型**，
-- 新申请要能追溯回原申请的驳回依据（谁驳的、什么原因）。我们以报价单为审批对象，
-- 对应做法是：销售用新一版报价单（可改价、可换来源）重新提交，审批单上记住「从哪张审批来的」。
--
-- 为什么要存这个关联，而不是让销售随便重报：
--   1. 只允许重报被驳回的模型，避免已批准的模型重复进审批、重复派待办；
--   2. 审批记录能显示「关联自 #12 的驳回」，审核人看得到上一轮驳回原因。

ALTER TABLE public.customer_quote_approval
  ADD COLUMN IF NOT EXISTS previous_approval_id bigint NULL
    REFERENCES public.customer_quote_approval(id);

COMMENT ON COLUMN public.customer_quote_approval.previous_approval_id IS
  '重报来源：本次审批针对上一张审批单（PARTIAL/REJECTED）里被驳回的模型；普通首次提交为 NULL。';

CREATE INDEX IF NOT EXISTS idx_cqa_previous
  ON public.customer_quote_approval(previous_approval_id)
  WHERE previous_approval_id IS NOT NULL;
