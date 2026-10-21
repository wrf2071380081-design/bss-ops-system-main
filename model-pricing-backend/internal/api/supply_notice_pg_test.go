package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"model_bss/internal/infra/supplypg"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Real handler/reader/PostgreSQL test. Authentication context is injected;
// public main/router wiring and actual login/session validation are not tested.
func TestSupplyNoticePostgres34HTTP(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated PostgreSQL HTTP test not requested")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("explicit dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong test database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var org, id string
	staff := make([]string, 2)
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES ('A公告HTTP测试','/a-notice-http/') RETURNING id::text`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	for i := range staff {
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES ($1,$2,$3) RETURNING id::text`, org, fmt.Sprintf("HTTP人员%d", i), fmt.Sprintf("anh-%s-%d", org, i)).Scan(&staff[i]); err != nil {
			t.Fatal(err)
		}
	}
	zero := uint64(0)
	n := &supply.Notice{ResourceID: "9007199254740993", Kind: "RESOURCE_OFFLINED", ResourceName: "HTTP测试资源", Region: "CN", Title: "真实公告查询", Content: "只读查询不代办", Reason: "测试资料", AffectedStatus: "READY", AffectedCustomerCount: &zero}
	raw, err := supplypg.EncodeNoticePayload(n)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.notice(type,target_type,target_id,payload) VALUES ($1,'RESOURCE',$2,$3::jsonb) RETURNING id::text`, n.Kind, n.ResourceID, string(raw)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.notice WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
		for _, s := range staff {
			if _, err := db.ExecContext(cleanup, `DELETE FROM public.internal_staff WHERE id=$1`, s); err != nil {
				t.Error(err)
			}
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.org_unit WHERE id=$1`, org); err != nil {
			t.Error(err)
		}
	}()
	actor, _ := strconv.ParseInt(staff[0], 10, 64)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	group := router.Group("/api/internal")
	group.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Identity") != "anonymous" {
			op := supplyHTTPStaff()
			op.StaffID = actor
			if c.GetHeader("X-Test-Identity") == "supplier" {
				op.PortalType = "SUPPLIER"
				op.OperatorType = "SUPPLIER"
			}
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(group, SupplyDeps{Notices: supply.NewNoticeReader(supplypg.NewNoticeReadStore(db))})
	call := func(path, identity string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/internal"+path, nil)
		req.Header.Set("X-Test-Identity", identity)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		path, selected string
		canRead        bool
	}{
		{"/notices?resource_id=" + n.ResourceID, staff[0], true},
		{"/notices/read-states?resource_id=" + n.ResourceID + "&staff_ids=" + staff[1], staff[1], false},
		{"/notices/" + id, staff[0], true},
		{"/notices/" + id + "?staff_id=" + staff[1], staff[1], false},
	} {
		w := call(tc.path, "staff")
		if w.Code != 200 {
			t.Fatalf("%s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
		var envelope struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 {
			t.Fatalf("bad envelope %v", err)
		}
		var view supply.NoticeReadView
		if strings.Contains(tc.path, "resource_id=") {
			var page supply.NoticeReadPage
			if err := json.Unmarshal(envelope.Data, &page); err != nil || page.Total != 1 || len(page.List) != 1 {
				t.Fatalf("bad page %s %v", envelope.Data, err)
			}
			view = *page.List[0]
		} else {
			if err := json.Unmarshal(envelope.Data, &view); err != nil {
				t.Fatal(err)
			}
		}
		if view.StaffID != tc.selected || view.Permissions.CanMarkRead != tc.canRead || view.ReadStatus != "UNREAD" || view.Notice.ResourceID != n.ResourceID || view.Notice.AffectedCustomerCount == nil || *view.Notice.AffectedCustomerCount != 0 {
			t.Fatalf("wrong personal projection %+v", view)
		}
	}
	for _, tc := range []struct {
		path, identity string
		status         int
	}{
		{"/notices/" + id, "anonymous", 401}, {"/notices/" + id, "supplier", 403},
		{"/notices/" + id + "?staff_id=9223372036854775807", "staff", 404},
		{"/notices?staff_ids=" + staff[1], "staff", 400},
	} {
		if w := call(tc.path, tc.identity); w.Code != tc.status {
			t.Fatalf("%s got %d want %d: %s", tc.path, w.Code, tc.status, w.Body.String())
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.notice_read WHERE notice_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("GET wrote reads count=%d %v", count, err)
	}
}
