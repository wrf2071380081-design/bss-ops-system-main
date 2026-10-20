-- 000035_work_item_view：工作台统一收件箱视图（实时 UNION，不落表）。
--
-- 口径（2026-10-20 已确认）：
--   · 待办 = 分配给我且未完成的办理节点 / 计划 / 旧待办
--   · 已办 = 我完成过的办理节点 / 计划 / 旧待办
--   · 待阅 / 已阅 = 公告 × 人员，读状态来自 notice_read
--   · 读取全员可见；人员选择只读，操作仍由各业务模块按绑定人校验
--
-- 说明：视图只做"收件箱投影"，不写业务数据；各模块仍以自己的表为准。

CREATE OR REPLACE VIEW v_work_item AS
-- 1) 旧待办：todo_task
SELECT
  t.assignee_id,
  'TODO'::varchar(8)                    AS item_type,
  CASE WHEN t.status = 'DONE' THEN 'DONE' ELSE 'TODO' END::varchar(16) AS state,
  'TODO_TASK'::varchar(32)              AS biz_type,
  t.id                                  AS biz_id,
  t.title                               AS title,
  t.title                               AS biz_object,
  NULL::varchar(16)                     AS region,
  t.priority                            AS priority,
  t.created_at                          AS occurred_at
FROM todo_task t
WHERE t.assignee_id IS NOT NULL

UNION ALL
-- 2) 办理节点：onboarding_node（办理人）
SELECT
  n.assignee_id,
  'TODO'::varchar(8),
  CASE WHEN n.status = 'DONE' THEN 'DONE' ELSE 'TODO' END::varchar(16),
  'ONBOARDING_NODE'::varchar(32),
  f.id,
  n.node_key,
  CASE WHEN f.kind = 'SUPPLIER' THEN s.name ELSE c.name END,
  COALESCE(s.region, c.region),
  'MID'::varchar(8),
  COALESCE(n.completed_at, f.created_at)
FROM onboarding_node n
JOIN onboarding_flow f ON f.id = n.flow_id
LEFT JOIN supplier s ON f.kind = 'SUPPLIER' AND s.id = f.owner_id
LEFT JOIN customer c ON f.kind = 'CUSTOMER' AND c.id = f.owner_id
WHERE n.assignee_id IS NOT NULL AND n.enabled = true

UNION ALL
-- 3) 上下架/价格计划：发起人视为办理人
SELECT
  p.created_by,
  'TODO'::varchar(8),
  CASE WHEN p.status IN ('DONE', 'CANCELED') THEN 'DONE' ELSE 'TODO' END::varchar(16),
  'PLAN'::varchar(32),
  p.resource_id,
  p.kind,
  COALESCE(sm.name, '') || ' · ' || COALESCE(s.name, ''),
  r.region,
  'MID'::varchar(8),
  p.created_at
FROM plan p
JOIN resource r ON r.id = p.resource_id
JOIN standard_model sm ON sm.id = r.standard_model_id
JOIN supplier s ON s.id = r.supplier_id
WHERE p.created_by IS NOT NULL

UNION ALL
-- 4) 公告：每个在用人员 × 公告；读状态来自 notice_read
SELECT
  st.id,
  'READ'::varchar(8),
  CASE WHEN nr.read_at IS NOT NULL THEN 'READ_DONE' ELSE 'READ' END::varchar(16),
  'NOTICE'::varchar(32),
  n.id,
  n.type,
  COALESCE(n.payload->>'name', n.payload->>'title', n.target_type || '#' || n.target_id),
  COALESCE(n.payload->>'region', NULL),
  'MID'::varchar(8),
  n.created_at
FROM notice n
CROSS JOIN internal_staff st
LEFT JOIN notice_read nr ON nr.notice_id = n.id AND nr.staff_id = st.id
WHERE st.status = 'ACTIVE';

COMMENT ON VIEW v_work_item IS '工作台统一收件箱：TODO/DONE（待办/已办）+ READ/READ_DONE（待阅/已阅），实时聚合，不落表';
