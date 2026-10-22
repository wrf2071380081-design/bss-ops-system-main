package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"model_bss/internal/infra/supplypg"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Native SQL, persisted control heads and real handlers/reader. Authentication
// is controlled; this does not certify main/JWT/B integration.
func TestSupplyResourceHTTPPostgres34Native(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL HTTP test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"standard_model", "standard_model_version", "supplier", "resource", "operation_log"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	logs := 0
	work, err := supplypg.NewModelUnitOfWork("pg_temp", func(context.Context) (*sql.Tx, error) { return tx, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { logs++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	model, err := supply.NewModelService(work, nil, func() time.Time { return now }).Create(ctx, supply.WriteMeta{ActorID: "7", Reason: "资源HTTP夹具", RequestID: "resource-http-fixture"}, supply.ModelData{Name: "资源HTTP模型", Company: "Example", OfficialID: "resource-http", ModelType: "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.supplier(id,name,region,password_enc) OVERRIDING SYSTEM VALUE VALUES(21,'来源一','CN','supplier-secret'),(22,'来源二','CN','supplier-secret')`); err != nil {
		t.Fatal(err)
	}
	state, err := supplypg.NewResourceStateStore("pg_temp", func(c context.Context, current *sql.Tx, e supplypg.ResourceStateLog) error {
		logs++
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"2", "10"} {
		supplier := strconv.Itoa(21 + i)
		_, err := state.Insert(ctx, tx, &supply.Resource{ID: id, StandardModelID: model.ID, SupplierID: supplier, SupplierModelID: "vendor-model", Region: "CN", Coefficient: "0.7000", BuyerID: "7", State: "PENDING_TEST", Revision: 1, OriginRowID: id + "00", OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}, supply.WriteMeta{ActorID: "7", Reason: "资源HTTP夹具", RequestID: "resource-http-fixture"}, "CREATE")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.resource SET test_base_url='https://private.invalid',test_api_key_enc='test-secret' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	facts := state.FactsLoader(nil, nil)
	unavailable := false
	repository, err := supplypg.NewResourceRepository("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, func(c context.Context, current *sql.Tx, p supplypg.ResourceProjection) (*supplypg.ResourceReadFacts, error) {
		if current != tx {
			t.Fatal("facts escaped transaction")
		}
		if unavailable {
			return nil, supply.ErrDependencyUnavailable
		}
		return facts(c, current, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	group := router.Group("/api/internal")
	group.Use(func(c *gin.Context) {
		who := c.GetHeader("X-Test-Identity")
		if who != "anonymous" {
			actor, _ := strconv.ParseInt(who, 10, 64)
			op := &middleware.Operator{StaffID: actor, PortalType: "INTERNAL", OperatorType: "STAFF"}
			if who == "supplier" {
				op.StaffID = 7
				op.PortalType = "SUPPLIER"
				op.OperatorType = "SUPPLIER"
			}
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(group, SupplyDeps{Resources: supply.NewResourceReader(repository)})
	call := func(path, identity string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/internal/resources"+path, nil).WithContext(ctx)
		req.Header.Set("X-Test-Identity", identity)
		req.Header.Set("X-Request-ID", "resource-pg-http")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		for _, secret := range []string{"supplier-secret", "test-secret", "private.invalid", "password_enc", "test_api_key_enc"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatal("secret leaked in HTTP")
			}
		}
		return rec
	}
	decode := func(path, identity string, target any) {
		t.Helper()
		rec := call(path, identity)
		if rec.Code != 200 {
			t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Code      int             `json:"code"`
			RequestID string          `json:"requestId"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != 0 || body.RequestID != "resource-pg-http" {
			t.Fatalf("bad envelope %s %v", rec.Body.String(), err)
		}
		if err := json.Unmarshal(body.Data, target); err != nil {
			t.Fatal(err)
		}
	}
	var grouped struct {
		Total int64                  `json:"total"`
		List  []supply.ResourceGroup `json:"list"`
	}
	decode("?view=grouped&size=1", "8", &grouped)
	if grouped.Total != 1 || len(grouped.List) != 1 || grouped.List[0].ResourceCount != 2 || grouped.List[0].SupplierCount != 2 || grouped.List[0].AvailableCount != 0 {
		t.Fatalf("bad group %+v", grouped)
	}
	var flat struct {
		Total int64               `json:"total"`
		List  []supplyResourceDTO `json:"list"`
	}
	decode("?view=flat&standard_model_id="+model.ID, "8", &flat)
	if flat.Total != 2 || len(flat.List) != 2 || flat.List[0].Permissions.CanEdit {
		t.Fatalf("other staff global read %+v", flat)
	}
	var detail supplyResourceDTO
	decode("/2", "7", &detail)
	if !detail.Permissions.CanEdit || detail.Revision != 1 || detail.QuoteReadiness.Eligible || detail.Test.Status != "NOT_TESTED" {
		t.Fatalf("bad own detail %+v", detail)
	}
	decode("/2", "8", &detail)
	if detail.Permissions.CanEdit {
		t.Fatal("read granted write permission")
	}
	var options supply.ResourceFilterOptions
	decode("/filter-options?region=CN", "8", &options)
	if len(options.Companies) != 1 || len(options.Suppliers) != 2 {
		t.Fatalf("options incomplete %+v", options)
	}
	for _, tc := range []struct {
		path, identity string
		status         int
	}{
		{"", "anonymous", 401}, {"", "supplier", 403}, {"/999", "8", 404}, {"/2?region=OVERSEAS", "8", 404},
		{"?view=grouped&sort_by=coefficient", "8", 400}, {"?actor_id=7", "8", 400}, {"?region=US", "8", 400},
		{"?keyword=" + url.QueryEscape(strings.Repeat("中", 201)), "8", 400},
		{"/filter-options?keyword=a%00b", "8", 400},
	} {
		if rec := call(tc.path, tc.identity); rec.Code != tc.status {
			t.Fatalf("%s %s: HTTP %d", tc.path, tc.identity, rec.Code)
		}
	}
	unavailable = true
	if rec := call("/2", "8"); rec.Code != 503 {
		t.Fatal("dependency error became empty success", rec.Code)
	}
	unavailable = false
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,"after") VALUES('resource',2,'RESOURCE_REVISION','{"contract":"broken"}')`); err != nil {
		t.Fatal(err)
	}
	if rec := call("/2", "8"); rec.Code != 500 {
		t.Fatal("missing persisted facts accepted", rec.Code)
	}
	if logs != 3 {
		t.Fatalf("GET changed business/audit state %d", logs)
	}
}
