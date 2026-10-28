package supplypg

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// 供应商模型列表的可见行规则集成校验（opt-in）：
//
//	SUPPLIER_MODEL_TEST_DSN="postgres://app:***@localhost:5433/model_bss?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestSupplierModelStoreVisibility -v
//
// 规则来自原型 sPortalResourceRows()：
//   - 待测试/待接入且从没接入过、也没有「通过」申报的资源 → 不显示；
//   - 有过 APPROVED 申报（行里带该标准模型）→ 显示；
//   - ever_admitted=true 或 已接入/已下架 → 显示。
//
// 顺带确认 company / model_type 有带出来（原型表格要用）。
func TestSupplierModelStoreVisibility(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_MODEL_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUPPLIER_MODEL_TEST_DSN to run supplier model store test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	store, err := NewSupplierModelStore(db, "public")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := t.Context()

	var supplierID, modelID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO public.supplier (name, region) VALUES ('单测主体-模型列表', 'CN') RETURNING id`).Scan(&supplierID); err != nil {
		t.Skipf("insert supplier: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM public.submission WHERE supplier_id = $1`, supplierID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.resource WHERE supplier_id = $1`, supplierID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.supplier WHERE id = $1`, supplierID)
	}()
	if err := db.QueryRowContext(ctx, `SELECT id FROM public.standard_model LIMIT 1`).Scan(&modelID); err != nil {
		t.Skipf("no standard model: %v", err)
	}
	var resourceID int64
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.resource (standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot, region, coefficient, state, ever_admitted)
VALUES ($1, $2, 'vendor-x', '标准模型', 'CN', 0.68, 'PENDING_TEST', false) RETURNING id`, modelID, supplierID).Scan(&resourceID); err != nil {
		t.Fatalf("insert resource: %v", err)
	}

	// 1) 待测试 + 没接入过 + 没申报 → 不显示
	rows, total, err := store.List(ctx, supplierID, 1, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("未接入且无通过申报的资源不该显示，实际 total=%d rows=%d", total, len(rows))
	}

	// 2) 有 APPROVED 申报（行里带同一标准模型）→ 显示
	var submissionID int64
	if err := db.QueryRowContext(ctx, `
INSERT INTO public.submission (supplier_id, kind, region, status, revision)
VALUES ($1, 'MODEL_NEW', 'CN', 'APPROVED', 1) RETURNING id`, supplierID).Scan(&submissionID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO public.submission_row (submission_id, standard_model_id, supplier_model_name, coefficient)
VALUES ($1, $2, 'vendor-x', 0.68)`, submissionID, modelID); err != nil {
		t.Fatalf("insert submission row: %v", err)
	}
	rows, total, err = store.List(ctx, supplierID, 1, 50)
	if err != nil {
		t.Fatalf("list after submission: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != itoa64(resourceID) {
		t.Fatalf("有通过申报后应显示该资源，实际 total=%d rows=%+v", total, rows)
	}
	if rows[0].Company == "" || rows[0].ModelType == "" {
		t.Fatalf("company / model_type 没带出来：%+v", rows[0])
	}

	// 3) ever_admitted=true 也可见（不再依赖申报）
	if _, err := db.ExecContext(ctx, `UPDATE public.resource SET ever_admitted = true WHERE id = $1`, resourceID); err != nil {
		t.Fatalf("update resource: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM public.submission WHERE supplier_id = $1`, supplierID); err != nil {
		t.Fatalf("delete submission: %v", err)
	}
	if _, total, err = store.List(ctx, supplierID, 1, 50); err != nil || total != 1 {
		t.Fatalf("曾接入的资源应显示，实际 total=%d err=%v", total, err)
	}

	// 4) 分页与计数一致
	if _, total, err = store.List(ctx, supplierID, 2, 1); err != nil || total != 1 {
		t.Fatalf("分页时总数应保持 1，实际 total=%d err=%v", total, err)
	}
}
