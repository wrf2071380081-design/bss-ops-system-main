package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"model_bss/internal/domain/customer"
)

// isUniqueViolation 判断是否撞了指定名字的唯一约束/唯一索引。
// （pgconn 对部分唯一索引也会把索引名放进 ConstraintName。）
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func timeNow() time.Time { return time.Now().UTC() }

// 客户开通流程的仓储集成校验（opt-in）：只在显式给了测试库 DSN 时跑。
//
//	CUSTOMER_ONBOARDING_TEST_DSN="postgres://<user>:<password>@localhost:5433/model_bss?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestCustomerOnboardingStore -v
//
// 这里验的都是「只有真库能证明」的东西：
//   - 首单/非首单的节点与 current_node；
//   - 部分唯一索引 uk_flow_customer_processing 真的拦得住第二条进行中流程；
//   - CONFIG 完成时按模型 1:1 写绑定；任一冲突整批回滚；
//   - CONTRACT 完成把 CONFIG 推到 IN_PROGRESS；
//   - 审计每条动作恰好一行；
//   - v_work_item 修好后客户节点的名字/区域非空、完成后 state 变 DONE。

func openOnboardingTestStore(t *testing.T) (*CustomerResourceLinkStore, *CustomerOnboardingStore, *sql.DB) {
	t.Helper()
	dsn := integrationTestDSN(t, "CUSTOMER_ONBOARDING_TEST_DSN")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	links, err := NewCustomerResourceLinkStore(db, "public")
	if err != nil {
		_ = db.Close()
		t.Fatalf("new link store: %v", err)
	}
	store, err := NewCustomerOnboardingStore(db, "public", links)
	if err != nil {
		_ = db.Close()
		t.Fatalf("new onboarding store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return links, store, db
}

type onboardingFixture struct {
	customerID int64
	modelID    int64 // 模型甲：两条 CN 已准入资源（resA / resB，不同供应商）
	resA       int64
	resB       int64
	modelBID   int64 // 模型乙：一条 CN 已准入资源（resC）
	resC       int64
	staffA     int64
	staffB     int64
}

// newOnboardingFixture 复用绑定集成测试的数据集（客户 + 自建模型 + 两条 CN 已准入资源），
// 再取两个 ACTIVE 员工当办理人；清理时按 flow / owner 精确删除。
func newOnboardingFixture(t *testing.T, db *sql.DB) *onboardingFixture {
	t.Helper()
	base := newLinkFixture(t, db)
	f := &onboardingFixture{customerID: base.customerID, modelID: base.modelID, resA: base.cnResA, resB: base.cnResB}
	// 「一个客户一个模型只能有一个生效来源」——所以一次流程要写多条绑定，必须是**不同标准模型**。
	// 这里补一个模型乙 + 一条 CN 已准入资源，用于「批量写绑定」与「整批回滚」两个用例。
	modelBName := fmt.Sprintf("qa-onboarding-model-b-%d", time.Now().UnixNano())
	if err := db.QueryRowContext(context.Background(), `
INSERT INTO public.standard_model (company, model_type, name, official_id)
VALUES ('QA', 'TEXT', 'QA-客户开通集成乙', $1) RETURNING id`, modelBName).Scan(&f.modelBID); err != nil {
		t.Fatalf("insert model B: %v", err)
	}
	supplierRows, err := db.QueryContext(context.Background(), `SELECT id FROM public.supplier ORDER BY id LIMIT 1`)
	if err != nil {
		t.Fatalf("load supplier: %v", err)
	}
	var supplierID int64
	if supplierRows.Next() {
		if err := supplierRows.Scan(&supplierID); err != nil {
			t.Fatalf("scan supplier: %v", err)
		}
	}
	_ = supplierRows.Close()
	f.resC = insertLinkTestResource(t, db, f.modelBID, supplierID, modelBName+"-cn")
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.ExecContext(ctx, `DELETE FROM public.customer_resource_link WHERE standard_model_id = $1`, f.modelBID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.resource WHERE standard_model_id = $1`, f.modelBID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.standard_model WHERE id = $1`, f.modelBID)
	})
	rows, err := db.QueryContext(context.Background(), `SELECT id FROM public.internal_staff WHERE status='ACTIVE' ORDER BY id LIMIT 2`)
	if err != nil {
		t.Fatalf("load staff: %v", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan staff: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) < 2 {
		t.Skip("need at least 2 ACTIVE internal_staff")
	}
	f.staffA, f.staffB = ids[0], ids[1]

	t.Cleanup(func() {
		ctx := context.Background()
		// 顺序：审计 → 模型明细 → 节点 → 流程（CASCADE 也会带走节点/明细，但显式删更稳）。
		if _, err := db.ExecContext(ctx, `
DELETE FROM public.operation_log
 WHERE entity_type = 'onboarding_flow'
   AND entity_id IN (SELECT id FROM public.onboarding_flow WHERE kind='CUSTOMER' AND owner_id=$1)`, f.customerID); err != nil {
			t.Logf("cleanup operation_log: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
DELETE FROM public.onboarding_flow WHERE kind='CUSTOMER' AND owner_id=$1`, f.customerID); err != nil {
			t.Logf("cleanup onboarding_flow: %v", err)
		}
	})
	return f
}

func (f *onboardingFixture) models(resourceIDs ...int64) []customer.OnboardingModelInput {
	out := make([]customer.OnboardingModelInput, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		modelID := f.modelID
		if id == f.resC {
			modelID = f.modelBID
		}
		out = append(out, customer.OnboardingModelInput{StandardModelID: modelID, ResourceID: id})
	}
	return out
}

func findNode(flow *customer.OnboardingFlow, key string) *customer.OnboardingNode {
	for i := range flow.Nodes {
		if flow.Nodes[i].Key == key {
			return &flow.Nodes[i]
		}
	}
	return nil
}

// TestCustomerOnboardingStoreFirstOrder 首单：CONTRACT 先跑，CONFIG 挂 WAITING_PREDECESSOR。
func TestCustomerOnboardingStoreFirstOrder(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)

	flow, err := svc.Start(context.Background(), customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, FirstOrder: true,
		ContractAssigneeID: f.staffA, ConfigAssigneeID: f.staffB,
		Models: f.models(f.resA), Reason: "首单开通",
	}, f.staffA, "qa-onboarding-first")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if flow.Status != customer.OnboardingFlowProcessing {
		t.Fatalf("新流程应 PROCESSING，got %s", flow.Status)
	}
	if flow.CurrentNode != customer.OnboardingNodeContract {
		t.Fatalf("首单 current_node 应为 CONTRACT，got %q", flow.CurrentNode)
	}
	contract, config := findNode(flow, "CONTRACT"), findNode(flow, "CONFIG")
	if contract == nil || config == nil {
		t.Fatalf("首单应有两个节点：%+v", flow.Nodes)
	}
	if contract.Status != customer.OnboardingNodeInProgress || contract.AssigneeID != f.staffA {
		t.Fatalf("CONTRACT 节点不符：%+v", contract)
	}
	if config.Status != customer.OnboardingNodeWaiting || config.AssigneeID != f.staffB {
		t.Fatalf("CONFIG 应为 WAITING_PREDECESSOR：%+v", config)
	}
	if len(flow.Models) != 1 || flow.Models[0].ResourceID != f.resA {
		t.Fatalf("模型明细不符：%+v", flow.Models)
	}
}

// TestCustomerOnboardingStoreNonFirstOrder 非首单：只有 CONFIG，直接 IN_PROGRESS。
func TestCustomerOnboardingStoreNonFirstOrder(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)

	flow, err := svc.Start(context.Background(), customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, FirstOrder: false,
		ConfigAssigneeID: f.staffB, Models: f.models(f.resA), Reason: "增开模型",
	}, f.staffB, "qa-onboarding-second")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if flow.CurrentNode != customer.OnboardingNodeConfig {
		t.Fatalf("非首单 current_node 应为 CONFIG，got %q", flow.CurrentNode)
	}
	if findNode(flow, "CONTRACT") != nil {
		t.Fatalf("非首单不应建 CONTRACT 节点：%+v", flow.Nodes)
	}
	config := findNode(flow, "CONFIG")
	if config == nil || config.Status != customer.OnboardingNodeInProgress {
		t.Fatalf("CONFIG 应 IN_PROGRESS：%+v", config)
	}
}

// TestCustomerOnboardingStoreProcessingBlockedByIndex 第二条进行中流程必须被**索引**拦住（不是只靠应用层检查）。
func TestCustomerOnboardingStoreProcessingBlockedByIndex(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	if _, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, ConfigAssigneeID: f.staffB, Models: f.models(f.resA),
	}, f.staffB, "qa-onboarding-idx-1"); err != nil {
		t.Fatalf("首次发起失败：%v", err)
	}

	// ① 应用层给出可读错误。
	if _, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, ConfigAssigneeID: f.staffB, Models: f.models(f.resB),
	}, f.staffB, "qa-onboarding-idx-2"); !errors.Is(err, customer.ErrOnboardingInProgress) {
		t.Fatalf("重复发起应返回 ErrOnboardingInProgress，got %v", err)
	}

	// ② 绕过应用层，直接插一条 PROCESSING：必须撞 uk_flow_customer_processing。
	_, err := db.ExecContext(ctx, `
INSERT INTO public.onboarding_flow(kind, owner_id, status, current_node)
VALUES ('CUSTOMER', $1, 'PROCESSING', 'CONFIG')`, f.customerID)
	if err == nil {
		t.Fatal("部分唯一索引未生效：同一客户出现了两条进行中流程")
	}
	if !isUniqueViolation(err, "uk_flow_customer_processing") {
		t.Fatalf("应撞 uk_flow_customer_processing，实际错误：%v", err)
	}
}

// TestCustomerOnboardingStoreCompleteConfigWritesLinks CONFIG 完成：按模型 1:1 写绑定。
func TestCustomerOnboardingStoreCompleteConfigWritesLinks(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, ConfigAssigneeID: f.staffB,
		Models: f.models(f.resA, f.resC), Reason: "两模型开通",
	}, f.staffB, "qa-onboarding-config")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	config := findNode(flow, "CONFIG")
	done, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: config.ID, Reason: "两个模型已配置完成", Operator: f.staffB, Request: "qa-onboarding-config-done",
	})
	if err != nil {
		t.Fatalf("complete config: %v", err)
	}
	if done.Status != customer.OnboardingFlowCompleted || findNode(done, "CONFIG").Status != customer.OnboardingNodeCompleted {
		t.Fatalf("CONFIG 完成后应 COMPLETED：flow=%s node=%+v", done.Status, findNode(done, "CONFIG"))
	}

	// 绑定 1:1 写入且指向正确资源（两条分属不同标准模型）。
	var rows int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM public.customer_resource_link
 WHERE customer_profile_id=$1 AND archived=FALSE AND resource_id IN ($2,$3)`, f.customerID, f.resA, f.resC).Scan(&rows); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if rows != 2 {
		t.Fatalf("应写入 2 条绑定，got %d", rows)
	}
	for _, pair := range []struct {
		name     string
		resource int64
		model    int64
	}{{"甲", f.resA, f.modelID}, {"乙", f.resC, f.modelBID}} {
		var state string
		var modelID int64
		if err := db.QueryRowContext(ctx, `
SELECT state, standard_model_id FROM public.customer_resource_link
 WHERE customer_profile_id=$1 AND resource_id=$2 AND archived=FALSE`, f.customerID, pair.resource).Scan(&state, &modelID); err != nil {
			t.Fatalf("load link %s: %v", pair.name, err)
		}
		if state != customer.LinkStateActive || modelID != pair.model {
			t.Fatalf("模型%s 的绑定不符：state=%s model=%d", pair.name, state, modelID)
		}
	}
	if len(done.Links) != 2 {
		t.Fatalf("读模型应带出 2 条绑定，got %d", len(done.Links))
	}
}

// TestCustomerOnboardingStoreCompleteConfigRollsBackOnConflict 一条冲突 = 整批回滚。
func TestCustomerOnboardingStoreCompleteConfigRollsBackOnConflict(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	// 先给「模型甲 + 资源 A」建一条 ACTIVE 绑定：
	// 之后流程里再给模型甲配资源 B，就会撞 uk_crl_customer_model（一客户一模型只能一个生效来源）。
	now := timeNow()
	if _, err := links.CreateLink(ctx, customer.CreateLinkRecord{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.resA,
		State: customer.LinkStateActive, ActivationNote: "预置生效来源", ActivatedAt: &now,
		CreatedBy: f.staffB, RequestID: "qa-pre-link",
	}); err != nil {
		t.Fatalf("pre-create link: %v", err)
	}

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, ConfigAssigneeID: f.staffB,
		Models: f.models(f.resB, f.resC), Reason: "含冲突的批量开通",
	}, f.staffB, "qa-onboarding-conflict")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	config := findNode(flow, "CONFIG")
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: config.ID, Reason: "配置完成", Operator: f.staffB, Request: "qa-onboarding-conflict",
	}); !errors.Is(err, customer.ErrLinkModelAlreadyActive) {
		t.Fatalf("应返回可读的「已有生效来源」冲突，got %v", err)
	}

	// 关键：模型乙那条（本来可以成功）**必须一起回滚**，不能留下半个批次。
	var countC int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM public.customer_resource_link WHERE customer_profile_id=$1 AND resource_id=$2`,
		f.customerID, f.resC).Scan(&countC); err != nil {
		t.Fatalf("count link 乙: %v", err)
	}
	if countC != 0 {
		t.Fatalf("冲突后模型乙的绑定必须回滚，got %d 行", countC)
	}
	// 流程与节点也不能前进。
	var flowStatus, nodeStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM public.onboarding_flow WHERE id=$1`, flow.ID).Scan(&flowStatus); err != nil {
		t.Fatalf("load flow: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM public.onboarding_node WHERE id=$1`, config.ID).Scan(&nodeStatus); err != nil {
		t.Fatalf("load node: %v", err)
	}
	if flowStatus != customer.OnboardingFlowProcessing || nodeStatus != customer.OnboardingNodeInProgress {
		t.Fatalf("失败不应推进状态：flow=%s node=%s", flowStatus, nodeStatus)
	}
}

// TestCustomerOnboardingStoreCompleteContractPromotesConfig CONTRACT 完成把 CONFIG 推到 IN_PROGRESS。
//
// 客户侧 CONTRACT 完成是「办理人确认」而非 F12 就绪（该快照只覆盖供应商），
// 所以这条走完整的 service 路径，不绕过任何校验。
func TestCustomerOnboardingStoreCompleteContractPromotesConfig(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, FirstOrder: true,
		ContractAssigneeID: f.staffA, ConfigAssigneeID: f.staffB,
		Models: f.models(f.resA), Reason: "首单",
	}, f.staffA, "qa-onboarding-contract")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	contract := findNode(flow, "CONTRACT")
	updated, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: contract.ID, Reason: "合同与授信已归档（纸质已入档）", F12Reference: "HT-QA-001",
		Operator: f.staffA, Request: "qa-onboarding-contract-done",
	})
	if err != nil {
		t.Fatalf("complete contract: %v", err)
	}
	if findNode(updated, "CONTRACT").Status != customer.OnboardingNodeCompleted {
		t.Fatalf("CONTRACT 应 COMPLETED：%+v", findNode(updated, "CONTRACT"))
	}
	if findNode(updated, "CONFIG").Status != customer.OnboardingNodeInProgress {
		t.Fatalf("CONFIG 应被推到 IN_PROGRESS：%+v", findNode(updated, "CONFIG"))
	}
	if updated.CurrentNode != customer.OnboardingNodeConfig {
		t.Fatalf("current_node 应变为 CONFIG，got %q", updated.CurrentNode)
	}
	// 节点 note 必须留下「客户版 F12 provider 未实现」的痕迹，避免后来人以为漏了就绪校验。
	contractNode := findNode(updated, "CONTRACT")
	if !strings.Contains(contractNode.Note, "F12 就绪能力待客户版 provider 就绪后启用") {
		t.Fatalf("节点 note 缺少 F12 未接的说明：%q", contractNode.Note)
	}
	if !strings.Contains(contractNode.Note, "纸质已入档") {
		t.Fatalf("节点 note 应保留办理人说明：%q", contractNode.Note)
	}
}

// TestCustomerOnboardingStoreCompleteTwiceRejected 完成后重复提交必须被拒。
func TestCustomerOnboardingStoreCompleteTwiceRejected(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, ConfigAssigneeID: f.staffB, Models: f.models(f.resA),
	}, f.staffB, "qa-onboarding-twice")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	config := findNode(flow, "CONFIG")
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{NodeID: config.ID, Reason: "完成", Operator: f.staffB, Request: "qa-1"}); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{NodeID: config.ID, Reason: "再点一次", Operator: f.staffB, Request: "qa-2"}); !errors.Is(err, customer.ErrOnboardingNodeCompleted) {
		t.Fatalf("重复完成应被拒，got %v", err)
	}
	// 也不该因此写出第二条绑定。
	var rows int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM public.customer_resource_link WHERE customer_profile_id=$1 AND standard_model_id=$2`,
		f.customerID, f.modelID).Scan(&rows); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if rows != 1 {
		t.Fatalf("重复提交不应重复写绑定，got %d", rows)
	}
}

// TestCustomerOnboardingStoreAuditRows 每个动作恰好一条审计。
func TestCustomerOnboardingStoreAuditRows(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, FirstOrder: true,
		ContractAssigneeID: f.staffA, ConfigAssigneeID: f.staffB,
		Models: f.models(f.resA), Reason: "审计用例",
	}, f.staffA, "qa-audit-start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: findNode(flow, "CONTRACT").ID, Reason: "合同与授信已归档", F12Reference: "HT-AUDIT-9",
		Operator: f.staffA, Request: "qa-audit-contract",
	}); err != nil {
		t.Fatalf("complete contract: %v", err)
	}
	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: findNode(flow, "CONFIG").ID, Reason: "资源已配", Operator: f.staffB, Request: "qa-audit-config",
	}); err != nil {
		t.Fatalf("complete config: %v", err)
	}

	assertAudit := func(action string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM public.operation_log
 WHERE entity_type='onboarding_flow' AND entity_id=$1 AND action=$2`, flow.ID, action).Scan(&got); err != nil {
			t.Fatalf("count audit %s: %v", action, err)
		}
		if got != want {
			t.Fatalf("审计 %s 期望 %d 条，got %d", action, want, got)
		}
	}
	assertAudit("START", 1)
	assertAudit("COMPLETE_CONTRACT", 1)
	assertAudit("COMPLETE_CONFIG", 1)

	// CONTRACT 审计必须记下可选凭据（合同号 / F12 记录号）。
	var reference string
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE("after"->>'f12_reference','') FROM public.operation_log
 WHERE entity_type='onboarding_flow' AND entity_id=$1 AND action='COMPLETE_CONTRACT'`, flow.ID).Scan(&reference); err != nil {
		t.Fatalf("load contract audit: %v", err)
	}
	if reference != "HT-AUDIT-9" {
		t.Fatalf("审计未记录 F12 凭据，got %q", reference)
	}

	// before/after 与 request_id 必须落上（可追溯）。
	var hasBefore, hasAfter, requestID string
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE("before"->>'node_key',''), COALESCE("after"->>'flow_status',''), COALESCE(request_id,'')
  FROM public.operation_log WHERE entity_type='onboarding_flow' AND entity_id=$1 AND action='COMPLETE_CONFIG'`, flow.ID).
		Scan(&hasBefore, &hasAfter, &requestID); err != nil {
		t.Fatalf("load audit row: %v", err)
	}
	if hasBefore != "CONFIG" || hasAfter != customer.OnboardingFlowCompleted || requestID != "qa-audit-config" {
		t.Fatalf("审计内容不符：before=%q after=%q request=%q", hasBefore, hasAfter, requestID)
	}
}

// TestCustomerOnboardingViewShowsCustomerNode v_work_item 修复验证：
// 客户节点的名字/区域必须非空，完成后 state 必须从 TODO 变 DONE。
func TestCustomerOnboardingViewShowsCustomerNode(t *testing.T) {
	links, store, db := openOnboardingTestStore(t)
	f := newOnboardingFixture(t, db)
	svc := customer.NewOnboardingService(store, links)
	ctx := context.Background()

	flow, err := svc.Start(ctx, customer.StartOnboardingInput{
		CustomerProfileID: f.customerID, ConfigAssigneeID: f.staffB, Models: f.models(f.resA),
	}, f.staffB, "qa-view")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	read := func() (bizObject, region, state string) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
SELECT COALESCE(biz_object,''), COALESCE(region,''), state
  FROM public.v_work_item
 WHERE biz_type='ONBOARDING_NODE' AND biz_id=$1 AND title='CONFIG' AND assignee_id=$2`,
			flow.ID, f.staffB).Scan(&bizObject, &region, &state); err != nil {
			t.Fatalf("read v_work_item: %v", err)
		}
		return bizObject, region, state
	}

	beforeName, beforeRegion, beforeState := read()
	if beforeName == "" {
		t.Fatal("Bug 1 未修：客户节点在收件箱里 biz_object 为空（CUSTOMER 分支没接 customer_profile）")
	}
	if beforeRegion != "CN" {
		t.Fatalf("Bug 1 未修：客户节点 region 期望 CN，got %q", beforeRegion)
	}
	if beforeState != "TODO" {
		t.Fatalf("未完成节点应为 TODO，got %q", beforeState)
	}

	if _, err := svc.CompleteNode(ctx, customer.CompleteNodeInput{
		NodeID: findNode(flow, "CONFIG").ID, Reason: "完成配置", Operator: f.staffB, Request: "qa-view-done",
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	afterName, afterRegion, afterState := read()
	if afterState != "DONE" {
		t.Fatalf("Bug 2 未修：节点 COMPLETED 后视图 state 仍为 %q（应 DONE）", afterState)
	}
	if afterName == "" || afterRegion == "" {
		t.Fatalf("完成后名字/区域仍为空：%q / %q", afterName, afterRegion)
	}
	t.Logf("视图修复实证：biz_object=%q region=%q state %q → %q", afterName, afterRegion, beforeState, afterState)
}
