package supplypg

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"model_bss/internal/domain/supply"
)

// 门户保存必须顺带续写档案快照。
//
// 回归的是 2026-11-20 用户看到的黄色提示：供应商在门户点一次「保存」，
// 主行 updated_at 被刷新，但 operation_log 里留的是 entity_type='supplier_account'，
// 内部档案页只认 entity_type='supplier' 的完整快照并要求它与主行逐字段相等，
// 于是档案被判定为审计不完整（INCOMPLETE）、revision 归零、编辑入口关闭。
//
// 集成校验（opt-in）：只在显式给了测试库 DSN 时跑，自己造数据、自己清场。
//
//	SUPPLIER_ACCOUNT_TEST_DSN="postgres://<user>:<password>@localhost:5433/model_bss?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestAccountStoreUpdateKeepsAuditChain -v
func TestAccountStoreUpdateKeepsAuditChain(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_ACCOUNT_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUPPLIER_ACCOUNT_TEST_DSN to run audit-chain test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()

	store, err := NewAccountStore(db, "public")
	if err != nil {
		t.Fatalf("new account store: %v", err)
	}
	suppliers := &SupplierStore{db: db, schema: "public"}

	supplierID, _ := seedAuditedSupplier(t, db)
	t.Cleanup(func() { cleanupSupplier(t, db, supplierID) })

	// ① 前置：造出来的档案本身是「审计完整」的，否则后面的断言没有意义。
	if got := currentProfile(t, suppliers, supplierID); got.AuditStatus != "" || got.Revision != 1 {
		t.Fatalf("seed supplier should be audit-verified at revision 1, got status=%q revision=%d", got.AuditStatus, got.Revision)
	}

	// ② 改名字 + 改联系人：快照必须跟着走，revision 递增，旧名沉成曾用名。
	if _, err := store.Update(ctx, supplierID, AccountUpdate{
		Name: "门户快照测试公司（改）", RegistrationNo: "91310000TEST00001", Contact: "李四", Region: "CN",
	}, "张三", "req-portal-update-1"); err != nil {
		t.Fatalf("portal update: %v", err)
	}
	renamed := currentProfile(t, suppliers, supplierID)
	if renamed.AuditStatus != "" {
		t.Fatalf("portal save broke the audit chain: status=%q revision=%d", renamed.AuditStatus, renamed.Revision)
	}
	if renamed.Revision != 2 {
		t.Fatalf("rename should bump revision to 2, got %d", renamed.Revision)
	}
	if renamed.Data.Name != "门户快照测试公司（改）" || renamed.Data.Contact != "李四" {
		t.Fatalf("snapshot did not follow the row: name=%q contact=%q", renamed.Data.Name, renamed.Data.Contact)
	}
	if len(renamed.Aliases) != 1 || renamed.Aliases[0] != "门户快照测试公司" {
		t.Fatalf("old name should become an alias, got %v", renamed.Aliases)
	}
	if action := latestSupplierAction(t, db, supplierID); action != "UPDATE" {
		t.Fatalf("expected an UPDATE snapshot, got %q", action)
	}

	// ③ 内容没变只点保存：只该留一条 UPDATE_NOOP，revision 不涨，链依然自洽。
	if _, err := store.Update(ctx, supplierID, AccountUpdate{
		Name: "门户快照测试公司（改）", RegistrationNo: "91310000TEST00001", Contact: "李四", Region: "CN",
	}, "张三", "req-portal-update-2"); err != nil {
		t.Fatalf("portal noop update: %v", err)
	}
	noop := currentProfile(t, suppliers, supplierID)
	if noop.AuditStatus != "" || noop.Revision != 2 {
		t.Fatalf("noop save should keep revision 2 and stay verified, got status=%q revision=%d", noop.AuditStatus, noop.Revision)
	}
	if action := latestSupplierAction(t, db, supplierID); action != "UPDATE_NOOP" {
		t.Fatalf("expected an UPDATE_NOOP snapshot, got %q", action)
	}
}

// seedAuditedSupplier 造一条「已建档、有归属采购人、有 rev1 快照」的供应商，
// 结构和内部建档路径写出来的完全一致。
func seedAuditedSupplier(t *testing.T, db *sql.DB) (supplierID int64, buyerID int64) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)

	if err := db.QueryRow(`SELECT id FROM public.internal_staff ORDER BY id LIMIT 1`).Scan(&buyerID); err != nil {
		t.Fatalf("pick staff: %v", err)
	}
	err := db.QueryRow(`INSERT INTO public.supplier
(name,region,registration_no,contact,phone,established,tax_rate,settlement_currency,remark,status,created_at,updated_at)
VALUES('门户快照测试公司','CN','91310000TEST00001','张三','13800000000',false,6.00,'CNY','','ACTIVE',$1,$1)
RETURNING id`, now).Scan(&supplierID)
	if err != nil {
		t.Fatalf("insert supplier: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO public.assignment_log (entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by,changed_at)
VALUES('supplier',$1,'buyer_id',NULL,$2,$2,$3)`, supplierID, buyerID, now); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}

	rate := "0.06000000"
	snapshot := supply.Supplier{
		ID: fmt.Sprint(supplierID), BuyerID: fmt.Sprint(buyerID),
		Data: supply.SupplierData{
			Name: "门户快照测试公司", Region: "CN", RegistrationNo: "91310000TEST00001",
			Contact: "张三", Phone: "13800000000", TaxRate: &rate, SettlementCurrency: "CNY",
		},
		Status: "ACTIVE", Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal seed snapshot: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO public.operation_log
(entity_type,entity_id,action,operator_id,operator_name_snapshot,"after",reason,request_id,created_at)
VALUES('supplier',$1,'CREATE',$2,NULL,$3::jsonb,'测试建档','req-seed-supplier',$4)`,
		supplierID, buyerID, string(payload), now); err != nil {
		t.Fatalf("insert seed snapshot: %v", err)
	}
	return supplierID, buyerID
}

func cleanupSupplier(t *testing.T, db *sql.DB, supplierID int64) {
	t.Helper()
	statements := []string{
		`DELETE FROM public.operation_log WHERE entity_type IN ('supplier','supplier_account') AND entity_id=$1`,
		`DELETE FROM public.assignment_log WHERE entity_type='supplier' AND entity_id=$1`,
		`DELETE FROM public.supplier_alias WHERE supplier_id=$1`,
		`DELETE FROM public.supplier WHERE id=$1`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt, supplierID); err != nil {
			t.Fatalf("cleanup (%s): %v", stmt, err)
		}
	}
}

func currentProfile(t *testing.T, suppliers *SupplierStore, supplierID int64) *supply.Supplier {
	t.Helper()
	tx, err := suppliers.db.Begin()
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	profile, err := suppliers.loadSupplier(t.Context(), tx, fmt.Sprint(supplierID), false)
	if err != nil {
		t.Fatalf("load supplier %d: %v", supplierID, err)
	}
	return profile
}

func latestSupplierAction(t *testing.T, db *sql.DB, supplierID int64) string {
	t.Helper()
	var action string
	if err := db.QueryRow(`SELECT action FROM public.operation_log
WHERE entity_type='supplier' AND entity_id=$1 AND action IN ('CREATE','UPDATE','UPDATE_NOOP','BASELINE')
ORDER BY id DESC LIMIT 1`, supplierID).Scan(&action); err != nil {
		t.Fatalf("read latest supplier action: %v", err)
	}
	return action
}
