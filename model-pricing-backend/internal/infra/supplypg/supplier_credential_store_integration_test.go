package supplypg

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// 主体级测试凭据集成校验（opt-in）：只在显式给了测试库 DSN 时跑。
//
//	SUPPLIER_CREDENTIAL_TEST_DSN="postgres://app:***@localhost:5433/model_bss?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestSupplierCredentialStore -v
//
// 覆盖 000046 的三条关键语义：
//  1. 未登记时 Get 返回 (nil, nil)，不是 ErrNotFound；
//  2. Upsert 传 nil Key = 留空保留原值；
//  3. RefreshOpenRowSnapshots 只刷未提交申报（DRAFT/REJECTED/WITHDRAWN）的行。
//
// 用例自己造数据、自己收尾，跑完不留残留（supplier 用的是临时行）。
func TestSupplierCredentialStore(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_CREDENTIAL_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUPPLIER_CREDENTIAL_TEST_DSN to run supplier credential store test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	store, err := NewSupplierCredentialStore(db, "public")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := t.Context()

	// 造一个临时主体 + 一张临时标准模型，结束时按外键级联清理。
	var supplierID, modelID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO public.supplier (name, region, phone) VALUES ('单测主体-凭据', 'CN', '19900000099') RETURNING id`).Scan(&supplierID); err != nil {
		t.Skipf("insert supplier: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM public.supplier WHERE id = $1`, supplierID) }()
	if err := db.QueryRowContext(ctx, `SELECT id FROM public.standard_model WHERE status = 'ACTIVE' LIMIT 1`).Scan(&modelID); err != nil {
		t.Skipf("no active standard model: %v", err)
	}

	if got, err := store.Get(ctx, supplierID); err != nil || got != nil {
		t.Fatalf("未登记时应返回 (nil, nil)，实际 %v / %v", got, err)
	}

	key := "demo-key"
	first, err := store.Upsert(ctx, supplierID, "https://a.example/v1", &key, 0)
	if err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	if first.BaseURL != "https://a.example/v1" || first.APIKey != "demo-key" {
		t.Fatalf("首次写入结果不符: %+v", first)
	}

	// 留空（nil）保留原 Key，只换 URL。
	second, err := store.Upsert(ctx, supplierID, "https://b.example/v1", nil, 0)
	if err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	if second.APIKey != "demo-key" {
		t.Fatalf("留空应保留原 Key，实际 %q", second.APIKey)
	}

	// 造一张草稿申报 + 一行，验证刷新只动未提交单。
	var draftID, pendingID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO public.submission (supplier_id, kind, region, status, revision) VALUES ($1,'MODEL_NEW','CN','DRAFT',1) RETURNING id`, supplierID).Scan(&draftID); err != nil {
		t.Fatalf("insert draft: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO public.submission (supplier_id, kind, region, status, revision) VALUES ($1,'MODEL_NEW','CN','PENDING_APPROVAL',1) RETURNING id`, supplierID).Scan(&pendingID); err != nil {
		t.Fatalf("insert pending: %v", err)
	}
	var draftRowID, pendingRowID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO public.submission_row (submission_id, standard_model_id, supplier_model_name, coefficient, test_base_url, test_fingerprint) VALUES ($1,$2,'m-1',0.8,'https://old.example/v1','old-fp') RETURNING id`, draftID, modelID).Scan(&draftRowID); err != nil {
		t.Fatalf("insert draft row: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO public.submission_row (submission_id, standard_model_id, supplier_model_name, coefficient, test_base_url, test_fingerprint) VALUES ($1,$2,'m-1',0.8,'https://old.example/v1','old-fp') RETURNING id`, pendingID, modelID).Scan(&pendingRowID); err != nil {
		t.Fatalf("insert pending row: %v", err)
	}

	if err := store.RefreshOpenRowSnapshots(ctx, supplierID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	var draftURL, draftFP, pendingURL string
	if err := db.QueryRowContext(ctx, `SELECT test_base_url, COALESCE(test_fingerprint,'') FROM public.submission_row WHERE id=$1`, draftRowID).Scan(&draftURL, &draftFP); err != nil {
		t.Fatalf("read draft row: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT test_base_url FROM public.submission_row WHERE id=$1`, pendingRowID).Scan(&pendingURL); err != nil {
		t.Fatalf("read pending row: %v", err)
	}
	if draftURL != "https://b.example/v1" {
		t.Fatalf("草稿行快照应刷成新凭据，实际 %q", draftURL)
	}
	if draftFP == "" || draftFP == "old-fp" {
		t.Fatalf("草稿行指纹应重算，实际 %q", draftFP)
	}
	if pendingURL != "https://old.example/v1" {
		t.Fatalf("已提交行不应被改动，实际 %q", pendingURL)
	}
}
