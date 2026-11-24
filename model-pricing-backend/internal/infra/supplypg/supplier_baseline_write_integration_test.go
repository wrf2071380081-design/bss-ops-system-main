package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"model_bss/internal/domain/supply"
)

// 自助注册的供应商（无归属采购人、无可用修订号）也要能被内部维护。
//
// 业务口径（2026-11-23）：内部不做那么细的权限区分，持 M3:E 的人应当能改；
// 缺的是「归属采购人」这条历史，不应该把人挡在编辑入口外。保存时写一条
// BASELINE 基线流水，等归属人补齐后审计链自然恢复完整。
//
//	SUPPLIER_ACCOUNT_TEST_DSN="postgres://.../model_bss_srv?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestSupplierBaselineWrite -v
func TestSupplierBaselineWriteRepairsSelfRegisteredArchive(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_ACCOUNT_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUPPLIER_ACCOUNT_TEST_DSN to run baseline write test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	supplierID := seedSelfRegisteredSupplier(t, tx)
	resolve := func(context.Context) (*sql.Tx, error) { return tx, nil }
	logfn := func(ctx context.Context, tx *sql.Tx, e ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at)
VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`,
			e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	store, err := NewSupplierStore("public", db, resolve, logfn)
	if err != nil {
		t.Fatalf("new supplier store: %v", err)
	}
	svc := supply.NewSupplierService(store, time.Now)
	readProfile := func() *supply.Supplier {
		profile, err := store.loadSupplier(ctx, tx, supplierID, false)
		if err != nil {
			t.Fatalf("load supplier: %v", err)
		}
		return profile
	}

	// ① 前置：自助注册的档案链不可信，拿不到修订号。
	if got := readProfile(); got.AuditStatus != supply.SupplierAuditIncomplete || got.Revision != 0 || got.BuyerID != "" {
		t.Fatalf("self-registered seed should be INCOMPLETE with revision 0, got status=%q rev=%d buyer=%q", got.AuditStatus, got.Revision, got.BuyerID)
	}

	// ② 没有 M3:E 的人仍然不能写（护栏还在，只是不再拦维护人）。
	noMaintain := supply.WriteMeta{ActorID: "1", Reason: "无维护权限的尝试", RequestID: "baseline-denied"}
	if _, err := svc.Update(ctx, noMaintain, supplierID, 0, supplierDataFor("测试公司（越权）")); err == nil {
		t.Fatal("update without M3:E should be rejected")
	}

	// ③ 持 M3:E 的人用 expected=0 重建基线。
	meta := supply.WriteMeta{ActorID: "1", Reason: "补录联系人", RequestID: "baseline-write", CanMaintain: true}
	updated, err := svc.Update(ctx, meta, supplierID, 0, supplierDataFor("测试公司（已认领）"))
	if err != nil {
		t.Fatalf("maintainer baseline update: %v", err)
	}
	if updated.Revision != 1 || updated.Data.Name != "测试公司（已认领）" {
		t.Fatalf("baseline update should land on revision 1 with new name, got rev=%d name=%q", updated.Revision, updated.Data.Name)
	}

	// ④ 还没有归属人，所以链仍然不完整——但档案已经可写，不再被锁死。
	if got := readProfile(); got.AuditStatus != supply.SupplierAuditIncomplete {
		t.Fatalf("archive without buyer must stay INCOMPLETE, got %q", got.AuditStatus)
	}

	// ⑤ 补上归属采购人后，BASELINE 基线让审计链恢复完整（这正是它存在的意义）。
	if _, err := tx.Exec(`INSERT INTO public.assignment_log (entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by,changed_at)
VALUES('supplier',$1,'buyer_id',NULL,1,1,now())`, supplierID); err != nil {
		t.Fatalf("assign buyer: %v", err)
	}
	if got := readProfile(); got.AuditStatus != supply.SupplierAuditVerified || got.Revision != 1 {
		t.Fatalf("after assigning buyer the chain should verify, got status=%q rev=%d", got.AuditStatus, got.Revision)
	}
}

// seedSelfRegisteredSupplier 按自助注册的真实形状造数据：
// supplier 行 + 一条 buyer_id 为空、操作人落手机号的 CREATE 快照，没有 assignment_log。
func seedSelfRegisteredSupplier(t *testing.T, tx *sql.Tx) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var id string
	err := tx.QueryRow(`INSERT INTO public.supplier
(name,region,registration_no,contact,phone,established,tax_rate,settlement_currency,remark,status,created_at,updated_at)
VALUES($1,'CN','','','13800002222',false,NULL,'CNY','','ACTIVE',$2,$2)
RETURNING id::text`, "自助注册测试公司-"+now.Format("150405.000000"), now).Scan(&id)
	if err != nil {
		t.Fatalf("insert supplier: %v", err)
	}
	var name string
	if err := tx.QueryRow(`SELECT name FROM public.supplier WHERE id=$1`, id).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}
	snapshot := map[string]any{
		"id":      id,
		"data":    map[string]any{"name": name, "region": "CN", "registration_no": "", "contact": "", "phone": "13800002222", "tax_rate": nil, "settlement_currency": "CNY", "remark": ""},
		"aliases": []string{}, "status": "ACTIVE", "buyer_id": "", "revision": "1",
		"created_at": now, "updated_at": now, "established": false,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO public.operation_log (entity_type,entity_id,action,operator_name_snapshot,"after",reason,request_id,created_at)
VALUES('supplier',$1,'CREATE','13800002222',$2::jsonb,'供应商自助注册',$3,$4)`, id, string(raw), "seed-"+fmt.Sprint(now.UnixNano()), now); err != nil {
		t.Fatalf("insert create snapshot: %v", err)
	}
	return id
}

func supplierDataFor(name string) supply.SupplierData {
	return supply.SupplierData{Name: name, Region: "CN", Phone: "13800002222", SettlementCurrency: "CNY"}
}
