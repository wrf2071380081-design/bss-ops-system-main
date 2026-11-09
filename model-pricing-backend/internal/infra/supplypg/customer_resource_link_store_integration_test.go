package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"model_bss/internal/domain/customer"
)

// 客户「接入模型」绑定的仓储集成校验（opt-in）：只在显式给了测试库 DSN 时跑。
//
//	CUSTOMER_RESOURCE_LINK_TEST_DSN="postgres://<user>:<password>@localhost:5433/model_bss?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestCustomerResourceLinkStore -v
//
// 覆盖需求里「必须由真实数据库证明」的部分：
//   - 两个部分唯一索引（uk_crl_customer_resource / uk_crl_customer_model）真的生效，
//     且 23505 被翻译成可读的领域错误（不是原始 SQL 错误）；
//   - 归档后名额释放，可以重建同客户同来源的绑定；
//   - 下架 → 待处理（archived=false，客户仍可用）→ 人工确认 → 归档，状态机与 SQL 一致；
//   - 重新配置来源在同一事务内完成；
//   - 投影 available_count 只统计「同模型 + 同 region + ADMITTED」。
//
// 隔离纪律：所有测试数据都以「本次新建的 standard_model」为根，清理时按该 model 删除，
// 绝不按 customer_profile 批量删（避免在共享库上误删真实绑定）。

func openLinkTestStore(t *testing.T) (*CustomerResourceLinkStore, *sql.DB) {
	t.Helper()
	dsn := integrationTestDSN(t, "CUSTOMER_RESOURCE_LINK_TEST_DSN")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store, err := NewCustomerResourceLinkStore(db, "public")
	if err != nil {
		_ = db.Close()
		t.Fatalf("new store: %v", err)
	}
	// 用 t.Cleanup 关连接而不是 defer：t.Cleanup 是 LIFO，先注册的先最后执行，
	// 这样数据清理（后注册）会先跑完再关库。若写成 defer db.Close()，
	// 它会先于 t.Cleanup 执行，清理语句会以「database is closed」静默失败。
	t.Cleanup(func() { _ = db.Close() })
	return store, db
}

// linkFixture 是测试用的最小数据集：1 个客户 + 自建模型 + 3 条资源（CN×2、OVERSEAS×1）。
type linkFixture struct {
	customerID int64
	modelID    int64
	cnResA     int64 // 供应商甲 · CN · ADMITTED
	cnResB     int64 // 供应商乙 · CN · ADMITTED
	overseas   int64 // 供应商甲 · OVERSEAS · ADMITTED（用来证明 available_count 不跨节点）
}

func newLinkFixture(t *testing.T, db *sql.DB) *linkFixture {
	t.Helper()
	ctx := context.Background()
	var f linkFixture
	if err := db.QueryRowContext(ctx, `SELECT id FROM public.customer_profile ORDER BY id LIMIT 1`).Scan(&f.customerID); err != nil {
		t.Skipf("database has no customer_profile: %v", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM public.supplier ORDER BY id LIMIT 2`)
	if err != nil {
		t.Skipf("load suppliers: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var suppliers []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan supplier: %v", err)
		}
		suppliers = append(suppliers, id)
	}
	if len(suppliers) < 2 {
		t.Skip("need at least 2 suppliers to build two resources of the same model+region")
	}

	// 自建标准模型：避免与库里已有绑定撞车，也让清理范围收敛到本次数据。
	// official_id 带时间戳后缀：standard_model 对 (company, official_id) 有唯一约束，
	// 固定值会让「上一次失败未清理干净」变成下一次的假失败。
	officialID := fmt.Sprintf("qa-customer-link-it-%d", time.Now().UnixNano())
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.standard_model (company, model_type, name, official_id)
VALUES ('QA', 'TEXT', 'QA-客户绑定集成', $1) RETURNING id`, officialID).Scan(&f.modelID); err != nil {
		t.Fatalf("insert standard_model: %v", err)
	}
	f.cnResA = insertLinkTestResource(t, db, f.modelID, suppliers[0], officialID+"-cn-a")
	f.cnResB = insertLinkTestResource(t, db, f.modelID, suppliers[1], officialID+"-cn-b")
	f.overseas = insertOverseasResource(t, db, f.modelID, suppliers[0], officialID+"-overseas")

	t.Cleanup(func() {
		// 清理失败必须报出来：静默失败会在库里留垃圾，并让下一次运行以「重复键」假失败。
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			// 先删审计再删绑定：绑定迁移现在与审计同事务写入（operation_log.entity_type='customer_resource_link'），
			// 只删绑定会在库里留下指向已删除 link 的孤儿审计——QA 就撞到过这种残留。
			// 用子查询在一次 Exec 里完成，避免"先查 id 再删"的两步窗口。
			{`DELETE FROM public.operation_log
			   WHERE entity_type = 'customer_resource_link'
			     AND entity_id IN (SELECT id FROM public.customer_resource_link WHERE standard_model_id = $1)`, []any{f.modelID}},
			{`DELETE FROM public.customer_resource_link WHERE standard_model_id = $1`, []any{f.modelID}},
			{`DELETE FROM public.resource WHERE standard_model_id = $1`, []any{f.modelID}},
			{`DELETE FROM public.standard_model WHERE id = $1`, []any{f.modelID}},
		} {
			if _, err := db.ExecContext(context.Background(), stmt.sql, stmt.args...); err != nil {
				t.Logf("cleanup %q failed: %v", stmt.sql, err)
			}
		}
	})
	return &f
}

func insertLinkTestResource(t *testing.T, db *sql.DB, modelID, supplierID int64, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(), `
INSERT INTO public.resource (standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot,
                             region, coefficient, state, ever_admitted)
VALUES ($1, $2, $3, 'QA-客户绑定集成', 'CN', 0.5000, 'ADMITTED', TRUE) RETURNING id`,
		modelID, supplierID, name).Scan(&id); err != nil {
		t.Fatalf("insert resource %s: %v", name, err)
	}
	return id
}

func insertOverseasResource(t *testing.T, db *sql.DB, modelID, supplierID int64, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(), `
INSERT INTO public.resource (standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot,
                             region, coefficient, state, ever_admitted)
VALUES ($1, $2, $3, 'QA-客户绑定集成', 'OVERSEAS', 0.5000, 'ADMITTED', TRUE) RETURNING id`,
		modelID, supplierID, name).Scan(&id); err != nil {
		t.Fatalf("insert overseas resource %s: %v", name, err)
	}
	return id
}

// TestCustomerResourceLinkStoreUniqueIndexes 锁住两个部分唯一索引的语义与错误可读性。
func TestCustomerResourceLinkStoreUniqueIndexes(t *testing.T) {
	store, db := openLinkTestStore(t)
	f := newLinkFixture(t, db)
	ctx := context.Background()

	base := customer.CreateLinkRecord{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
		State: customer.LinkStateActive, ActivationNote: "集成测试开通",
	}
	first, err := store.CreateLink(ctx, base)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.State != customer.LinkStateActive || first.Revision != 1 || first.Archived {
		t.Fatalf("新建绑定字段不符：%+v", first)
	}

	// ① 同客户同资源重复建 → 必须报「已绑定此供应资源」而不是原始 SQL 错误。
	if _, err := store.CreateLink(ctx, base); !errors.Is(err, customer.ErrLinkDuplicateResource) {
		t.Fatalf("同资源重复：期望 ErrLinkDuplicateResource，got %v", err)
	}
	// ② 同客户同模型第二条 ACTIVE → 必须提示先归档旧绑定。
	second := base
	second.ResourceID = f.cnResB
	if _, err := store.CreateLink(ctx, second); !errors.Is(err, customer.ErrLinkModelAlreadyActive) {
		t.Fatalf("同模型第二条生效：期望 ErrLinkModelAlreadyActive，got %v", err)
	}
	// ③ 乐观锁：旧 revision 更新被拒。
	if _, err := store.ApplyTransition(ctx, customer.TransitionRecord{
		ID: first.ID, ExpectedRevision: first.Revision + 5, To: customer.LinkStateDisablePending,
	}); !errors.Is(err, customer.ErrLinkRevisionConflict) {
		t.Fatalf("revision 冲突：期望 ErrLinkRevisionConflict，got %v", err)
	}
}

// TestCustomerResourceLinkStoreOfflineLinkage 下架联动：待处理不停用客户，人工确认后才归档并释放名额。
func TestCustomerResourceLinkStoreOfflineLinkage(t *testing.T) {
	store, db := openLinkTestStore(t)
	f := newLinkFixture(t, db)
	ctx := context.Background()
	svc := customer.NewResourceLinkService(store)

	created, err := svc.Create(ctx, customer.CreateLinkInput{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
	}, 1, "it-offline")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 资源下架 → 来源待处理，但 archived 必须仍为 false（客户仍可用）。
	pending, err := svc.MarkSupplyOffline(ctx, created.ID, created.Revision, "供应商主动下架", "it-offline")
	if err != nil {
		t.Fatalf("mark supply offline: %v", err)
	}
	if pending.State != customer.LinkStateSupplyOfflinePending {
		t.Fatalf("state got %s", pending.State)
	}
	if pending.Archived {
		t.Fatal("下架不能自动停用客户（archived 必须为 false）")
	}
	if pending.StateBeforeOffline != customer.LinkStateActive {
		t.Fatalf("必须记住下架前状态，got %q", pending.StateBeforeOffline)
	}
	// 此时「同客户同模型第二条 ACTIVE」仍然不被允许（原绑定仍是未归档的 ACTIVE 语义→已变待处理，
	// 所以这一条实际放开了；这里断言的是「同资源重复」仍被挡）。
	if _, err := svc.Create(ctx, customer.CreateLinkInput{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
	}, 1, "it-offline"); !errors.Is(err, customer.ErrLinkDuplicateResource) {
		t.Fatalf("待处理期间同资源重复绑定应被拒，got %v", err)
	}

	// 人工确认停用 → SUPPLY_OFFLINE + archived。
	archived, err := svc.Act(ctx, customer.LinkActionInput{
		ID: pending.ID, Action: customer.LinkActionOfflineConfirm, Reason: "客户已迁移", ExpectedRevision: pending.Revision,
	}, 1, "it-offline")
	if err != nil {
		t.Fatalf("offline confirm: %v", err)
	}
	if archived.State != customer.LinkStateSupplyOffline || !archived.Archived {
		t.Fatalf("确认停用后必须 SUPPLY_OFFLINE + archived，got state=%s archived=%v", archived.State, archived.Archived)
	}

	// 归档释放 uk_crl_customer_resource：同客户同来源可以重新建。
	recreated, err := svc.Create(ctx, customer.CreateLinkInput{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
	}, 1, "it-offline")
	if err != nil {
		t.Fatalf("归档后应能重建同来源绑定：%v", err)
	}
	if recreated.State != customer.LinkStateActive {
		t.Fatalf("重建绑定 state got %s", recreated.State)
	}
}

// TestCustomerResourceLinkStoreRebind 重新配置来源：同一事务内归档旧的 + 建新的。
func TestCustomerResourceLinkStoreRebind(t *testing.T) {
	store, db := openLinkTestStore(t)
	f := newLinkFixture(t, db)
	ctx := context.Background()
	svc := customer.NewResourceLinkService(store)

	old, err := svc.Create(ctx, customer.CreateLinkInput{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
	}, 1, "it-rebind")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fresh, err := svc.Rebind(ctx, customer.RebindInput{
		ID: old.ID, ExpectedRevision: old.Revision, ResourceID: f.cnResB, Note: "换更便宜的来源",
	}, 1, "it-rebind")
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if fresh.ID == old.ID || fresh.ResourceID != f.cnResB || fresh.State != customer.LinkStateActive {
		t.Fatalf("新绑定不符：%+v", fresh)
	}
	reloaded, err := store.GetLink(ctx, old.ID)
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	if !reloaded.Archived {
		t.Fatal("旧绑定必须被归档，否则 uk_crl_customer_model 会挡住新绑定")
	}

	// 审计：换源是"这个客户的这个模型改由哪个货源供给"，比禁用/恢复更实质，必须留痕。
	// 断言三条：恰好 1 条、entity_id 指向新绑定、before/after 里两个 link id 都在（能看出从哪换到哪）。
	var auditCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM public.operation_log
 WHERE entity_type='customer_resource_link' AND action='REBIND' AND entity_id=$1`, fresh.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count rebind audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("换源审计应恰好 1 条，got %d", auditCount)
	}
	var before, after, requestID, operator string
	if err := db.QueryRowContext(ctx, `
SELECT "before"::text, "after"::text, COALESCE(request_id,''), COALESCE(operator_id::text,'')
  FROM public.operation_log
 WHERE entity_type='customer_resource_link' AND action='REBIND' AND entity_id=$1`, fresh.ID).
		Scan(&before, &after, &requestID, &operator); err != nil {
		t.Fatalf("load rebind audit: %v", err)
	}
	if !strings.Contains(before, fmt.Sprintf(`"old_link_id": %d`, old.ID)) ||
		!strings.Contains(before, fmt.Sprintf(`"old_resource_id": %d`, f.cnResA)) {
		t.Fatalf("before 应记录被归档的旧绑定与旧货源：%s", before)
	}
	if !strings.Contains(after, fmt.Sprintf(`"new_link_id": %d`, fresh.ID)) ||
		!strings.Contains(after, fmt.Sprintf(`"new_resource_id": %d`, f.cnResB)) ||
		!strings.Contains(after, `"archived_old": true`) {
		t.Fatalf("after 应记录新绑定、新货源与归档标记：%s", after)
	}
	if requestID != "it-rebind" || operator == "" {
		t.Fatalf("审计应带请求号与操作人：request=%q operator=%q", requestID, operator)
	}
}

// TestCustomerResourceLinkStoreForeignKeyMapping 23503 必须按约束名指向真正缺失的对象。
//
// 这条只能靠真实 PG 证明：外键约束名是 PostgreSQL 自动生成的
// （customer_resource_link_{customer_profile_id,standard_model_id,resource_id}_fkey），
// 靠读代码猜不出来；一旦名字对不上，就会静默退化成「客户绑定引用的对象不存在」兜底文案。
func TestCustomerResourceLinkStoreForeignKeyMapping(t *testing.T) {
	store, db := openLinkTestStore(t)
	f := newLinkFixture(t, db)
	ctx := context.Background()

	// 一个几乎不可能存在的 bigint（bigint 上限 - 1）。
	const ghost int64 = 9223372036854775806
	base := customer.CreateLinkRecord{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
		State: customer.LinkStateActive, ActivationNote: "外键映射测试",
	}

	missingCustomer := base
	missingCustomer.CustomerProfileID = ghost
	if _, err := store.CreateLink(ctx, missingCustomer); !errors.Is(err, customer.ErrLinkCustomerNotFound) {
		t.Fatalf("客户缺失：期望 ErrLinkCustomerNotFound，got %v", err)
	}

	missingModel := base
	missingModel.StandardModelID = ghost
	if _, err := store.CreateLink(ctx, missingModel); !errors.Is(err, customer.ErrLinkStandardModelNotFound) {
		t.Fatalf("模型缺失：期望 ErrLinkStandardModelNotFound（不能报成客户不存在），got %v", err)
	}

	missingResource := base
	missingResource.ResourceID = ghost
	if _, err := store.CreateLink(ctx, missingResource); !errors.Is(err, customer.ErrLinkResourceNotFound) {
		t.Fatalf("资源缺失：期望 ErrLinkResourceNotFound（不能报成客户不存在），got %v", err)
	}
}

// TestCustomerResourceLinkStoreProjection 投影：available_count 只统计同模型同 region 且已准入。
func TestCustomerResourceLinkStoreProjection(t *testing.T) {
	store, db := openLinkTestStore(t)
	f := newLinkFixture(t, db)
	ctx := context.Background()
	svc := customer.NewResourceLinkService(store)

	if _, err := svc.Create(ctx, customer.CreateLinkInput{
		CustomerProfileID: f.customerID, StandardModelID: f.modelID, ResourceID: f.cnResA,
	}, 1, "it-projection"); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := svc.ListCustomerModels(ctx, f.customerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var row *customer.CustomerModelService
	for i := range list {
		if list[i].StandardModelID == f.modelID {
			row = &list[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("投影里找不到本次模型 %d", f.modelID)
	}
	if row.Region != "CN" {
		t.Fatalf("region 应取绑定资源的部署节点，got %q", row.Region)
	}
	// CN 下同模型 ADMITTED 资源恰好 2 条（resA / resB），OVERSEAS 那条不能算进来。
	if row.AvailableCount != 2 {
		t.Fatalf("available_count 只统计同模型同 region 且已准入，期望 2，got %d", row.AvailableCount)
	}
	if row.BoundCount != 1 || row.ServiceState != customer.ServiceStateServing {
		t.Fatalf("投影行不符：%+v", *row)
	}

	// 把另一条 CN 资源改为已下架 → available_count 应降到 1。
	if _, err := db.ExecContext(ctx, `UPDATE public.resource SET state = 'OFFLINE' WHERE id = $1`, f.cnResB); err != nil {
		t.Fatalf("update resource: %v", err)
	}
	list, err = svc.ListCustomerModels(ctx, f.customerID)
	if err != nil {
		t.Fatalf("list again: %v", err)
	}
	for _, item := range list {
		if item.StandardModelID == f.modelID && item.AvailableCount != 1 {
			t.Fatalf("下架后 available_count 期望 1，got %d", item.AvailableCount)
		}
	}
}
