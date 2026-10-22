package api

import (
	"context"
	"encoding/json"
	"fmt"
	"model_bss/internal/api/middleware"
	infra "model_bss/internal/infra/middleware"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSupplyRouteModuleCommitsBusinessLogAndIdempotencyTogether(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated test database required")
	}
	options := newCompositionOptions(t)
	raw := options.DB
	raw.SetMaxOpenConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	label := fmt.Sprintf("f1-module-%d", time.Now().UnixNano())
	var orgID, staffID string
	modelName, requestKey := label+"-model", label+"-key"
	defer func() {
		cleanup := func(query string, args ...any) {
			if _, err := raw.ExecContext(context.Background(), query, args...); err != nil {
				t.Errorf("cleanup failed: %v", err)
			}
		}
		cleanup(`DELETE FROM operation_log WHERE entity_type='standard_model' AND entity_id IN (SELECT id FROM standard_model WHERE company='Example' AND official_id=$1)`, label)
		cleanup(`DELETE FROM standard_model WHERE company='Example' AND official_id=$1`, label)
		cleanup(`DELETE FROM idempotency_key WHERE request_id=$1`, requestKey)
		if staffID != "" {
			cleanup(`DELETE FROM internal_staff WHERE id=$1`, staffID)
		}
		if orgID != "" {
			cleanup(`DELETE FROM org_unit WHERE id=$1`, orgID)
		}
	}()
	if err := raw.QueryRowContext(ctx, `INSERT INTO org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `INSERT INTO internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, orgID, label, fmt.Sprint(time.Now().UnixNano())).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	staffNumber, err := strconv.ParseInt(staffID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	options.Clock = func() time.Time { return time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC) }
	module, err := NewSupplyRouteModule(options)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	internal := router.Group("/api/internal")
	internal.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: staffNumber, OperatorID: staffNumber})
		c.Next()
	})
	RegisterSupplyRoutes(internal, module.Deps)
	body := fmt.Sprintf(`{"company":"Example","official_id":%q,"name":%q,"model_type":"TEXT","reason":"模块接线验收"}`, label, modelName)
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/internal/standard-models", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", requestKey)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	first := call()
	if first.Code != http.StatusOK {
		t.Fatalf("first create returned %d: %s", first.Code, first.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.ID == "" {
		t.Fatalf("invalid create response %s: %v", first.Body.String(), err)
	}
	var createdBy, versionCount, logCount, idemStatus string
	if err := raw.QueryRowContext(ctx, `SELECT created_by::text FROM standard_model WHERE id=$1`, envelope.Data.ID).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT count(*)::text FROM standard_model_version WHERE standard_model_id=$1`, envelope.Data.ID).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT count(*)::text FROM operation_log WHERE entity_type='standard_model' AND entity_id=$1 AND operator_id=$2 AND reason='模块接线验收'`, envelope.Data.ID, staffID).Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT status FROM idempotency_key WHERE request_id=$1`, requestKey).Scan(&idemStatus); err != nil {
		t.Fatal(err)
	}
	if createdBy != staffID || versionCount != "1" || logCount != "1" || idemStatus != "DONE" {
		t.Fatalf("non-atomic F1 effects created_by=%s versions=%s logs=%s idempotency=%s", createdBy, versionCount, logCount, idemStatus)
	}
	second := call()
	if second.Code != http.StatusOK {
		t.Fatalf("idempotent replay returned %d: %s", second.Code, second.Body.String())
	}
	var replayData struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &replayData); err != nil || replayData.Data.ID != envelope.Data.ID {
		t.Fatalf("replay changed result %s: %v", second.Body.String(), err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT count(*)::text FROM operation_log WHERE entity_type='standard_model' AND entity_id=$1`, envelope.Data.ID).Scan(&logCount); err != nil || logCount != "1" {
		t.Fatalf("replay duplicated business log count=%s err=%v", logCount, err)
	}
}

func TestSupplyRouteModuleReadsV36SchemaThroughRealHandlers(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL route test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated A test database required")
	}
	options := newCompositionOptions(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var database string
	var hasCacheStats, hasWorkItemView, hasApprovalFlag bool
	err := options.DB.QueryRowContext(ctx, `
SELECT current_database(),
       EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='test_report' AND column_name='cache_hit_tokens'),
       EXISTS (SELECT 1 FROM information_schema.views WHERE table_schema='public' AND table_name='v_work_item'),
       EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='internal_staff' AND column_name='can_approve_supplier')`).
		Scan(&database, &hasCacheStats, &hasWorkItemView, &hasApprovalFlag)
	if err != nil {
		t.Fatal(err)
	}
	if database != name || !hasCacheStats || !hasWorkItemView || !hasApprovalFlag {
		t.Fatalf("expected migrated 33-36 test schema, database=%s cache=%t work_item=%t approval=%t", database, hasCacheStats, hasWorkItemView, hasApprovalFlag)
	}
	label := fmt.Sprintf("route-module-%d", time.Now().UnixNano())
	var orgID, staffID string
	if err := options.DB.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if staffID != "" {
			if _, err := options.DB.ExecContext(context.Background(), `DELETE FROM public.internal_staff WHERE id=$1`, staffID); err != nil {
				t.Errorf("cleanup test staff failed: %v", err)
			}
		}
		if _, err := options.DB.ExecContext(context.Background(), `DELETE FROM public.org_unit WHERE id=$1`, orgID); err != nil {
			t.Errorf("cleanup test org failed: %v", err)
		}
	}()
	if err := options.DB.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, orgID, label, fmt.Sprint(time.Now().UnixNano())).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	staffNumber, err := strconv.ParseInt(staffID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	module, err := NewSupplyRouteModule(options)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: staffNumber, OperatorID: staffNumber})
		c.Next()
	})
	RegisterSupplyRoutes(router.Group("/api/internal"), module.Deps)
	for _, path := range []string{
		"/api/internal/standard-models?page=1&size=2",
		"/api/internal/resources?view=grouped&page=1&size=2",
		"/api/internal/notices?page=1&size=2",
		"/api/internal/plans?page=1&size=2",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Errorf("GET %s returned %d: %s", path, res.Code, res.Body.String())
		}
	}
}
