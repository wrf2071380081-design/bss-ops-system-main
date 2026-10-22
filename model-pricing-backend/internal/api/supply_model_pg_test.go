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

// Authentication is injected as a test boundary; handlers, reader, domain
// permissions and PostgreSQL queries are real. This is not main/JWT deployment.
func TestSupplyModelHTTPPostgres34Native(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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
	for _, ddl := range []string{
		`CREATE TEMP TABLE standard_model (LIKE public.standard_model INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`,
		`ALTER TABLE pg_temp.standard_model ADD PRIMARY KEY(id),ADD UNIQUE(company,official_id)`,
		`CREATE TEMP TABLE standard_model_version (LIKE public.standard_model_version INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`,
		`ALTER TABLE pg_temp.standard_model_version ADD UNIQUE(standard_model_id,version_no)`,
		`CREATE TEMP TABLE supplier(id bigint PRIMARY KEY,name text,region text) ON COMMIT DROP`,
		`CREATE TEMP TABLE resource(standard_model_id bigint,supplier_id bigint,region text) ON COMMIT DROP`,
		`INSERT INTO pg_temp.supplier VALUES(21,'HTTP供应商','CN')`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	operations := 0
	work, err := supplypg.NewModelUnitOfWork("pg_temp", func(context.Context) (*sql.Tx, error) { return tx, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { operations++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	service := supply.NewModelService(work, nil, func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "7", RequestID: "http-fixture", Reason: "测试夹具"}
	model, err := service.Create(ctx, meta, supply.ModelData{Name: "HTTP模型", Company: "Example", OfficialID: "http-model", ModelType: "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	data := model.Data
	data.Name = "HTTP模型新版"
	now = now.Add(time.Second)
	if _, err := service.Update(ctx, meta, model.ID, 1, data); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.resource VALUES($1,21,'CN')`, model.ID); err != nil {
		t.Fatal(err)
	}
	repository, err := supplypg.NewModelRepository("pg_temp", func(ctx context.Context, run func(*sql.Tx) error) error { return run(tx) })
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	group := router.Group("/api/internal")
	group.Use(func(c *gin.Context) {
		identity := c.GetHeader("X-Test-Identity")
		if identity != "anonymous" {
			actor, _ := strconv.ParseInt(identity, 10, 64)
			op := &middleware.Operator{StaffID: actor, PortalType: "INTERNAL", OperatorType: "STAFF"}
			if identity == "supplier" {
				op.StaffID = 7
				op.PortalType = "SUPPLIER"
				op.OperatorType = "SUPPLIER"
			}
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(group, SupplyDeps{Reader: supply.NewModelReader(repository)})
	call := func(path, identity string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/api/internal/standard-models"+path, nil).WithContext(ctx)
		request.Header.Set("X-Test-Identity", identity)
		request.Header.Set("X-Request-ID", "model-pg-http")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	decode := func(rec *httptest.ResponseRecorder, target any) {
		t.Helper()
		if rec.Code != 200 {
			t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Code      int             `json:"code"`
			RequestID string          `json:"requestId"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != 0 || body.RequestID != "model-pg-http" {
			t.Fatalf("response envelope %s %v", rec.Body.String(), err)
		}
		if err := json.Unmarshal(body.Data, target); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("list filters and options", func(t *testing.T) {
		query := url.Values{"companies": {`["Example"]`}, "supplier_ids": {`["21"]`}, "regions": {`["CN"]`}}
		var page supplyModelPageDTO
		decode(call("?"+query.Encode(), "8"), &page)
		if page.Total != 1 || len(page.List) != 1 || page.List[0].ID != model.ID || page.List[0].Permissions.CanEdit {
			t.Fatalf("list %+v", page)
		}
		var options supply.ModelFilterOptions
		decode(call("/filter-options?regions="+url.QueryEscape(`["CN"]`), "8"), &options)
		if len(options.Companies) != 1 || len(options.Suppliers) != 1 || options.Suppliers[0].ID != "21" {
			t.Fatalf("options %+v", options)
		}
	})
	t.Run("owner other staff and history", func(t *testing.T) {
		for _, identity := range []string{"7", "8"} {
			var current supplyModelDTO
			decode(call("/"+model.ID, identity), &current)
			if current.Name != "HTTP模型新版" || current.Revision != 2 || current.MaintainerID != "7" || current.Permissions.CanEdit != (identity == "7") {
				t.Fatalf("detail %s %+v", identity, current)
			}
		}
		var page supplyModelPageDTO
		decode(call("/"+model.ID+"/versions", "7"), &page)
		if page.Total != 2 || len(page.List) != 2 || page.List[1].Name != "HTTP模型" || page.List[0].Permissions.CanEdit || page.List[1].QuoteReadiness.Ready {
			t.Fatalf("history %+v", page)
		}
	})
	t.Run("identity and invalid query guards", func(t *testing.T) {
		for _, path := range []string{"", "/filter-options", "/" + model.ID, "/" + model.ID + "/versions"} {
			if rec := call(path, "anonymous"); rec.Code != 401 {
				t.Fatalf("anonymous %s %d", path, rec.Code)
			}
			if rec := call(path, "supplier"); rec.Code != 403 {
				t.Fatalf("supplier %s %d", path, rec.Code)
			}
		}
		for _, path := range []string{"?sort_by=id%3BDROP", "?regions=" + url.QueryEscape(`["UNKNOWN"]`), "/" + model.ID + "?staff_id=7"} {
			if rec := call(path, "8"); rec.Code != 400 {
				t.Fatalf("invalid query %s %d", path, rec.Code)
			}
		}
		if rec := call("/9223372036854775807", "8"); rec.Code != 404 {
			t.Fatalf("missing %d", rec.Code)
		}
	})
	t.Run("database failure not empty success", func(t *testing.T) {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT missing_column"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE pg_temp.standard_model DROP COLUMN official_id`); err != nil {
			t.Fatal(err)
		}
		rec := call("/"+model.ID, "8")
		if rec.Code != 500 || strings.Contains(rec.Body.String(), "official_id") || strings.Contains(rec.Body.String(), "SQLSTATE") {
			t.Fatalf("unsafe/success response %d %s", rec.Code, rec.Body.String())
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT missing_column"); err != nil {
			t.Fatal(err)
		}
	})
	if operations != 2 {
		t.Fatalf("GET generated writes: %d", operations)
	}
}
