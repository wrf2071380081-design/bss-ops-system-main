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
		c := context.Background()
		// 顺序：绑定 → 档案 → 主体 → 资源 → 模型 → 供应商
		if _, err := db.ExecContext(c, `
DELETE FROM public.customer_resource_link
 WHERE customer_profile_id IN (
   SELECT cp.id FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id=cp.subject_id
    WHERE ls.legal_name LIKE $1)`, f.prefix+"%"); err != nil {
			t.Logf("cleanup links: %v", err)
		}
		_, _ = db.ExecContext(c, `
DELETE FROM public.customer_profile
 WHERE id IN (SELECT cp.id FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id=cp.subject_id
               WHERE ls.legal_name LIKE $1)`, f.prefix+"%")
		_, _ = db.ExecContext(c, `DELETE FROM public.legal_subject WHERE legal_name LIKE $1`, f.prefix+"%")
		_, _ = db.ExecContext(c, `DELETE FROM public.resource WHERE standard_model_id=$1`, f.modelID)
		_, _ = db.ExecContext(c, `DELETE FROM public.standard_model WHERE id=$1`, f.modelID)
		_, _ = db.ExecContext(c, `DELETE FROM public.supplier WHERE id = ANY($1)`, supplierIDs)
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
