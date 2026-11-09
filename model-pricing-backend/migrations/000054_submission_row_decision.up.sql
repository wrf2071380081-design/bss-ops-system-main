-- 000054_submission_row_decision：申报审批改为「逐模型决策」（原型对齐）。
--
-- 原型：审批页每行一个「决定」勾选框，勾=通过、不勾=不通过，底部计数
-- 「通过 0 个 · 不通过 2 个」，只有通过的行进入技术接入。
-- 代码此前是整单通过/整单驳回。
--
-- 设计要点：**不复用 row_status**。row_status 是办理状态机
-- （PENDING → CONFIGURED → ADMITTED → COMPLETED / CANCELED），混进审批结论
-- 会让「已驳回」和「本次办理已取消」纠缠，历史数据也无法区分是没批还是没办。
-- 因此单开 decision 一列，只表达审批结论。

ALTER TABLE public.submission_row
  ADD COLUMN IF NOT EXISTS decision   varchar(16) NOT NULL DEFAULT 'PENDING',
  ADD COLUMN IF NOT EXISTS decided_at timestamptz NULL,
  ADD COLUMN IF NOT EXISTS decided_by bigint      NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'ck_submission_row_decision'
  ) THEN
    ALTER TABLE public.submission_row
      ADD CONSTRAINT ck_submission_row_decision
      CHECK (decision IN ('PENDING', 'APPROVED', 'REJECTED'));
  END IF;
END $$;

-- 存量数据：已经进入办理或已完成的申报，其行按「通过」回填，
-- 否则历史单子在新界面里会显示成「待决策」，且办理页会把它们当成未审行。
UPDATE public.submission_row r
   SET decision = 'APPROVED',
       decided_at = COALESCE(s.updated_at, now())
  FROM public.submission s
 WHERE r.submission_id = s.id
   AND r.decision = 'PENDING'
   AND s.status IN ('PROCESSING', 'COMPLETED');

CREATE INDEX IF NOT EXISTS idx_submission_row_decision
  ON public.submission_row(submission_id, decision);

COMMENT ON COLUMN public.submission_row.decision IS '审批结论：PENDING 未决策 / APPROVED 通过 / REJECTED 不通过；不影响 row_status 办理状态机';
