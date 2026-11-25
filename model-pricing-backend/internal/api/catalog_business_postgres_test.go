package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCatalogBusinessPostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated catalog DB required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var name string
	if err = db.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, "_catalog_test") {
		t.Fatal("refusing non-test database")
	}
	if _, err = db.Exec("DROP SCHEMA IF EXISTS model_catalog CASCADE"); err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile("../../migrations/000079_catalog_business.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	ddl, err = os.ReadFile("../../migrations/000080_catalog_plan_removal.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	store := NewCatalogBusinessStore(db)
	a := catalogActor{staff: 3, account: 5, edit: true, manage: true}
	b := catalogActor{staff: 4, account: 6, edit: true}
	call := func(q catalogCommand, actor catalogActor) any {
		t.Helper()
		v, e := store.execute(ctx, q, actor)
		if e != nil {
			t.Fatalf("%s: %v", q.Operation, e)
		}
		return v
	}
	claim := func(key string, actor catalogActor) string {
		t.Helper()
		v := call(catalogCommand{Operation: "claim", Vendor: "deepseek", Trigger: "MANUAL", Key: key}, actor).(gin.H)
		return v["id"].(string)
	}
	result := json.RawMessage(`{"sourceUrl":"https://api-docs.deepseek.com/quick_start/pricing","models":[{"id":"deepseek-chat","status":"NEW","contextLength":"128K"}]}`)
	complete := func(id string) {
		t.Helper()
		call(catalogCommand{Operation: "complete", Vendor: "deepseek", RunID: id, Result: result}, catalogActor{})
	}
	first := claim("manual:3:first", a)
	complete(first)
	if id := claim("manual:3:first", a); id != first {
		t.Fatal("claim was not idempotent")
	}
	if _, e := store.execute(ctx, catalogCommand{Operation: "status", Vendor: "deepseek", RunID: first}, b); !errors.Is(e, errCatalogForbidden) {
		t.Fatalf("personal run leaked: %v", e)
	}
	second := claim("manual:3:second", a)
	baseline := call(catalogCommand{Operation: "baseline", Vendor: "deepseek", RunID: second}, a).(gin.H)
	if baseline["id"] != first || baseline["kind"] != "PERSONAL" {
		t.Fatalf("wrong baseline: %v", baseline)
	}
	complete(second)
	platform := claim("daily:shared", catalogActor{})
	complete(platform)
	personalBaseline := call(catalogCommand{Operation: "baseline", Vendor: "deepseek", RunID: platform}, a).(gin.H)
	if personalBaseline["id"] != second || personalBaseline["kind"] != "PERSONAL" {
		t.Fatal("shared run lost personal-first comparison")
	}
	if _, e := store.execute(ctx, catalogCommand{Operation: "status", Vendor: "deepseek", RunID: platform}, b); e != nil {
		t.Fatal(e)
	}
	// A third employee must fall back to the shared record, never staff 3's private record.
	third := claim("manual:4:third", b)
	v := call(catalogCommand{Operation: "baseline", Vendor: "deepseek", RunID: third}, b).(gin.H)
	if v["id"] != platform || v["kind"] != "GLOBAL" {
		t.Fatalf("unsafe fallback: %v", v)
	}
	complete(third)
	call(catalogCommand{Operation: "draft-save", Vendor: "deepseek", RunID: second, ModelID: "deepseek-chat", Overrides: json.RawMessage(`{"name":"DeepSeek"}`)}, a)
	draft := call(catalogCommand{Operation: "draft-list", Vendor: "deepseek", RunID: second}, a).(json.RawMessage)
	if !strings.Contains(string(draft), "DeepSeek") {
		t.Fatal("draft missing")
	}
	pub := json.RawMessage(`{"deepseek-chat":{"id":"deepseek-chat","contextLength":"128K"}}`)
	q := catalogCommand{Operation: "approve", Vendor: "deepseek", RunID: second, ModelIDs: []string{"deepseek-chat"}, Published: pub}
	if _, e := store.execute(ctx, q, a); !errors.Is(e, errCatalogStale) {
		t.Fatalf("approved without write receipt: %v", e)
	}
	call(catalogCommand{Operation: "receipt-save", Vendor: "deepseek", RunID: second, ModelID: "deepseek-chat", Key: "write-1", Result: json.RawMessage(`{"status":"WRITTEN"}`)}, a)
	call(q, a)
	if _, e := store.execute(ctx, q, a); !errors.Is(e, errCatalogStale) {
		t.Fatalf("duplicate approval accepted: %v", e)
	}
	p := json.RawMessage(`{"scope":"platform","title":"官方采集","active":true,"localTime":"09:00","recurrence":"daily","vendors":["deepseek"]}`)
	if _, e := store.execute(ctx, catalogCommand{Operation: "plan-save", Plan: p}, b); !errors.Is(e, errCatalogForbidden) {
		t.Fatalf("unprivileged plan editor: %v", e)
	}
	saved := call(catalogCommand{Operation: "plan-save", Plan: p}, a).(gin.H)
	id := saved["id"].(string)
	stale, _ := json.Marshal(map[string]any{"id": id, "scope": "platform", "title": "stale", "active": true, "localTime": "09:00", "recurrence": "daily", "vendors": []string{"deepseek"}, "revision": 0})
	if _, e := store.execute(ctx, catalogCommand{Operation: "plan-save", Plan: stale}, a); !errors.Is(e, errCatalogStale) {
		t.Fatalf("stale edit accepted: %v", e)
	}
	if _, e := db.Exec("UPDATE model_catalog.plans SET next_at=now()-interval '1 minute' WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	due := call(catalogCommand{Operation: "plans-claim", Key: "platform"}, catalogActor{}).([]any)
	if len(due) != 1 {
		t.Fatalf("due plan not claimed: %v", due)
	}
	if again := call(catalogCommand{Operation: "plans-claim", Key: "platform"}, catalogActor{}).([]any); len(again) != 0 {
		t.Fatal("claimed slot twice")
	}
	if _, e := db.Exec("UPDATE model_catalog.plan_occurrences SET lease_until=now()-interval '1 second'"); e != nil {
		t.Fatal(e)
	}
	retry := call(catalogCommand{Operation: "plans-claim", Key: "platform"}, catalogActor{}).([]any)
	if len(retry) != 1 || retry[0].(gin.H)["requestId"] != due[0].(gin.H)["requestId"] {
		t.Fatal("restart did not reuse request")
	}
	requestID := due[0].(gin.H)["requestId"].(string)
	staleReceipt := call(catalogCommand{Operation: "plans-settle", Key: requestID, Attempt: 1, Error: "SUCCEEDED", Result: json.RawMessage(`[]`)}, catalogActor{}).(gin.H)
	if staleReceipt["rowCount"] != int64(0) {
		t.Fatal("late plan receipt replaced a reclaimed lease")
	}
	currentReceipt := call(catalogCommand{Operation: "plans-settle", Key: requestID, Attempt: 2, Error: "SUCCEEDED", Result: json.RawMessage(`[]`)}, catalogActor{}).(gin.H)
	if currentReceipt["rowCount"] != int64(1) {
		t.Fatal("current lease could not settle")
	}
	// A trusted internal-employee gateway attests shared-plan access, without granting model-edit rights.
	colleague := catalogActor{staff: 4, account: 6, manage: true}
	edit, _ := json.Marshal(map[string]any{"id": id, "scope": "platform", "title": "共享采集", "active": false, "localTime": "10:00", "recurrence": "weekly:2", "vendors": []string{"deepseek"}, "revision": 1})
	call(catalogCommand{Operation: "plan-save", Plan: edit}, colleague)
	remove, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"id": id, "revision": 2}}, "action": "delete"})
	call(catalogCommand{Operation: "plans-bulk", Plan: remove}, colleague)
	var retained int
	if e := db.QueryRow("SELECT count(*) FROM model_catalog.plan_occurrences WHERE plan_id=$1", id).Scan(&retained); e != nil || retained != 1 {
		t.Fatalf("deleted execution history: %d %v", retained, e)
	}
	listed := call(catalogCommand{Operation: "plans-list"}, colleague).(json.RawMessage)
	if strings.Contains(string(listed), id) {
		t.Fatal("removed plan still visible")
	}
	if _, e := store.execute(ctx, catalogCommand{Operation: "plan-save", Plan: edit}, colleague); !errors.Is(e, errCatalogStale) {
		t.Fatalf("removed plan was editable: %v", e)
	}
	var next time.Time
	if e := db.QueryRow("SELECT model_catalog.schedule_occurrence('monthly:31','09:00','2026-02-01+08',NULL,NULL)").Scan(&next); e != nil || next.In(time.FixedZone("CST", 8*3600)).Day() != 28 {
		t.Fatalf("monthly end: %v %v", next, e)
	}
}
