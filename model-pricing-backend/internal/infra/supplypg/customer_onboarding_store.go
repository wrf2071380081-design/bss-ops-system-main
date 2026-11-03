package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"model_bss/internal/domain/customer"
)

// 客户开通流程的仓储（迁移 000050，G4-B2）。
//
// 事务纪律：
//   - StartFlow：insert flow + node(1~2) + 模型明细 + 审计，同一事务；
//   - CompleteConfig：**一次事务里写 N 条 customer_resource_link** + 更新节点 + 推进流程 + 审计。
//     半个批次写进去比不写更糟（客户会看到「有一部分模型开通了」的假象），所以任何一条绑定
//     冲突（例如该模型已有生效来源）都整体回滚，返回可读 409 语义。
//   - CompleteContract：完成 CONTRACT + 把 CONFIG 从 WAITING 推到 IN_PROGRESS + 审计。
//
// 绑定插入刻意复用 CustomerResourceLinkStore.createLinkWith(tx, ...)：唯一索引识别、
// 错误文案与单条创建完全一致，不会出现"单条建和别人批量建口径不同"。

// CustomerOnboardingStore 客户开通流程仓储。
type CustomerOnboardingStore struct {
	db     *sql.DB
	schema string
	links  *CustomerResourceLinkStore
}

var _ customer.OnboardingStore = (*CustomerOnboardingStore)(nil)

// NewCustomerOnboardingStore 构造。links 与单条绑定共用同一个仓储实例（同一个 schema）。
func NewCustomerOnboardingStore(db *sql.DB, schema string, links *CustomerResourceLinkStore) (*CustomerOnboardingStore, error) {
	if db == nil || links == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("customer onboarding store")
	}
	return &CustomerOnboardingStore{db: db, schema: schema, links: links}, nil
}

func (s *CustomerOnboardingStore) table(name string) string { return s.schema + "." + name }

// mapOnboardingError 把 PG 错误翻译成领域错误。
//
// 23505 要分两种：
//   - uk_flow_customer_processing → 「该客户已有进行中流程」（应用层已先查，这里是并发兜底）；
//   - 其它唯一冲突（绑定索引）→ 沿用绑定仓储的映射，保证与单条创建同一套文案。
func mapOnboardingError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return customer.ErrOnboardingFlowNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "uk_flow_customer_processing" {
				return customer.ErrOnboardingInProgress
			}
			return mapLinkStorageError(err)
		case "40001":
			return customer.ErrOnboardingNodeNotReady
		case "23503":
			return customer.ErrCustomerProfileNotFound
		case "42703", "42P01", "42804", "42846", "42883":
			return customer.ErrOnboardingUnavailable
		}
	}
	return err
}

// auditFlow 写一条流程审计（before/after 由调用方给，nil 表示无）。
func (s *CustomerOnboardingStore) auditFlow(ctx context.Context, tx *sql.Tx, flowID, operatorID int64, action string, before, after map[string]any, reason, requestID string) error {
	encode := func(value map[string]any) any {
		if value == nil {
			return nil
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		return string(raw)
	}
	query := fmt.Sprintf(`
INSERT INTO %s (entity_type, entity_id, action, operator_id, operator_name_snapshot,
                "before", "after", reason, request_id, created_at)
VALUES ('onboarding_flow', $1, $2, $3, (SELECT name FROM %s WHERE id = $3), $4::jsonb, $5::jsonb, NULLIF($6, ''), NULLIF($7, ''), now())`,
		s.table("operation_log"), s.table("internal_staff"))
	_, err := tx.ExecContext(ctx, query, flowID, action, operatorID, encode(before), encode(after), reason, requestID)
	return err
}

// CustomerRegion 读客户主体区域。
func (s *CustomerOnboardingStore) CustomerRegion(ctx context.Context, customerProfileID int64) (string, error) {
	var region string
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT region FROM %s WHERE id = $1`, s.table("customer_profile")), customerProfileID).Scan(&region); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", customer.ErrCustomerProfileNotFound
		}
		return "", mapOnboardingError(err)
	}
	return region, nil
}

// StartFlow 建流程 + 节点 + 模型明细 + 审计（单事务）。
func (s *CustomerOnboardingStore) StartFlow(ctx context.Context, in customer.StartFlowRecord) (*customer.OnboardingFlow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	defer func() { _ = tx.Rollback() }()

	// 客户必须存在（顺带取区域，用于把 owner 语义钉死在 customer_profile 上）。
	var owner int64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE id = $1`, s.table("customer_profile")), in.CustomerProfileID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrCustomerProfileNotFound
		}
		return nil, mapOnboardingError(err)
	}

	// 应用层先查一次，给出可读错误；真正的并发保护是部分唯一索引 uk_flow_customer_processing。
	var processing bool
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s WHERE kind = 'CUSTOMER' AND owner_id = $1 AND status = 'PROCESSING')`,
		s.table("onboarding_flow")), in.CustomerProfileID).Scan(&processing); err != nil {
		return nil, mapOnboardingError(err)
	}
	if processing {
		return nil, customer.ErrOnboardingInProgress
	}

	currentNode := customer.OnboardingNodeConfig
	if in.FirstOrder {
		currentNode = customer.OnboardingNodeContract
	}
	var flowID int64
	var createdAt, updatedAt time.Time
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
INSERT INTO %s (kind, owner_id, status, current_node)
VALUES ('CUSTOMER', $1, 'PROCESSING', $2)
RETURNING id, created_at, updated_at`, s.table("onboarding_flow")),
		in.CustomerProfileID, currentNode).Scan(&flowID, &createdAt, &updatedAt); err != nil {
		return nil, mapOnboardingError(err)
	}

	// 首单顺序是 CONTRACT → CONFIG：CONTRACT 立刻 IN_PROGRESS，
	// CONFIG 先挂 WAITING_PREDECESSOR，等合同授信完成后由 CompleteContract 推到 IN_PROGRESS。
	// 非首单只有 CONFIG，直接 IN_PROGRESS。
	configStatus := customer.OnboardingNodeInProgress
	if in.FirstOrder {
		configStatus = customer.OnboardingNodeWaiting
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (flow_id, node_key, status, assignee_id) VALUES ($1, 'CONTRACT', 'IN_PROGRESS', $2)`,
			s.table("onboarding_node")), flowID, in.ContractAssigneeID); err != nil {
			return nil, mapOnboardingError(err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (flow_id, node_key, status, assignee_id) VALUES ($1, 'CONFIG', $2, $3)`,
		s.table("onboarding_node")), flowID, configStatus, in.ConfigAssigneeID); err != nil {
		return nil, mapOnboardingError(err)
	}
	for _, model := range in.Models {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (flow_id, standard_model_id, resource_id) VALUES ($1, $2, $3)`,
			s.table("customer_onboarding_model")), flowID, model.StandardModelID, model.ResourceID); err != nil {
			return nil, mapOnboardingError(err)
		}
	}

	models := make([]map[string]any, 0, len(in.Models))
	for _, model := range in.Models {
		models = append(models, map[string]any{"standard_model_id": model.StandardModelID, "resource_id": model.ResourceID})
	}
	if err := s.auditFlow(ctx, tx, flowID, in.OperatorID, "START",
		nil,
		map[string]any{"kind": "CUSTOMER", "owner_id": in.CustomerProfileID, "first_order": in.FirstOrder, "current_node": currentNode, "models": models},
		in.Reason, in.RequestID); err != nil {
		return nil, mapOnboardingError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapOnboardingError(err)
	}
	return s.GetFlow(ctx, in.CustomerProfileID)
}

// LoadNodeContext 读完成节点前的上下文。
func (s *CustomerOnboardingStore) LoadNodeContext(ctx context.Context, nodeID int64) (*customer.OnboardingNodeContext, error) {
	query := fmt.Sprintf(`
SELECT n.id, n.node_key, n.status, COALESCE(n.assignee_id, 0),
       f.id, f.status, f.owner_id,
       COALESCE((SELECT c.status FROM %s c WHERE c.flow_id = f.id AND c.node_key = 'CONFIG' AND c.enabled), ''),
       COALESCE((SELECT c.status = 'COMPLETED' FROM %s c WHERE c.flow_id = f.id AND c.node_key = 'CONTRACT' AND c.enabled), FALSE)
  FROM %s n
  JOIN %s f ON f.id = n.flow_id
 WHERE n.id = $1 AND f.kind = 'CUSTOMER' AND n.enabled`,
		s.table("onboarding_node"), s.table("onboarding_node"), s.table("onboarding_node"), s.table("onboarding_flow"))
	var out customer.OnboardingNodeContext
	if err := s.db.QueryRowContext(ctx, query, nodeID).Scan(
		&out.NodeID, &out.NodeKey, &out.NodeStatus, &out.NodeAssigneeID,
		&out.FlowID, &out.FlowStatus, &out.CustomerProfileID, &out.ConfigNodeStatus, &out.ContractCompleted,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrOnboardingNodeNotFound
		}
		return nil, mapOnboardingError(err)
	}
	return &out, nil
}

// CompleteConfig 写绑定 + 完成 CONFIG + 推进/收尾流程（单事务）。
func (s *CustomerOnboardingStore) CompleteConfig(ctx context.Context, in customer.CompleteConfigRecord) (*customer.OnboardingFlow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID int64
	var flowStatus, currentNode string
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT owner_id, status, COALESCE(current_node, '') FROM %s WHERE id = $1 AND kind = 'CUSTOMER' FOR UPDATE`,
		s.table("onboarding_flow")), in.FlowID).Scan(&ownerID, &flowStatus, &currentNode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrOnboardingFlowNotFound
		}
		return nil, mapOnboardingError(err)
	}
	if flowStatus != customer.OnboardingFlowProcessing {
		return nil, customer.ErrOnboardingNodeCompleted
	}

	var nodeID int64
	var nodeStatus string
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT id, status FROM %s WHERE flow_id = $1 AND node_key = 'CONFIG' AND enabled FOR UPDATE`,
		s.table("onboarding_node")), in.FlowID).Scan(&nodeID, &nodeStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrOnboardingNodeNotFound
		}
		return nil, mapOnboardingError(err)
	}
	if nodeStatus == customer.OnboardingNodeCompleted {
		return nil, customer.ErrOnboardingNodeCompleted
	}
	if nodeStatus != customer.OnboardingNodeInProgress {
		return nil, customer.ErrOnboardingNodeNotReady
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
SELECT standard_model_id, resource_id FROM %s WHERE flow_id = $1 ORDER BY id`,
		s.table("customer_onboarding_model")), in.FlowID)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	type modelRow struct{ standardModelID, resourceID int64 }
	models := make([]modelRow, 0, 4)
	for rows.Next() {
		var m modelRow
		if err := rows.Scan(&m.standardModelID, &m.resourceID); err != nil {
			_ = rows.Close()
			return nil, mapOnboardingError(err)
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, mapOnboardingError(err)
	}
	_ = rows.Close()
	if len(models) == 0 {
		return nil, customer.ErrOnboardingModelsRequired
	}

	// 逐条写绑定：任何一条撞唯一索引（该模型已有生效来源 / 该资源已绑定）都整体回滚。
	now := time.Now().UTC()
	note := fmt.Sprintf("客户开通流程完成资源配置（流程 %d）", in.FlowID)
	for _, m := range models {
		if _, err := s.links.createLinkWith(ctx, tx, customer.CreateLinkRecord{
			CustomerProfileID: ownerID,
			StandardModelID:   m.standardModelID,
			ResourceID:        m.resourceID,
			State:             customer.LinkStateActive,
			ActivationNote:    note,
			ActivatedAt:       &now,
			CreatedBy:         in.OperatorID,
			RequestID:         in.RequestID,
		}); err != nil {
			return nil, err // 已经是领域错误（ErrLinkModelAlreadyActive 等），整批回滚
		}
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s SET status = 'COMPLETED', completed_at = $2, note = $3 WHERE id = $1`,
		s.table("onboarding_node")), nodeID, now, in.Reason); err != nil {
		return nil, mapOnboardingError(err)
	}

	var remaining int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT count(*) FROM %s WHERE flow_id = $1 AND enabled AND status NOT IN ('COMPLETED','CANCELED')`,
		s.table("onboarding_node")), in.FlowID).Scan(&remaining); err != nil {
		return nil, mapOnboardingError(err)
	}
	after := map[string]any{"node_key": "CONFIG", "node_status": "COMPLETED"}
	if remaining == 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s SET status = 'COMPLETED', current_node = NULL, updated_at = $2 WHERE id = $1`,
			s.table("onboarding_flow")), in.FlowID, now); err != nil {
			return nil, mapOnboardingError(err)
		}
		after["flow_status"] = customer.OnboardingFlowCompleted
	} else {
		// 理论上不会发生（CONFIG 是最后一个节点），保留推进逻辑以防将来加节点。
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s SET status = 'IN_PROGRESS' WHERE flow_id = $1 AND enabled AND status = 'WAITING_PREDECESSOR'`,
			s.table("onboarding_node")), in.FlowID); err != nil {
			return nil, mapOnboardingError(err)
		}
	}
	if err := s.auditFlow(ctx, tx, in.FlowID, in.OperatorID, "COMPLETE_CONFIG",
		map[string]any{"node_key": "CONFIG", "node_status": nodeStatus, "flow_status": flowStatus, "current_node": currentNode},
		after, in.Reason, in.RequestID); err != nil {
		return nil, mapOnboardingError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapOnboardingError(err)
	}
	return s.GetFlow(ctx, ownerID)
}

// CompleteContract 完成 CONTRACT 并把 CONFIG 推到 IN_PROGRESS（单事务）。
func (s *CustomerOnboardingStore) CompleteContract(ctx context.Context, in customer.CompleteContractRecord) (*customer.OnboardingFlow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID int64
	var flowStatus string
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT owner_id, status FROM %s WHERE id = $1 AND kind = 'CUSTOMER' FOR UPDATE`,
		s.table("onboarding_flow")), in.FlowID).Scan(&ownerID, &flowStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrOnboardingFlowNotFound
		}
		return nil, mapOnboardingError(err)
	}
	if flowStatus != customer.OnboardingFlowProcessing {
		return nil, customer.ErrOnboardingNodeCompleted
	}

	var status string
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT status FROM %s WHERE id = $1 AND flow_id = $2 AND node_key = 'CONTRACT' AND enabled FOR UPDATE`,
		s.table("onboarding_node")), in.NodeID, in.FlowID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrOnboardingNodeNotFound
		}
		return nil, mapOnboardingError(err)
	}
	if status == customer.OnboardingNodeCompleted {
		return nil, customer.ErrOnboardingNodeCompleted
	}
	if status != customer.OnboardingNodeInProgress {
		return nil, customer.ErrOnboardingNodeNotReady
	}

	now := time.Now().UTC()
	// 节点 note 同时记录「当前是按办理人确认完成的」这件事：
	// 客户版 F12 provider 尚未实现，若不写清楚，后来人会以为这里漏了就绪校验。
	note := strings.TrimSpace(in.Reason) + "\nF12 就绪能力待客户版 provider 就绪后启用（当前按办理人确认完成）"
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s SET status = 'COMPLETED', completed_at = $2, note = $3 WHERE id = $1`,
		s.table("onboarding_node")), in.NodeID, now, note); err != nil {
		return nil, mapOnboardingError(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s SET status = 'IN_PROGRESS' WHERE flow_id = $1 AND enabled AND node_key = 'CONFIG' AND status = 'WAITING_PREDECESSOR'`,
		s.table("onboarding_node")), in.FlowID); err != nil {
		return nil, mapOnboardingError(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s SET current_node = 'CONFIG', updated_at = $2 WHERE id = $1`,
		s.table("onboarding_flow")), in.FlowID, now); err != nil {
		return nil, mapOnboardingError(err)
	}
	after := map[string]any{
		"node_key": "CONTRACT", "node_status": "COMPLETED", "current_node": "CONFIG",
		"confirmation": "合同与授信已归档（办理人确认，客户侧不走 F12 就绪快照）",
	}
	if in.F12Reference != "" {
		after["f12_reference"] = in.F12Reference
	}
	if err := s.auditFlow(ctx, tx, in.FlowID, in.OperatorID, "COMPLETE_CONTRACT",
		map[string]any{"node_key": "CONTRACT", "node_status": status, "current_node": "CONTRACT"},
		after,
		in.Reason, in.RequestID); err != nil {
		return nil, mapOnboardingError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapOnboardingError(err)
	}
	return s.GetFlow(ctx, ownerID)
}

// GetFlow 读该客户最近一条开通流程（含节点、模型明细、绑定）。
func (s *CustomerOnboardingStore) GetFlow(ctx context.Context, customerProfileID int64) (*customer.OnboardingFlow, error) {
	flowQuery := fmt.Sprintf(`
SELECT id, owner_id, status, COALESCE(current_node, ''), created_at, updated_at
  FROM %s WHERE kind = 'CUSTOMER' AND owner_id = $1 ORDER BY id DESC LIMIT 1`, s.table("onboarding_flow"))
	flow := customer.OnboardingFlow{CustomerProfileID: customerProfileID}
	if err := s.db.QueryRowContext(ctx, flowQuery, customerProfileID).Scan(
		&flow.ID, &flow.CustomerProfileID, &flow.Status, &flow.CurrentNode, &flow.CreatedAt, &flow.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // 由领域层翻译成 ErrOnboardingFlowNotFound
		}
		return nil, mapOnboardingError(err)
	}

	nodesQuery := fmt.Sprintf(`
SELECT n.id, n.node_key, n.status, COALESCE(n.assignee_id, 0), COALESCE(st.name, ''), n.completed_at, COALESCE(n.note, '')
  FROM %s n LEFT JOIN %s st ON st.id = n.assignee_id
 WHERE n.flow_id = $1 AND n.enabled
 ORDER BY CASE n.node_key WHEN 'CONTRACT' THEN 1 WHEN 'CONFIG' THEN 2 ELSE 3 END, n.id`,
		s.table("onboarding_node"), s.table("internal_staff"))
	rows, err := s.db.QueryContext(ctx, nodesQuery, flow.ID)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	flow.Nodes = make([]customer.OnboardingNode, 0, 2)
	for rows.Next() {
		var node customer.OnboardingNode
		var completed sql.NullTime
		if err := rows.Scan(&node.ID, &node.Key, &node.Status, &node.AssigneeID, &node.AssigneeName, &completed, &node.Note); err != nil {
			_ = rows.Close()
			return nil, mapOnboardingError(err)
		}
		if completed.Valid {
			value := completed.Time.UTC()
			node.CompletedAt = &value
		}
		flow.Nodes = append(flow.Nodes, node)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, mapOnboardingError(err)
	}
	_ = rows.Close()

	modelsQuery := fmt.Sprintf(`
SELECT m.standard_model_id, COALESCE(sm.name, ''), m.resource_id, COALESCE(s.name, ''), COALESCE(r.region, '')
  FROM %s m
  JOIN %s sm ON sm.id = m.standard_model_id
  JOIN %s r ON r.id = m.resource_id
  JOIN %s s ON s.id = r.supplier_id
 WHERE m.flow_id = $1 ORDER BY m.id`,
		s.table("customer_onboarding_model"), s.table("standard_model"), s.table("resource"), s.table("supplier"))
	modelRows, err := s.db.QueryContext(ctx, modelsQuery, flow.ID)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	flow.Models = make([]customer.OnboardingModelRow, 0, 4)
	for modelRows.Next() {
		var row customer.OnboardingModelRow
		if err := modelRows.Scan(&row.StandardModelID, &row.ModelName, &row.ResourceID, &row.SupplierName, &row.Region); err != nil {
			_ = modelRows.Close()
			return nil, mapOnboardingError(err)
		}
		flow.Models = append(flow.Models, row)
	}
	if err := modelRows.Err(); err != nil {
		_ = modelRows.Close()
		return nil, mapOnboardingError(err)
	}
	_ = modelRows.Close()

	linksQuery := fmt.Sprintf(`
SELECT %s FROM %s
 WHERE customer_profile_id = $1 AND archived = FALSE
   AND standard_model_id IN (SELECT standard_model_id FROM %s WHERE flow_id = $2)
 ORDER BY id`, linkColumns, s.table("customer_resource_link"), s.table("customer_onboarding_model"))
	linkRows, err := s.db.QueryContext(ctx, linksQuery, customerProfileID, flow.ID)
	if err != nil {
		return nil, mapOnboardingError(err)
	}
	flow.Links = make([]customer.ResourceLink, 0, 4)
	for linkRows.Next() {
		link, err := scanLink(linkRows)
		if err != nil {
			_ = linkRows.Close()
			return nil, mapOnboardingError(err)
		}
		flow.Links = append(flow.Links, *link)
	}
	if err := linkRows.Err(); err != nil {
		_ = linkRows.Close()
		return nil, mapOnboardingError(err)
	}
	_ = linkRows.Close()
	return &flow, nil
}
