-- 000050_customer_onboarding：客户开通流程（G4-B2）。
--
-- 这个迁移做四件事（前三件是工单指定，第四件是工单没给、但实现必需的存储位）：
--   1. 给「一客户同时只允许一条进行中流程」加**仅限 CUSTOMER** 的部分唯一索引；
--   2. 修 v_work_item 的两处缺陷（CUSTOMER 分支接错表、节点状态词不匹配）；
--   3. 纠正 onboarding_flow.owner_id 的注释（CUSTOMER 用 customer_profile.id）；
--   4. 新增 customer_onboarding_model：保存「本次开通要落哪几个模型 × 哪条资源」。
--
-- 为什么第 4 件必须新增表：
--   CONFIG 节点完成时要为每个模型写一条 customer_resource_link，这需要知道「本次开通
--   指定了哪些模型」。onboarding_flow / onboarding_node 没有任何 JSON/明细列，而工单明确
--   要求不动这两张表的列结构；000033 的 onboarding_batch 又写死了 supplier_id
--   （NOT NULL REFERENCES supplier(id)），客户流程用不了。所以只能新增一张纯从属表。
--   它只是 flow 的明细，不改变任何既有表结构，回滚即删。

-- ============================================================
-- 1. 一客户同时只允许一条进行中流程
-- ============================================================
-- 理由：kind='CUSTOMER' 目前零数据（已实测 count=0），加索引零风险；
-- 谓词限定 kind，供应商链路完全不受影响（它们本来就没有这个约束）。
CREATE UNIQUE INDEX uk_flow_customer_processing
  ON public.onboarding_flow(owner_id)
  WHERE kind = 'CUSTOMER' AND status = 'PROCESSING';

COMMENT ON INDEX public.uk_flow_customer_processing IS
  '一个客户同时只能有一条进行中（PROCESSING）的开通流程。仅约束 kind=CUSTOMER，不影响供应商流程。';

-- ============================================================
-- 2. 修复 v_work_item
-- ============================================================
-- 两处缺陷（都是在新场景（客户节点）下才会暴露的老问题）：
--
--   Bug 1：CUSTOMER 分支 LEFT JOIN 的是 000033 的遗留 customer 表，而客户流程的
--          owner_id 是 customer_profile.id。两者 id 空间不同，join 不上 →
--          客户节点在收件箱里 name / region 全是 NULL（卡片标题空白）。
--          000033 的 customer 表在 Go 侧零引用，本视图是它最后的引用点，这里一并切干净。
--
--   Bug 2：节点状态判断写的是 n.status = 'DONE'，但代码实际写入的是 'COMPLETED'
--          （native_handling_approval.go / supply_native_contract.go）。
--          供应商侧因为 workbench_inbox.go 自己又拼了 supplier 专属 UNION 覆盖，
--          所以一直没人发现；客户节点没有那条覆盖，会直接命中「永远 TODO」。
--
-- 注意：前两段（todo_task / plan）的 'DONE' 是它们自己表的词表，不要一起改。
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
  CASE WHEN n.status = 'COMPLETED' THEN 'DONE' ELSE 'TODO' END::varchar(16),
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

-- ============================================================
-- 3. owner_id 注释纠偏
-- ============================================================
COMMENT ON COLUMN public.onboarding_flow.owner_id IS
  '流程主体 ID：kind=SUPPLIER 时是 supplier.id；kind=CUSTOMER 时是 customer_profile.id（不是 000033 的遗留 customer 表）。';

-- ============================================================
-- 4. 客户开通的模型明细
-- ============================================================
CREATE TABLE public.customer_onboarding_model (
  id                bigserial   PRIMARY KEY,
  flow_id           bigint      NOT NULL REFERENCES public.onboarding_flow(id) ON DELETE CASCADE,
  standard_model_id bigint      NOT NULL REFERENCES public.standard_model(id) ON DELETE RESTRICT,
  resource_id       bigint      NOT NULL REFERENCES public.resource(id) ON DELETE RESTRICT,
  created_at        timestamptz NOT NULL DEFAULT now(),
  -- 同一次开通里同一资源只能出现一次（同一模型换节点是另一条资源，各自一行）。
  CONSTRAINT uk_com_flow_resource UNIQUE (flow_id, resource_id)
);

CREATE INDEX idx_com_flow ON public.customer_onboarding_model(flow_id);

COMMENT ON TABLE public.customer_onboarding_model IS
  '客户开通流程的模型明细：本次开通要为哪些「标准模型 × 供应资源」建立客户来源绑定。CONFIG 节点完成时按本表逐行写 customer_resource_link。';
COMMENT ON COLUMN public.customer_onboarding_model.flow_id IS
  '所属开通流程（onboarding_flow.id，kind=CUSTOMER）。流程删除时明细级联删除。';
COMMENT ON COLUMN public.customer_onboarding_model.resource_id IS
  '供应资源：创建流程时已校验「模型一致 + 与客户同部署节点 + 已准入」。';
