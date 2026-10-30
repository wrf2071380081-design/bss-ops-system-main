-- ============================================================================
-- 待阅（公告）演示数据（2026-10-30）
--
-- 背景：notice 表 0 行 → v_work_item 的 READ 分支没数据 → 工作台「待阅/已阅」两个 tab 全空。
-- 契约：payload 必须满足 supply.notice v1（internal/infra/supplypg/notice_row.go 的 noticePayload，
--       DisallowUnknownFields：**不能加额外键**），且 target_type='RESOURCE'、target_id 指向真实资源、
--       type 必须是前端 NOTICE_LABELS 里的五种之一（否则事项列显示原始英文枚举）。
-- 结构：notice（一条公告）+ notice_read（每人一条已读记录，未读就没有行）= 1:n，
--       视图 CROSS JOIN internal_staff 展开成「一条公告 × N 个在用人员」。
-- 幂等：每条公告用固定 created_at 作键，重跑不重复插；notice_read 靠主键 ON CONFLICT 去重。
-- 执行：psql -v ON_ERROR_STOP=1 --single-transaction -f seed_workbench_notice_20261030.sql
-- ============================================================================

BEGIN;

-- 用「已接入资源」的前 5 条做公告对象；资源不足时对应的公告自动跳过（INSERT ... SELECT 0 行）。
CREATE TEMP TABLE tmp_pick ON COMMIT DROP AS
SELECT r.id,
       r.supplier_model_name AS model_name,
       r.region,
       s.name AS supplier_name,
       row_number() OVER (ORDER BY r.id) AS rn
FROM public.resource r
JOIN public.supplier s ON s.id = r.supplier_id
WHERE r.state = 'ADMITTED';

-- 受影响客户：只算已给客户、非草稿的报价（与 /internal/resources/:id/affected-customers 同口径）
CREATE TEMP TABLE tmp_affected ON COMMIT DROP AS
SELECT cqi.resource_id,
       count(DISTINCT cq.customer_id) AS customer_count,
       jsonb_agg(DISTINCT ls.legal_name) AS customer_names
FROM public.customer_quote_item cqi
JOIN public.customer_quote cq ON cq.id = cqi.customer_quote_id
JOIN public.customer_profile cp ON cp.id = cq.customer_id
JOIN public.legal_subject ls ON ls.id = cp.subject_id
WHERE cqi.resource_id IS NOT NULL AND cq.status <> 'DRAFT'
GROUP BY cqi.resource_id;

-- 1) 资源上架公告（未读）
INSERT INTO public.notice (type, target_type, target_id, payload, created_at)
SELECT 'RESOURCE_ONLINED', 'RESOURCE', p.id,
       jsonb_build_object(
           'contract', 'supply.notice',
           'version', 1,
           'name', '资源上架 · ' || p.model_name,
           'resource_name', p.model_name,
           'region', p.region,
           'supplier_name', p.supplier_name,
           'title', '资源上架 · ' || p.model_name,
           'content', '该资源已完成接入验收并上架，客户可对其发起报价。',
           'reason', '正式配置与测试证据齐备，按上架计划执行上架。',
           'affected_status', 'PENDING'
       ),
       timestamptz '2026-10-29 09:20:00+08'
FROM tmp_pick p
WHERE p.rn = 1
  AND NOT EXISTS (SELECT 1 FROM public.notice n
                  WHERE n.type = 'RESOURCE_ONLINED' AND n.created_at = timestamptz '2026-10-29 09:20:00+08');

-- 2) 资源下架公告（未读）：影响面按报价明细真算，有客户才算 READY
INSERT INTO public.notice (type, target_type, target_id, payload, created_at)
SELECT 'RESOURCE_OFFLINED', 'RESOURCE', p.id,
       jsonb_build_object(
           'contract', 'supply.notice',
           'version', 1,
           'name', '资源下架 · ' || p.model_name,
           'resource_name', p.model_name,
           'region', p.region,
           'supplier_name', p.supplier_name,
           'title', '资源下架 · ' || p.model_name,
           'content', '该资源将从可售资源中下架，请复核存量客户报价是否仍可履约，并在对应 B 端平台完成实际操作后登记。',
           'reason', '供应商侧接口稳定性连续两周不达 SLA，暂停对外销售。',
           'affected_status', CASE WHEN COALESCE(a.customer_count, 0) > 0 THEN 'READY' ELSE 'PENDING' END
       )
       || CASE WHEN COALESCE(a.customer_count, 0) > 0
               THEN jsonb_build_object('affected_customer_count', a.customer_count::text,
                                       'affected_customers', a.customer_names)
               ELSE '{}'::jsonb END,
       timestamptz '2026-10-29 15:40:00+08'
FROM tmp_pick p
LEFT JOIN tmp_affected a ON a.resource_id = p.id
WHERE p.rn = 2
  AND NOT EXISTS (SELECT 1 FROM public.notice n
                  WHERE n.type = 'RESOURCE_OFFLINED' AND n.created_at = timestamptz '2026-10-29 15:40:00+08');

-- 3) 客户启用/禁用资源（未读）
INSERT INTO public.notice (type, target_type, target_id, payload, created_at)
SELECT 'RESOURCE_CHANGE', 'RESOURCE', p.id,
       jsonb_build_object(
           'contract', 'supply.notice',
           'version', 1,
           'name', '客户启用/禁用 · ' || p.model_name,
           'resource_name', p.model_name,
           'region', p.region,
           'supplier_name', p.supplier_name,
           'title', '客户启用/禁用 · ' || p.model_name,
           'content', '客户对该资源的启用状态发生变化，请复核该客户报价的适用范围。',
           'reason', '客户侧业务调整：暂停使用该资源，已下发的报价需要复核。',
           'affected_status', 'PENDING'
       ),
       timestamptz '2026-10-30 08:35:00+08'
FROM tmp_pick p
WHERE p.rn = 3
  AND NOT EXISTS (SELECT 1 FROM public.notice n
                  WHERE n.type = 'RESOURCE_CHANGE' AND n.created_at = timestamptz '2026-10-30 08:35:00+08');

-- 4) 采购系数（价格）变动（未读）
INSERT INTO public.notice (type, target_type, target_id, payload, created_at)
SELECT 'COST_CHANGE', 'RESOURCE', p.id,
       jsonb_build_object(
           'contract', 'supply.notice',
           'version', 1,
           'name', '采购系数调整 · ' || p.model_name,
           'resource_name', p.model_name,
           'region', p.region,
           'supplier_name', p.supplier_name,
           'title', '采购系数调整 · ' || p.model_name,
           'content', '该资源的采购系数已调整，完全成本与最低报价线同步变化，请复核在售报价。',
           'reason', '供应商季度调价，采购系数由 0.6800 调整为 0.7200。',
           'affected_status', 'PENDING'
       ),
       timestamptz '2026-10-30 10:05:00+08'
FROM tmp_pick p
WHERE p.rn = 4
  AND NOT EXISTS (SELECT 1 FROM public.notice n
                  WHERE n.type = 'COST_CHANGE' AND n.created_at = timestamptz '2026-10-30 10:05:00+08');

-- 5) 上下架计划到点提醒（未读）：原型「原通知」详情要计划名/供应商/预计时间/执行人
INSERT INTO public.notice (type, target_type, target_id, payload, created_at)
SELECT 'ONLINE_OFFLINE_PLAN', 'RESOURCE', p.id,
       jsonb_build_object(
           'contract', 'supply.notice',
           'version', 1,
           'name', '上架计划 · ' || p.model_name,
           'resource_name', p.model_name,
           'region', p.region,
           'supplier_name', p.supplier_name,
           'title', '上架计划到点提醒 · ' || p.model_name,
           'content', '预计时间已到，请在对应 B 端平台完成实际操作后登记。',
           'reason', '按供应商上架计划执行。',
           'affected_status', 'PENDING',
           'plan_name', '上架计划 · ' || p.model_name,
           'scheduled_at', to_char(timestamptz '2026-10-30 11:00:00+08' AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
           'executor_name', COALESCE((SELECT st.name FROM public.internal_staff st
                                        WHERE st.mobile = '13900002001'), '')
       ),
       timestamptz '2026-10-30 11:10:00+08'
FROM tmp_pick p
WHERE p.rn = 5
  AND NOT EXISTS (SELECT 1 FROM public.notice n
                  WHERE n.type = 'ONLINE_OFFLINE_PLAN' AND n.created_at = timestamptz '2026-10-30 11:10:00+08');

-- 6) 已阅样本：有了已读记录，「已阅」tab 才有内容；其余人保持未读。
--    翟龙龙读前两条、杜欢读前四条、张三读第三条。
INSERT INTO public.notice_read (notice_id, staff_id, read_at)
SELECT n.id, s.id, n.created_at + interval '40 minutes'
FROM public.notice n
JOIN public.internal_staff s ON s.mobile IN ('13900002001','13900002002','13900002003')
WHERE (n.type = 'RESOURCE_ONLINED'     AND n.created_at = timestamptz '2026-10-29 09:20:00+08' AND s.mobile IN ('13900002001','13900002002'))
   OR (n.type = 'RESOURCE_OFFLINED'    AND n.created_at = timestamptz '2026-10-29 15:40:00+08' AND s.mobile IN ('13900002001','13900002002'))
   OR (n.type = 'RESOURCE_CHANGE'      AND n.created_at = timestamptz '2026-10-30 08:35:00+08' AND s.mobile IN ('13900002002','13900002003'))
   OR (n.type = 'COST_CHANGE'          AND n.created_at = timestamptz '2026-10-30 10:05:00+08' AND s.mobile IN ('13900002002'))
ON CONFLICT (notice_id, staff_id) DO NOTHING;

COMMIT;

-- 核对：每条公告有多少人未读/已读，以及 13900002001 的待阅/已阅条数
SELECT n.type, n.created_at,
       count(*) FILTER (WHERE nr.read_at IS NULL) AS unread,
       count(*) FILTER (WHERE nr.read_at IS NOT NULL) AS read
FROM public.notice n
JOIN public.internal_staff s ON s.status = 'ACTIVE'
LEFT JOIN public.notice_read nr ON nr.notice_id = n.id AND nr.staff_id = s.id
WHERE n.payload ->> 'contract' = 'supply.notice'
GROUP BY n.id, n.type, n.created_at
ORDER BY n.created_at;

SELECT s.name, w.state, count(*)
FROM public.v_work_item w
JOIN public.internal_staff s ON s.id = w.assignee_id
WHERE w.item_type = 'READ' AND s.mobile IN ('13900002001','13900002002','13900002003')
GROUP BY s.name, w.state
ORDER BY s.name, w.state;
