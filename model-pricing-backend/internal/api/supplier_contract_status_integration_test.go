package api

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// 供应商列表「合同状态」的派生列回归（B3a 第 4 项，**存量缺陷修复**）。
//
// 缺陷：`supply_chain.go:332` 只认 `n.status='DONE'`，而所有活路径写的都是 `'COMPLETED'`
// （`native_handling_approval.go:273`、`supply_native_contract.go:71`），
// 于是「已归档」分支永不命中 —— 供应商合同授信节点已完成时列表仍显示「未登记」。
//
// 判据（已与团队确认）：**「已归档」= 至少存在一条 enabled 且已完成的 CONTRACT 节点**，
// 不是"当前流程已完成"。所以本测试同时钉住三面：
//   - 节点 COMPLETED → 已归档（修复的目标）
//   - 节点 IN_PROGRESS → 办理中（反向断言：未完成绝不能显示已归档）
//   - 节点 DONE（历史词）→ 已归档（与 000051 视图同口径，兼容历史数据）

// createTestSupplier 建一条最小供应商（supplier 表只要 name + region）。
func createTestSupplier(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO public.supplier (name, region) VALUES ($1, 'CN') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("insert supplier: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.ExecContext(ctx, `DELETE FROM public.onboarding_flow WHERE kind='SUPPLIER' AND owner_id=$1`, id)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.supplier WHERE id=$1`, id)
	})
	return id
}

// createContractNode 建 flow + CONTRACT 节点，返回 nodeID。
func createContractNode(t *testing.T, db *sql.DB, supplierID int64, status string) int64 {
	t.Helper()
	ctx := context.Background()
	var flowID int64
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.onboarding_flow (kind, owner_id, status, current_node)
VALUES ('SUPPLIER', $1, 'PROCESSING', 'CONTRACT') RETURNING id`, supplierID).Scan(&flowID); err != nil {
		t.Fatalf("insert flow: %v", err)
	}
	var nodeID int64
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.onboarding_node (flow_id, node_key, status, enabled)
VALUES ($1, 'CONTRACT', $2, TRUE) RETURNING id`, flowID, status).Scan(&nodeID); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	return nodeID
}

// supplierContractStatus 读列表里该供应商的「合同状态」。
func supplierContractStatus(t *testing.T, rt *SupplyChainRuntime, name string) string {
	t.Helper()
	page, err := rt.ListSuppliersPage(context.Background(), SupplierWorkspaceQuery{
		Page: 1, Size: 20, Keyword: name, SortBy: "id", SortOrder: "asc",
	})
	if err != nil {
		t.Fatalf("list suppliers: %v", err)
	}
	for _, row := range page.List {
		if row.Name == name {
			return row.ContractStatus
		}
	}
	t.Fatalf("列表里找不到供应商 %q（可能 keyword 过滤把测试数据滤掉了）", name)
	return ""
}

func TestSupplierContractStatusCountsCompletedContractNode(t *testing.T) {
	db := openB3ATestDB(t)
	rt := NewSupplyChainRuntime(db, nil)
	name := fmt.Sprintf("QA-合同状态-%d", time.Now().UnixNano())
	supplierID := createTestSupplier(t, db, name)

	// 反向断言先行：节点进行中时必须是「办理中」，不能因为放宽条件被判成已归档。
	nodeID := createContractNode(t, db, supplierID, "IN_PROGRESS")
	if got := supplierContractStatus(t, rt, name); got != "办理中" {
		t.Fatalf("节点 IN_PROGRESS 应显示「办理中」，got %q", got)
	}

	// 修复目标：节点 COMPLETED（活路径真正写的词）必须显示「已归档」。
	// 注意 contract 表里没有该供应商的 ARCHIVED 记录——正是靠这一分支才有值。
	if _, err := db.ExecContext(context.Background(),
		`UPDATE public.onboarding_node SET status='COMPLETED', completed_at=now() WHERE id=$1`, nodeID); err != nil {
		t.Fatalf("update node: %v", err)
	}
	if got := supplierContractStatus(t, rt, name); got != "已归档" {
		t.Fatalf("节点 COMPLETED 应显示「已归档」（修复前这里是「未登记」），got %q", got)
	}

	// 历史词兼容：库里确实存在 status='DONE' 的行（来源是已下线的 /simulate），
	// 放宽后必须与 COMPLETED 同口径，否则历史供应商永远显示未登记。
	if _, err := db.ExecContext(context.Background(),
		`UPDATE public.onboarding_node SET status='DONE' WHERE id=$1`, nodeID); err != nil {
		t.Fatalf("update node to DONE: %v", err)
	}
	if got := supplierContractStatus(t, rt, name); got != "已归档" {
		t.Fatalf("历史词 DONE 也应显示「已归档」，got %q", got)
	}
}

// TestSupplierContractStatusPrioritizesInProgress 钉住 CASE 的**优先级**（QA 实测发现的实现事实）：
// 「办理中」优先于「已归档」，即判据不是"任意一条已完成即已归档"，
// 而是"没有在办的 + 至少一条已完成的"。
//
// 这条很重要：如果有人为了"任意一条完成即已归档"去调换分支顺序，
// 那么"老单已归档 + 新单在办"的供应商会在列表上显示已归档，掩盖正在办的事。
func TestSupplierContractStatusPrioritizesInProgress(t *testing.T) {
	db := openB3ATestDB(t)
	rt := NewSupplyChainRuntime(db, nil)
	ctx := context.Background()

	// ---- 供应商 A：一条已完成 + 一条在办 → 办理中 ----
	nameA := fmt.Sprintf("QA-合同优先级-%d", time.Now().UnixNano())
	supplierA := createTestSupplier(t, db, nameA)
	createContractNode(t, db, supplierA, "COMPLETED")
	inProgressNode := createContractNode(t, db, supplierA, "IN_PROGRESS")
	if got := supplierContractStatus(t, rt, nameA); got != "办理中" {
		t.Fatalf("老单已完成 + 新单在办 → 应显示「办理中」（办理中优先于已归档），got %q", got)
	}
	// 把在办的那条也办完 → 才变已归档
	if _, err := db.ExecContext(ctx,
		`UPDATE public.onboarding_node SET status='COMPLETED', completed_at=now() WHERE id=$1`, inProgressNode); err != nil {
		t.Fatalf("update node: %v", err)
	}
	if got := supplierContractStatus(t, rt, nameA); got != "已归档" {
		t.Fatalf("全部办完后应显示「已归档」，got %q", got)
	}

	// ---- 供应商 B：一条已完成 + 一条已取消（enabled=false）→ 已归档 ----
	// CANCELED 节点 enabled=false，不参与"办理中"判断（那单已经整单取消），所以落到已归档。
	nameB := fmt.Sprintf("QA-合同取消-%d", time.Now().UnixNano())
	supplierB := createTestSupplier(t, db, nameB)
	createContractNode(t, db, supplierB, "COMPLETED")
	canceledNode := createContractNode(t, db, supplierB, "CANCELED")
	if _, err := db.ExecContext(ctx,
		`UPDATE public.onboarding_node SET enabled=FALSE WHERE id=$1`, canceledNode); err != nil {
		t.Fatalf("disable canceled node: %v", err)
	}
	if got := supplierContractStatus(t, rt, nameB); got != "已归档" {
		t.Fatalf("已完成的 + 已取消（enabled=false）应显示「已归档」，got %q", got)
	}
}
