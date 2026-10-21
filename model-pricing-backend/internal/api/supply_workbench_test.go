package api

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type noticeHTTPStore struct{}

func (noticeHTTPStore) GetNoticeReadState(_ context.Context, id, staff string) (supply.NoticeReadRecord, error) {
	if id != "51" {
		return supply.NoticeReadRecord{}, supply.ErrNotFound
	}
	return supply.NoticeReadRecord{Notice: &supply.Notice{ID: "51", ResourceID: "31", Region: "CN", Kind: "RESOURCE_OFFLINED", Title: "资源公告", Content: "下架说明", Reason: "调整", AffectedStatus: "PENDING", PublishedAt: supplyHTTPNow}, StaffID: staff, StaffName: "人员" + staff}, nil
}
func (s noticeHTTPStore) ListNoticeReadStates(ctx context.Context, q supply.NoticeQuery) ([]supply.NoticeReadRecord, int64, error) {
	ids := q.StaffIDs
	if q.AllStaff {
		ids = []string{"7", "8"}
	}
	rows := []supply.NoticeReadRecord{}
	for _, id := range ids {
		row, _ := s.GetNoticeReadState(ctx, "51", id)
		rows = append(rows, row)
	}
	return rows, int64(len(rows)), nil
}

type noticeHTTPWriter struct {
	calls int
	meta  supply.WriteMeta
}

func (w *noticeHTTPWriter) MarkRead(_ context.Context, meta supply.WriteMeta, id string) (*supply.NoticeReadReceipt, error) {
	w.calls++
	w.meta = meta
	return &supply.NoticeReadReceipt{NoticeID: id, StaffID: meta.ActorID, ReadAt: supplyHTTPNow, Created: true}, nil
}

type planHTTPStore struct{}

func (planHTTPStore) GetPlanSummary(context.Context, string) (supply.PlanReadRecord, error) {
	return supply.PlanReadRecord{Plan: &supply.ResourcePlan{ID: "61", ResourceID: "31", Region: "CN", Kind: "OFFLINE", Status: "PENDING", CreatedBy: "8", Revision: 1, ExpectedResourceRevision: 1, DueAt: supplyHTTPNow.Add(48 * time.Hour), CreatedAt: supplyHTTPNow, Reason: "调整"}, ResourceID: "31", ResourceRegion: "CN", ResourceRevision: 1, ResourceBuyerID: "7"}, nil
}
func workbenchHTTPRouter(writer *noticeHTTPWriter, identity string, missing bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	group := router.Group("/api/internal")
	group.Use(func(c *gin.Context) {
		if identity != "anonymous" {
			op := supplyHTTPStaff()
			if identity == "supplier" {
				op.PortalType = "SUPPLIER"
				op.OperatorType = "SUPPLIER"
			}
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	deps := SupplyDeps{IdemStore: newSupplyHTTPIdemStore()}
	if !missing {
		deps.Notices = supply.NewNoticeReader(noticeHTTPStore{})
		deps.NoticeWriter = writer
		deps.Plans = supply.NewPlanReader(planHTTPStore{})
	}
	RegisterSupplyRoutes(group, deps)
	return router
}
func TestSupplyWorkbenchHTTPReadsOtherPeopleWithoutImpersonation(t *testing.T) {
	router := workbenchHTTPRouter(&noticeHTTPWriter{}, "staff", false)
	for _, tc := range []struct {
		path    string
		staff   string
		canRead bool
	}{{"/notices", "7", true}, {"/notices/read-states?staff_ids=8", "8", false}, {"/notices/51?staff_id=8", "8", false}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/internal"+tc.path, nil))
		if w.Code != 200 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		data := body["data"].(map[string]any)
		if list, ok := data["list"]; ok {
			data = list.([]any)[0].(map[string]any)
		}
		if data["staff_id"] != tc.staff || data["permissions"].(map[string]any)["can_mark_read"] != tc.canRead {
			t.Fatalf("selected person replaced viewer: %+v", data)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/internal/plans/61", nil))
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	data := body["data"].(map[string]any)
	if w.Code != 200 || data["is_initiator"] != false || data["read_only"] != true {
		t.Fatalf("resource buyer became plan initiator: %s", w.Body.String())
	}
}
func TestSupplyWorkbenchHTTPMarkReadUsesStaffIdentityAndIdempotency(t *testing.T) {
	writer := &noticeHTTPWriter{}
	router := workbenchHTTPRouter(writer, "staff", false)
	for _, payload := range []string{`{"reader_id":"8"}`, `{"staff_id":"8"}`, `{"user_id":"8"}`, `{"reason":"choose actor"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/internal/notices/51/read", strings.NewReader(payload))
		req.Header.Set("Idempotency-Key", payload)
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("self-reported actor accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if writer.calls != 0 {
		t.Fatal("invalid payload reached writer")
	}
	for range 2 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/internal/notices/51/read", strings.NewReader(`{}`))
		req.Header.Set("Idempotency-Key", "notice-read-once")
		router.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"staff_id":"7"`) {
			t.Fatalf("wrong read identity or replay: %s", w.Body.String())
		}
	}
	if writer.calls != 1 || writer.meta.ActorID != "7" || writer.meta.Reason != "阅读公告" || writer.meta.RequestID == "" {
		t.Fatalf("lost auth staff or idempotency: %+v", writer)
	}
}
func TestSupplyWorkbenchHTTPRejectsInvalidSelectorsAndMissingDependencies(t *testing.T) {
	for _, tc := range []struct {
		identity, path string
		missing        bool
		status         int
	}{{"anonymous", "/notices", false, 401}, {"supplier", "/notices", false, 403}, {"staff", "/notices", true, 503}, {"staff", "/plans/61", true, 503}, {"staff", "/notices?staff_ids=8", false, 400}, {"staff", "/notices/read-states?staff_ids=7,7", false, 400}, {"staff", "/notices/read-states?staff_ids=7&all_staff=true", false, 400}, {"staff", "/notices/read-states?all_staff=maybe", false, 400}, {"staff", "/plans/61?staff_id=8", false, 400}} {
		w := httptest.NewRecorder()
		workbenchHTTPRouter(&noticeHTTPWriter{}, tc.identity, tc.missing).ServeHTTP(w, httptest.NewRequest("GET", "/api/internal"+tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s got%d want%d: %s", tc.path, w.Code, tc.status, w.Body.String())
		}
	}
}

func TestSupplyWorkbenchHTTPReadAcceptsEmptyBodyButNotPersonQuery(t *testing.T) {
	writer := &noticeHTTPWriter{}
	router := workbenchHTTPRouter(writer, "staff", false)
	bad := httptest.NewRecorder()
	badRequest := httptest.NewRequest("POST", "/api/internal/notices/51/read?staff_id=8", nil)
	badRequest.Header.Set("Idempotency-Key", "bad-read-query")
	router.ServeHTTP(bad, badRequest)
	if bad.Code != 400 || writer.calls != 0 {
		t.Fatalf("query could choose read actor: %d %s", bad.Code, bad.Body.String())
	}
	ok := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/internal/notices/51/read", nil)
	request.Header.Set("Idempotency-Key", "empty-read-body")
	router.ServeHTTP(ok, request)
	if ok.Code != 200 || writer.calls != 1 || writer.meta.ActorID != "7" {
		t.Fatalf("empty body was not a self read: %d %s", ok.Code, ok.Body.String())
	}
}
