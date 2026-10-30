-- ============================================================================
-- 工作台「待办/已办」演示数据（2026-10-30）
--
-- 背景：todo_task 的办理人全是老种子（staff 1/4/5），13900002001–05 登录后待办/已办为空，
--       四个 tab 里只有待阅有内容。这里把「供应商入驻 · 资源配置」办理节点按领导分配的口径
--       指派给五个人（节点原本挂在冒烟管理员名下），并留一条已完成的节点给张三做「已办」样本。
-- 语义：onboarding_node.assignee_id 就是该节点的办理人，工作台 v_work_item 的
--       TODO 分支直接从这张表展开，状态 DONE 的进「已办」。
-- 幂等：只在办理人仍是冒烟管理员（mobile 13900000001）或为空时才改，重跑不会二次搬动。
-- 注意：onboarding_node 没有 updated_at/updated_by/request_id 审计列，改办理人不会留痕
-- 执行：psql -v ON_ERROR_STOP=1 -f seed_workbench_todo_20261030.sql
-- ============================================================================

BEGIN;

-- 1) 待办：把 CONFIG 环节「进行中」的节点按 id 顺序分给五个人
WITH cand AS (
    SELECT n.id AS node_id, row_number() OVER (ORDER BY n.id) AS rn
    FROM public.onboarding_node n
    WHERE n.enabled
      AND n.node_key = 'CONFIG'
      AND n.status = 'IN_PROGRESS'
      AND (n.assignee_id IS NULL
           OR n.assignee_id = (SELECT id FROM public.internal_staff WHERE mobile = '13900000001'))
), target(rn, mobile) AS (
    VALUES (1::bigint, '13900002001'),
           (2::bigint, '13900002002'),
           (3::bigint, '13900002003'),
           (4::bigint, '13900002004'),
           (5::bigint, '13900002005')
)
UPDATE public.onboarding_node n
SET assignee_id = st.id
FROM cand c
JOIN target t ON t.rn = c.rn
JOIN public.internal_staff st ON st.mobile = t.mobile
WHERE n.id = c.node_id;

-- 2) 已办：把一条已完成的 CONFIG 节点给张三（他「办完过」，进已办 tab）
WITH cand AS (
    SELECT n.id AS node_id
    FROM public.onboarding_node n
    WHERE n.enabled
      AND n.node_key = 'CONFIG'
      AND n.status = 'DONE'
      AND n.assignee_id = (SELECT id FROM public.internal_staff WHERE mobile = '13900000001')
    ORDER BY n.id
    LIMIT 1
)
UPDATE public.onboarding_node n
SET assignee_id = (SELECT id FROM public.internal_staff WHERE mobile = '13900002003')
FROM cand
WHERE n.id = cand.node_id;

COMMIT;

-- 核对：五个人的工作台条目
SELECT s.name, w.item_type, w.state, count(*) AS items
FROM public.v_work_item w
JOIN public.internal_staff s ON s.id = w.assignee_id
WHERE s.mobile IN ('13900002001','13900002002','13900002003','13900002004','13900002005')
GROUP BY s.name, w.item_type, w.state
ORDER BY s.name, w.item_type, w.state;
