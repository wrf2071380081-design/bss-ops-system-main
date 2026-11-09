-- 000051_work_item_done_compat：工作台视图兼容历史的 'DONE' 节点状态。
--
-- 背景（QA 在真库查到的事实）：
--   onboarding_node.status **没有 CHECK 约束**，库里确实存在 status='DONE' 的历史行
--   （flow_id=1 / kind=SUPPLIER / node_key=PREP，completed_at 非空）。
--   000050 的视图只把 'COMPLETED' 映射成 DONE，于是这类**已完成**的节点
--   在 v_work_item 里仍然是 state=TODO，会长期挂在工作台待办里（inbox 的额外过滤只排除
--   带审批留痕的 CONFIG/CONTRACT，不排除 PREP）。
--
-- 为什么改视图而不是改数据：
--   那条历史记录可能还有别的读取方按 'DONE' 判断，直接 UPDATE 是全局影响；
--   兼容放在视图层代价最小，且本迁移可干净回滚。
--
-- 为什么新开 000051 而不是改 000050：
--   000050 已经在本地 dev 库应用过，改它需要 down/up 折腾，别人拉下来也会不一致。
--
-- 与 000050 的差异只有**一行**：节点终态判断从 `= 'COMPLETED'` 扩成 `IN ('COMPLETED','DONE')`。

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
--    主体名：SUPPLIER 取 supplier.name，CUSTOMER 取 legal_subject.legal_name
--    区域：SUPPLIER 取 supplier.region，CUSTOMER 取 customer_profile.region
SELECT
  n.assignee_id,
  'TODO'::varchar(8),
  -- onboarding_node 的终态词是 'COMPLETED'（不是 'DONE'）
  CASE WHEN n.status IN ('COMPLETED','DONE') THEN 'DONE' ELSE 'TODO' END::varchar(16),
  'ONBOARDING_NODE'::varchar(32),
  f.id,
  n.node_key,
  CASE WHEN f.kind = 'SUPPLIER' THEN s.name ELSE ls.legal_name END,
  COALESCE(s.region, cp.region),
  'MID'::varchar(8),
  COALESCE(n.completed_at, f.created_at)
FROM onboarding_node n
JOIN onboarding_flow f ON f.id = n.flow_id
LEFT JOIN supplier s ON f.kind = 'SUPPLIER' AND s.id = f.owner_id
LEFT JOIN customer_profile cp ON f.kind = 'CUSTOMER' AND cp.id = f.owner_id
LEFT JOIN legal_subject ls ON ls.id = cp.subject_id
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
