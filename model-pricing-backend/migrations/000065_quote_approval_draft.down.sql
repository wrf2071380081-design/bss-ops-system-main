ALTER TABLE public.customer_quote_approval
  DROP COLUMN IF EXISTS draft_saved_at,
  DROP COLUMN IF EXISTS draft_saved_by,
  DROP COLUMN IF EXISTS draft;
