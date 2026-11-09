DROP INDEX IF EXISTS public.idx_cqa_previous;

ALTER TABLE public.customer_quote_approval
  DROP COLUMN IF EXISTS previous_approval_id;
