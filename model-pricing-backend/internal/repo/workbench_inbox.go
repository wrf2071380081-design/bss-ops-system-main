package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"model_bss/internal/domain/workbench"
	"model_bss/internal/infra/db"
)

// WorkbenchInboxRepo 工作台统一收件箱仓储（读 v_work_item 视图）。
type WorkbenchInboxRepo struct {
	base *gorm.DB
}

// NewWorkbenchInboxRepo 构造。
func NewWorkbenchInboxRepo(base *gorm.DB) *WorkbenchInboxRepo {
	return &WorkbenchInboxRepo{base: base}
}

func (r *WorkbenchInboxRepo) txOf(ctx context.Context) *gorm.DB {
	if tx := db.FromContext(ctx); tx != nil {
		return tx
	}
	return r.base
}

type inboxRow struct {
	BizType      string    `gorm:"column:biz_type"`
	BizID        int64     `gorm:"column:biz_id"`
	ItemType     string    `gorm:"column:item_type"`
	State        string    `gorm:"column:state"`
	Title        string    `gorm:"column:title"`
	BizObject    *string   `gorm:"column:biz_object"`
	Region       *string   `gorm:"column:region"`
	Priority     *string   `gorm:"column:priority"`
	OccurredAt   time.Time `gorm:"column:occurred_at"`
	PlanID       int64     `gorm:"column:plan_id"`
	AssigneeID   int64     `gorm:"column:assignee_id"`
	AssigneeName *string   `gorm:"column:assignee_name"`
}

// workbenchInboxSource keeps the shared view as the source of non-plan items,
// while projecting plans from their native table. v_work_item exposes a
// resource_id as biz_id for PLAN rows and does not expose plan.id; correlating
// back by created_at can map multiple plans created in one transaction to the
// same plan. This projection carries the stable ID without changing the shared
// migration/view contract.
const workbenchInboxSource = `(
SELECT assignee_id, item_type, state, biz_type, biz_id, title, biz_object, region, priority, occurred_at,
       NULL::bigint AS plan_id
FROM v_work_item
WHERE biz_type <> 'PLAN'
  AND NOT (biz_type='ONBOARDING_NODE' AND title IN ('CONFIG','CONTRACT') AND EXISTS(SELECT 1 FROM operation_log l WHERE l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'flow_id'=v_work_item.biz_id::text))
  AND NOT EXISTS (
      SELECT 1
      FROM todo_task t
      JOIN quote_sheet qs ON qs.id = t.biz_id
      WHERE t.id = v_work_item.biz_id
        AND t.biz_type = 'QUOTE'
        AND qs.status = 'APPROVING'
  )
UNION ALL
SELECT st.id AS assignee_id,
       'TODO'::varchar(8) AS item_type,
       'TODO'::varchar(16) AS state,
       'QUOTE_APPROVAL'::varchar(32) AS biz_type,
       qs.id AS biz_id,
       '供应商报价审批'::varchar(256) AS title,
       ls.legal_name || ' · V' || qs.version_no::text AS biz_object,
       NULL::varchar(16) AS region,
       'MID'::varchar(8) AS priority,
       COALESCE(qs.submitted_at, qs.created_at) AS occurred_at,
       NULL::bigint AS plan_id
FROM quote_sheet qs
JOIN supplier_profile sp ON sp.id = qs.supplier_id
JOIN legal_subject ls ON ls.id = sp.subject_id
JOIN internal_staff st ON st.status = 'ACTIVE' AND st.can_approve_supplier = true
WHERE qs.status = 'APPROVING'
UNION ALL
SELECT st.id,'TODO'::varchar(8),CASE WHEN s.status='PENDING_APPROVAL' THEN 'TODO' ELSE 'DONE' END::varchar(16),
 'SUBMISSION_APPROVAL'::varchar(32),s.id,'供应商模型申报审批'::varchar(256),sp.name || ' · ' || s.submission_no,s.region,'MID'::varchar(8),coalesce(s.submitted_at,s.created_at),NULL::bigint
 FROM submission s JOIN supplier sp ON sp.id=s.supplier_id JOIN internal_staff st ON st.status='ACTIVE' AND st.can_approve_supplier
 LEFT JOIN LATERAL(SELECT operator_id FROM operation_log WHERE entity_type='submission' AND entity_id=s.id AND (action='REJECT' OR (action='APPROVE' AND "after"->>'contract'='supply.handling_approval')) ORDER BY id DESC LIMIT 1) decided ON TRUE
 WHERE s.status='PENDING_APPROVAL' OR (decided.operator_id=st.id AND s.status IN('REJECTED','PROCESSING','COMPLETED'))
UNION ALL
SELECT n.assignee_id,'TODO'::varchar(8),CASE WHEN n.status='COMPLETED' THEN 'DONE' ELSE 'TODO' END::varchar(16),
 'SUBMISSION_HANDLING'::varchar(32),n.id,'测试与资源配置办理'::varchar(256),sp.name || ' · ' || s.submission_no,s.region,'MID'::varchar(8),coalesce(n.completed_at,f.created_at),NULL::bigint
 FROM onboarding_node n JOIN onboarding_flow f ON f.id=n.flow_id
 JOIN operation_log l ON l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'node_id'=n.id::text
 JOIN submission s ON s.id=l.entity_id JOIN supplier sp ON sp.id=s.supplier_id
 WHERE n.node_key='CONFIG' AND n.enabled AND n.assignee_id IS NOT NULL AND n.status IN('IN_PROGRESS','COMPLETED')
UNION ALL
SELECT n.assignee_id,'TODO'::varchar(8),CASE WHEN n.status='COMPLETED' THEN 'DONE' ELSE 'TODO' END::varchar(16),
 'SUBMISSION_CONTRACT'::varchar(32),n.id,'合同与授信办理'::varchar(256),sp.name || ' · ' || s.submission_no,s.region,'MID'::varchar(8),coalesce(n.completed_at,f.created_at),NULL::bigint
 FROM onboarding_node n JOIN onboarding_flow f ON f.id=n.flow_id
 JOIN operation_log l ON l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'flow_id'=n.flow_id::text
 JOIN submission s ON s.id=l.entity_id JOIN supplier sp ON sp.id=s.supplier_id
 WHERE n.node_key='CONTRACT' AND n.enabled AND n.assignee_id IS NOT NULL AND n.status IN('IN_PROGRESS','COMPLETED')
UNION ALL
SELECT CASE WHEN p.kind='PRICE_CHANGE' THEN NULLIF(p."after"->'plan'->>'assignee_id','')::bigint ELSE p.created_by END AS assignee_id,
       'TODO'::varchar(8) AS item_type,
       CASE WHEN p.status IN ('DONE', 'CANCELED') THEN 'DONE' ELSE 'TODO' END::varchar(16) AS state,
       'PLAN'::varchar(32) AS biz_type,
       p.resource_id AS biz_id,
       p.kind AS title,
       COALESCE(sm.name, '') || ' · ' || COALESCE(s.name, '') AS biz_object,
       r.region AS region,
       'MID'::varchar(8) AS priority,
       p.created_at AS occurred_at,
       p.id AS plan_id
FROM plan p
JOIN resource r ON r.id = p.resource_id
JOIN standard_model sm ON sm.id = r.standard_model_id
JOIN supplier s ON s.id = r.supplier_id
WHERE p.created_by IS NOT NULL
) AS v_work_item`

// ListInbox 查询某人的收件箱（按状态/类型/区域/关键字过滤 + 分页）。
func (r *WorkbenchInboxRepo) ListInbox(ctx context.Context, q workbench.InboxQuery) ([]workbench.InboxItem, int64, error) {
	apply := func(tx *gorm.DB) *gorm.DB {
		tx = tx.Where("assignee_id = ?", q.AssigneeID)
		if q.State != "" {
			tx = tx.Where("state = ?", q.State)
		}
		if q.BizType != "" {
			tx = tx.Where("biz_type = ?", q.BizType)
		}
		if q.Region != "" {
			// 2026-10-21 口径：没有平台属性的待办（region 为空）不参与平台归属，
			// 选国内/海外时也一并显示，避免"筛个选东西全没了"。
			tx = tx.Where("(region = ? OR region IS NULL OR region = '')", q.Region)
		}
		if q.Keyword != "" {
			kw := "%" + q.Keyword + "%"
			tx = tx.Where("(title ILIKE ? OR biz_object ILIKE ?)", kw, kw)
		}
		return tx
	}

	// 责任人姓名来自 internal_staff；视图只带 assignee_id。
	source := func(tx *gorm.DB) *gorm.DB {
		return tx.Table(workbenchInboxSource).Joins("LEFT JOIN internal_staff st ON st.id = v_work_item.assignee_id")
	}

	var total int64
	if err := apply(source(r.txOf(ctx))).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []inboxRow
	err := apply(source(r.txOf(ctx))).
		Select("v_work_item.biz_type, v_work_item.biz_id, v_work_item.item_type, v_work_item.state, v_work_item.title, v_work_item.biz_object, v_work_item.region, v_work_item.priority, v_work_item.occurred_at, v_work_item.plan_id, v_work_item.assignee_id, st.name AS assignee_name").
		Order("v_work_item.occurred_at DESC, v_work_item.biz_id DESC, v_work_item.plan_id DESC NULLS LAST").
		Offset((q.Page - 1) * q.Size).
		Limit(q.Size).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	list := make([]workbench.InboxItem, 0, len(rows))
	for _, row := range rows {
		item := workbench.InboxItem{
			BizType:    row.BizType,
			BizID:      row.BizID,
			ItemType:   row.ItemType,
			State:      row.State,
			Title:      row.Title,
			OccurredAt: row.OccurredAt,
			PlanID:     row.PlanID,
			AssigneeID: row.AssigneeID,
		}
		if row.AssigneeName != nil {
			item.AssigneeName = *row.AssigneeName
		}
		if row.BizObject != nil {
			item.BizObject = *row.BizObject
		}
		if row.Region != nil {
			item.Region = *row.Region
		}
		if row.Priority != nil {
			item.Priority = *row.Priority
		}
		list = append(list, item)
	}
	return list, total, nil
}

type personRow struct {
	ID   int64  `gorm:"column:id"`
	Name string `gorm:"column:name"`
}

// ListPeople 人员选择器：所有在用内部人员。
func (r *WorkbenchInboxRepo) ListPeople(ctx context.Context) ([]workbench.Person, error) {
	var rows []personRow
	err := r.txOf(ctx).Table("internal_staff").
		Select("id, name").
		Where("status = ?", "ACTIVE").
		Order("name ASC, id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	list := make([]workbench.Person, 0, len(rows))
	for _, row := range rows {
		list = append(list, workbench.Person{ID: row.ID, Name: row.Name})
	}
	return list, nil
}

type operationRow struct {
	ID           int64     `gorm:"column:id"`
	Action       string    `gorm:"column:action"`
	EntityType   string    `gorm:"column:entity_type"`
	EntityID     int64     `gorm:"column:entity_id"`
	OperatorName *string   `gorm:"column:operator_name"`
	Reason       *string   `gorm:"column:reason"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// ListOperations 操作记录（可按操作人/实体过滤）。
func (r *WorkbenchInboxRepo) ListOperations(ctx context.Context, q workbench.OperationQuery) ([]workbench.OperationItem, int64, error) {
	apply := func(tx *gorm.DB) *gorm.DB {
		tx = tx.Table("operation_log")
		if q.OperatorID > 0 {
			tx = tx.Where("operator_id = ?", q.OperatorID)
		}
		if q.EntityType != "" {
			tx = tx.Where("entity_type = ?", q.EntityType)
		}
		if q.EntityID > 0 {
			tx = tx.Where("entity_id = ?", q.EntityID)
		}
		return tx
	}

	var total int64
	if err := apply(r.txOf(ctx)).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []operationRow
	err := apply(r.txOf(ctx)).
		Select("id, action, entity_type, entity_id, operator_name_snapshot AS operator_name, reason, created_at").
		Order("created_at DESC, id DESC").
		Offset((q.Page - 1) * q.Size).
		Limit(q.Size).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	list := make([]workbench.OperationItem, 0, len(rows))
	for _, row := range rows {
		item := workbench.OperationItem{
			ID:         row.ID,
			Action:     row.Action,
			EntityType: row.EntityType,
			EntityID:   row.EntityID,
			CreatedAt:  row.CreatedAt,
		}
		if row.OperatorName != nil {
			item.OperatorName = *row.OperatorName
		}
		if row.Reason != nil {
			item.Reason = *row.Reason
		}
		list = append(list, item)
	}
	return list, total, nil
}

// MarkNoticeRead 标记公告已读（幂等）；公告不存在返回 false。
func (r *WorkbenchInboxRepo) MarkNoticeRead(ctx context.Context, noticeID, staffID int64) (bool, error) {
	var n int64
	if err := r.txOf(ctx).Table("notice").Where("id = ?", noticeID).Count(&n).Error; err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	err := r.txOf(ctx).Exec(
		"INSERT INTO notice_read (notice_id, staff_id) VALUES (?, ?) ON CONFLICT (notice_id, staff_id) DO NOTHING",
		noticeID, staffID,
	).Error
	return err == nil, err
}
