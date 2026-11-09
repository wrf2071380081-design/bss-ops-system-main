-- 000050_customer_onboarding 回滚。
--
-- 顺序与 up 相反：
--   1. 删客户开通模型明细表（有数据也直接 DROP，因为它是本迁移新建的从属表）；
--   2. 把 v_work_item 还原成 000035 的定义（含 CUSTOMER 接旧 customer 表的写法）；
--   3. 删部分唯一索引；
--   4. 清掉 owner_id 的注释（000033 原本只有 DDL 行内注释，没有 COMMENT ON）。

DROP INDEX IF EXISTS public.idx_com_flow;
DROP TABLE IF EXISTS public.customer_onboarding_model;

-- 还原 000035 的视图定义（逐字一致，只把 CUSTOMER 分支接回遗留 customer 表、
-- 节点状态词改回 'DONE'）。
CREATE OR REPLACE VIEW v_work_item AS
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

DROP INDEX IF EXISTS public.uk_flow_customer_processing;

COMMENT ON COLUMN public.onboarding_flow.owner_id IS NULL;
