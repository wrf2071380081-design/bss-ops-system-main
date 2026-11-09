-- 000065_quote_approval_draft：逐模型审批的「保存进度」（原型弹窗里的草稿）。
--
-- 原型弹窗有「保存进度」：审批人填一半（几个模型已决定、办理人已选、意见写了一半）可以先存，
-- 关掉再回来接着填。存的是"审批人的填写状态"，不是审批结论，所以：
--   - 独立存在 approval 行的 draft 列里，不碰逐模型行的 result；
--   - 提交结论（decision）时清空；
--   - 只有审批人本人能写，读的时候同单可见（换人接手能看到上一人填到哪）。
--
-- 为什么用 jsonb 而不是建表：草稿是"一次性整体覆盖"的，不需要版本、不需要按字段查询，
-- 建表只会多一次 join 和一堆无意义的历史行。

ALTER TABLE public.customer_quote_approval
  ADD COLUMN IF NOT EXISTS draft jsonb NULL,
  ADD COLUMN IF NOT EXISTS draft_saved_by bigint NULL REFERENCES public.internal_staff(id),
  ADD COLUMN IF NOT EXISTS draft_saved_at timestamptz NULL;

COMMENT ON COLUMN public.customer_quote_approval.draft IS
  '审批草稿：{"rows":[{"row_id":1,"result":"APPROVED|REJECTED","reason":""}],"note":"","ops_assignee_id":0,"business_assignee_id":0}；提交结论后清空。';
COMMENT ON COLUMN public.customer_quote_approval.draft_saved_by IS '草稿最后保存人（换人接手时能看出上一人填到哪）。';
COMMENT ON COLUMN public.customer_quote_approval.draft_saved_at IS '草稿最后保存时间。';
