package api

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/auth"
	"model_bss/internal/domain/customer"
	"model_bss/internal/infra/supplypg"
)

// 客户列表「已开通模型数」真库回归（B3a 第 3 项）。
//
// 口径（team-lead 已定，依据原型 proto-app.js 的
// new Set(links.filter(l => !l.archived && l.state==='已开通').map(l=>l.model)).size）：
//   - 只算 `archived = FALSE AND state = 'ACTIVE'`
//   - DISABLED / DISABLE_PENDING / RESTORE_PENDING / SUPPLY_OFFLINE_PENDING 都不计入
//   - 必须按 standard_model_id 去重（同一客户同一模型可同时有 ACTIVE + DISABLED 两条未归档绑定）
//
// 本测试钉住三种输入：ACTIVE+未归档 DISABLED 共存的模型 → 1；只有 DISABLED → 0；无绑定 → 0。

type cwFixture struct {
	prefix      string
	ownerID     int64
	modelID     int64
	resActive   int64
	resDisabled int64
}

// openCustomerWorkspaceTest 建最小数据集：1 个员工（销售归属）、2 个供应商、1 个标准模型、
// 2 条同模型同区域资源（分属两个供应商，规避 uk_resource_supplier_model_region）。
func openCustomerWorkspaceTest(t *testing.T, db *sql.DB) *cwFixture {
	t.Helper()
	ctx := context.Background()
	f := &cwFixture{prefix: fmt.Sprintf("QA-已开通-%d", time.Now().UnixNano())}
	var ownerName string
	if err := db.QueryRowContext(ctx,
		`SELECT id, name FROM public.internal_staff WHERE status='ACTIVE' ORDER BY id LIMIT 1`).
		Scan(&f.ownerID, &ownerName); err != nil {
		t.Skipf("database has no ACTIVE internal_staff: %v", err)
	}

	supplierIDs := make([]int64, 0, 2)
	for i := 0; i < 2; i++ {
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO public.supplier (name, region) VALUES ($1,'CN') RETURNING id`,
			fmt.Sprintf("%s-供应商%d", f.prefix, i+1)).Scan(&id); err != nil {
			t.Fatalf("insert supplier: %v", err)
		}
		supplierIDs = append(supplierIDs, id)
	}

	if err := db.QueryRowContext(ctx, `
INSERT INTO public.standard_model (company, model_type, name, official_id)
VALUES ('QA','TEXT',$1,$1) RETURNING id`, f.prefix).Scan(&f.modelID); err != nil {
		t.Fatalf("insert standard_model: %v", err)
	}
	for i, supplierID := range supplierIDs {
		var id int64
		if err := db.QueryRowContext(ctx, `
INSERT INTO public.resource (standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot,
                             region, coefficient, state, ever_admitted)
VALUES ($1,$2,$3,$4,'CN',0.5000,'ADMITTED',TRUE) RETURNING id`,
			f.modelID, supplierID, fmt.Sprintf("%s-res%d", f.prefix, i+1), f.prefix).Scan(&id); err != nil {
			t.Fatalf("insert resource: %v", err)
		}
		if i == 0 {
			f.resActive = id
		} else {
			f.resDisabled = id
		}
	}

	t.Cleanup(func() {
		// 顺序很重要，而且每一步都要把错误打出来：
		// 之前这里用了 `_, _ =` 静默忽略，结果 resource 删不掉 → supplier 也删不掉，
		// 库里留了 6 个 QA 供应商 / 3 个模型；另外 onboarding_flow.owner_id 是**普通 bigint**
		// （没有 FK 指向 customer_profile），删档案不会带走它，必须显式删，否则留下孤儿流程。
		cleanupCustomerFixtures(t, db, f.prefix, f.modelID, supplierIDs)
	})
	return f
}

func (f *cwFixture) createCustomer(t *testing.T, store *supplypg.CustomerRegistryStore, suffix string) *customer.CustomerProfile {
	t.Helper()
	profile, err := store.CreateCustomer(context.Background(), customer.CreateCustomerRecord{
		LegalName:            f.prefix + "-" + suffix,
		SubjectType:          customer.SubjectTypeCompany,
		Region:               "CN",
		SettlementCurrency:   "CNY",
		OwnerSalesOperatorID: f.ownerID,
		PaymentType:          "PREPAID",
		BillingCycle:         30,
		VerificationStatus:   "VERIFIED",
		OperatorID:           f.ownerID,
		RequestID:            "qa-opened-models",
	})
	if err != nil {
		t.Fatalf("create customer %s: %v", suffix, err)
	}
	return profile
}

func (f *cwFixture) link(t *testing.T, store *supplypg.CustomerResourceLinkStore, profile *customer.CustomerProfile, resourceID int64, state string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := store.CreateLink(context.Background(), customer.CreateLinkRecord{
		CustomerProfileID: profile.ID,
		StandardModelID:   f.modelID,
		ResourceID:        resourceID,
		State:             state,
		ActivationNote:    "已开通模型数回归",
		ActivatedAt:       &now,
		CreatedBy:         f.ownerID,
		RequestID:         "qa-opened-models",
	}); err != nil {
		t.Fatalf("create link(%s): %v", state, err)
	}
}

func TestCustomerWorkspaceOpenedModelCount(t *testing.T) {
	db := openB3ATestDB(t)
	f := openCustomerWorkspaceTest(t, db)
	registry, err := supplypg.NewCustomerRegistryStore(db, "public")
	if err != nil {
		t.Fatalf("registry store: %v", err)
	}
	links, err := supplypg.NewCustomerResourceLinkStore(db, "public")
	if err != nil {
		t.Fatalf("link store: %v", err)
	}

	// ① ACTIVE + 未归档 DISABLED 落在同一个模型上 → 去重后必须是 1
	both := f.createCustomer(t, registry, "active+disabled")
	f.link(t, links, both, f.resActive, customer.LinkStateActive)
	f.link(t, links, both, f.resDisabled, customer.LinkStateDisabled)

	// ② 只有 DISABLED → 0（"曾经有过关系"不算已开通）
	disabledOnly := f.createCustomer(t, registry, "disabled-only")
	f.link(t, links, disabledOnly, f.resDisabled, customer.LinkStateDisabled)

	// ③ 完全没有绑定 → 0（不能是 null / 报错）
	noLinks := f.createCustomer(t, registry, "no-links")

	rt := NewCustomerWorkspaceRuntime(db)
	op := &middleware.Operator{DataScope: auth.ScopeALL}
	rows, total, err := rt.ListCustomers(context.Background(), op, f.prefix, 1, 20)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if total < 3 || len(rows) < 3 {
		t.Fatalf("期望至少 3 个测试客户，got total=%d rows=%d", total, len(rows))
	}

	byID := map[int64]CustomerWorkspaceRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	assertCount := func(label string, profile *customer.CustomerProfile, want int64) {
		t.Helper()
		row, ok := byID[profile.ID]
		if !ok {
			t.Fatalf("%s：列表里找不到客户 %d", label, profile.ID)
		}
		if row.OpenedModelCount != want {
			t.Fatalf("%s：opened_model_count 期望 %d，got %d", label, want, row.OpenedModelCount)
		}
		// 顺手做原字段回归：新增列不能把既有列挤错位。
		if row.Name == "" || row.Region != "CN" || row.Status != "ACTIVE" || row.ContractStatus != "待记录" {
			t.Fatalf("%s：原有字段被挤错位或丢失：%+v", label, row)
		}
		if !strings.HasPrefix(row.Name, f.prefix) {
			t.Fatalf("%s：客户名不符：%q", label, row.Name)
		}
	}
	assertCount("ACTIVE+未归档DISABLED 同模型", both, 1)
	assertCount("只有 DISABLED", disabledOnly, 0)
	assertCount("无绑定", noLinks, 0)
}

// cleanupCustomerFixtures 按「测试前缀 + 已知 id」精确清理，且每步都记录错误。
//
// 关键点（踩过一次的坑）：
//   - `onboarding_flow.owner_id` 是普通 bigint，没有指向 customer_profile 的外键，
//     删档案**不会**级联带走流程 → 必须先按 owner 删流程（节点与模型明细随 CASCADE 走）
//   - 先删 operation_log（它没有外键，但删完流程就查不到 entity_id 了）
//   - resource → standard_model → supplier 是链式外键，任何一步失败后续都会失败，
//     所以错误必须打出来，不能静默
func cleanupCustomerFixtures(t *testing.T, db *sql.DB, prefix string, modelID int64, supplierIDs []int64) {
	t.Helper()
	ctx := context.Background()
	exec := func(label, query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Logf("cleanup %s failed: %v", label, err)
		}
	}
	like := prefix + "%"

	profileIDs := queryIDList(t, db, `
SELECT cp.id FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id = cp.subject_id
 WHERE ls.legal_name LIKE $1`, like)
	if len(profileIDs) > 0 {
		flowIDs := queryIDList(t, db,
			`SELECT id FROM public.onboarding_flow WHERE kind='CUSTOMER' AND owner_id = ANY($1)`, profileIDs)
		if len(flowIDs) > 0 {
			exec("operation_log(flow)", `DELETE FROM public.operation_log WHERE entity_type='onboarding_flow' AND entity_id = ANY($1)`, flowIDs)
		}
		exec("operation_log(profile)", `DELETE FROM public.operation_log WHERE entity_type='customer_profile' AND entity_id = ANY($1)`, profileIDs)
		// 流程先删：节点与 customer_onboarding_model 通过 ON DELETE CASCADE 一起走。
		exec("onboarding_flow", `DELETE FROM public.onboarding_flow WHERE kind='CUSTOMER' AND owner_id = ANY($1)`, profileIDs)
		exec("customer_resource_link", `DELETE FROM public.customer_resource_link WHERE customer_profile_id = ANY($1)`, profileIDs)
	}
	exec("customer_profile", `
DELETE FROM public.customer_profile
 WHERE id IN (SELECT cp.id FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id = cp.subject_id
               WHERE ls.legal_name LIKE $1)`, like)
	exec("legal_subject", `DELETE FROM public.legal_subject WHERE legal_name LIKE $1`, like)
	exec("resource", `DELETE FROM public.resource WHERE standard_model_id = $1`, modelID)
	exec("standard_model", `DELETE FROM public.standard_model WHERE id = $1`, modelID)
	exec("supplier", `DELETE FROM public.supplier WHERE id = ANY($1)`, supplierIDs)
}

func queryIDList(t *testing.T, db *sql.DB, query string, args ...any) []int64 {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Logf("cleanup query failed: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, 4)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Logf("cleanup scan failed: %v", err)
			return ids
		}
		ids = append(ids, id)
	}
	return ids
}
