package api

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"model_bss/internal/domain/customer"
	"model_bss/internal/domain/workbench"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/repo"
)

// 收件箱 ONBOARDING_NODE deeplink 的真库回归（B3a 第 4 项）。
//
// 单元测试已经钉住"分派规则"和"每页只 +1 次查询"；这条负责钉住**真库链路**：
// `workbenchInboxSource`（6 分支 UNION）→ `FlowOwners` 批量补 kind/owner → deeplink 成型。
//
// 两个断言：
//   - 客户开通节点 → `/internal/customers/{customer_profile.id}?tab=models`
//   - 供应商办理节点 → **逐字等于**改动前的 `/internal/suppliers?keyword=<供应商名>`
//     （供应商侧一行代码都没动，这里是回归钉子）
func TestWorkbenchInboxOnboardingNodeDeeplinkByKind(t *testing.T) {
	db := openB3ATestDB(t)
	ctx := context.Background()

	// gorm 与 repo 复用同一个 *sql.DB：与生产装配一致（repo 走 gorm，读取作业走裸 SQL）。
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	svc := workbench.NewInboxService(repo.NewWorkbenchInboxRepo(gdb))

	var staffID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM public.internal_staff WHERE status='ACTIVE' ORDER BY id LIMIT 1`).Scan(&staffID); err != nil {
		t.Skipf("database has no ACTIVE internal_staff: %v", err)
	}

	label := fmt.Sprintf("QA-待办-%d", time.Now().UnixNano())
	registry, err := supplypg.NewCustomerRegistryStore(db, "public")
	if err != nil {
		t.Fatalf("registry store: %v", err)
	}

	// ---- 客户侧：customer_profile + kind=CUSTOMER 的 CONFIG 节点 ----
	customer, err := registry.CreateCustomer(ctx, customerRecord(label+"-客户", staffID))
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	var customerFlowID, customerNodeID int64
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.onboarding_flow (kind, owner_id, status, current_node)
VALUES ('CUSTOMER', $1, 'PROCESSING', 'CONFIG') RETURNING id`, customer.ID).Scan(&customerFlowID); err != nil {
		t.Fatalf("insert customer flow: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.onboarding_node (flow_id, node_key, status, enabled, assignee_id)
VALUES ($1, 'CONFIG', 'IN_PROGRESS', TRUE, $2) RETURNING id`, customerFlowID, staffID).Scan(&customerNodeID); err != nil {
		t.Fatalf("insert customer node: %v", err)
	}

	// ---- 供应商侧：supplier + kind=SUPPLIER 的 PREP 节点 ----
	// 用 PREP 而不是 CONFIG/CONTRACT：后者会带上审批留痕并被 workbench_inbox 的过滤剔除，
	// 而 PREP 正是库里那条历史 DONE 节点的真实形态，最贴近线上会遇到的情况。
	supplierName := label + "-供应商"
	var supplierID, supplierFlowID, supplierNodeID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO public.supplier (name, region) VALUES ($1,'CN') RETURNING id`, supplierName).Scan(&supplierID); err != nil {
		t.Fatalf("insert supplier: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.onboarding_flow (kind, owner_id, status, current_node)
VALUES ('SUPPLIER', $1, 'PROCESSING', 'PREP') RETURNING id`, supplierID).Scan(&supplierFlowID); err != nil {
		t.Fatalf("insert supplier flow: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.onboarding_node (flow_id, node_key, status, enabled, assignee_id)
VALUES ($1, 'PREP', 'IN_PROGRESS', TRUE, $2) RETURNING id`, supplierFlowID, staffID).Scan(&supplierNodeID); err != nil {
		t.Fatalf("insert supplier node: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		exec := func(label, query string, args ...any) {
			if _, err := db.ExecContext(c, query, args...); err != nil {
				t.Logf("cleanup %s: %v", label, err)
			}
		}
		// 客户：流程（无 FK 指向 customer_profile，必须显式删）→ 档案 → 主体
		exec("customer operation_log", `DELETE FROM public.operation_log WHERE entity_type='customer_profile' AND entity_id=$1`, customer.ID)
		exec("customer flow", `DELETE FROM public.onboarding_flow WHERE kind='CUSTOMER' AND owner_id=$1`, customer.ID)
		exec("customer profile", `DELETE FROM public.customer_profile WHERE id=$1`, customer.ID)
		exec("customer subject", `DELETE FROM public.legal_subject WHERE id=$1`, customer.SubjectID)
		// 供应商：节点随 flow 级联
		exec("supplier flow", `DELETE FROM public.onboarding_flow WHERE kind='SUPPLIER' AND owner_id=$1`, supplierID)
		exec("supplier", `DELETE FROM public.supplier WHERE id=$1`, supplierID)
	})

	res, err := svc.Inbox(ctx, workbench.InboxQuery{State: "TODO", AssigneeID: staffID, Page: 1, Size: 100}, staffID)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	found := map[int64]workbench.InboxItem{}
	for _, item := range res.List {
		if item.BizType == "ONBOARDING_NODE" && (item.BizID == customerFlowID || item.BizID == supplierFlowID) {
			found[item.BizID] = item
		}
	}
	gotCustomer, ok := found[customerFlowID]
	if !ok {
		t.Fatalf("客户节点待办未出现在收件箱里（flow=%d）", customerFlowID)
	}
	wantCustomer := fmt.Sprintf("/internal/customers/%d?tab=models", customer.ID)
	if gotCustomer.Deeplink != wantCustomer {
		t.Fatalf("客户节点 deeplink got %q want %q", gotCustomer.Deeplink, wantCustomer)
	}

	gotSupplier, ok := found[supplierFlowID]
	if !ok {
		t.Fatalf("供应商节点待办未出现在收件箱里（flow=%d）", supplierFlowID)
	}
	wantSupplier := "/internal/suppliers?keyword=" + url.QueryEscape(supplierName)
	if gotSupplier.Deeplink != wantSupplier {
		t.Fatalf("供应商节点 deeplink 必须逐字保持原行为：got %q want %q", gotSupplier.Deeplink, wantSupplier)
	}
}

// customerRecord 是建客户的最小记录（本测试只关心 id/subject_id）。
func customerRecord(name string, ownerID int64) customer.CreateCustomerRecord {
	return customer.CreateCustomerRecord{
		LegalName:            name,
		SubjectType:          "COMPANY",
		Region:               "CN",
		SettlementCurrency:   "CNY",
		OwnerSalesOperatorID: ownerID,
		PaymentType:          "PREPAID",
		BillingCycle:         30,
		VerificationStatus:   "VERIFIED",
		OperatorID:           ownerID,
		RequestID:            "qa-inbox-deeplink",
	}
}
