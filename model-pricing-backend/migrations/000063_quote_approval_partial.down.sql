ALTER TABLE public.customer_quote_approval_row DROP COLUMN IF EXISTS price_detail;

DROP INDEX IF EXISTS public.idx_cqa_decided_at;

ALTER TABLE public.customer_quote_approval
  DROP COLUMN IF EXISTS withdrawn_reason,
  DROP COLUMN IF EXISTS business_assignee_id,
  DROP COLUMN IF EXISTS ops_assignee_id;

COMMENT ON COLUMN public.customer_quote_approval.status IS NULL;
