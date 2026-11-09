-- 000054 down：撤掉逐模型决策列。
DROP INDEX IF EXISTS public.idx_submission_row_decision;

ALTER TABLE public.submission_row
  DROP CONSTRAINT IF EXISTS ck_submission_row_decision;

ALTER TABLE public.submission_row
  DROP COLUMN IF EXISTS decision,
  DROP COLUMN IF EXISTS decided_at,
  DROP COLUMN IF EXISTS decided_by;
