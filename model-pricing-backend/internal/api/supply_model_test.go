package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

var supplyHTTPNow = time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)

func supplyHTTPModel() *supply.StandardModel {
	m, _ := supply.NewStandardModel("9007199254740993", "7", supply.ModelData{Name: "待补齐模型"}, supplyHTTPNow)
	return m
}

type supplyHTTPReadStore struct{ model *supply.StandardModel }

func (s supplyHTTPReadStore) ListModels(context.Context, supply.ModelListQuery) ([]*supply.StandardModel, int64, error) {
	return []*supply.StandardModel{s.model}, 1, nil
}
func (s supplyHTTPReadStore) GetModel(_ context.Context, id string) (*supply.StandardModel, error) {
	if id != s.model.ID {
		return nil, supply.ErrNotFound
	}
	return s.model, nil
}
func (s supplyHTTPReadStore) ListModelVersions(context.Context, string, int, int) ([]*supply.StandardModel, int64, error) {
	return []*supply.StandardModel{s.model}, 1, nil
}
func (s supplyHTTPReadStore) GetModelVersion(context.Context, string, uint64) (*supply.StandardModel, error) {
	return s.model, nil
}

type supplyHTTPWriter struct {
	calls    int
	meta     supply.WriteMeta
	data     supply.ModelData
	revision uint64
	date     string
	err      error
}

func (w *supplyHTTPWriter) Create(_ context.Context, meta supply.WriteMeta, data supply.ModelData) (*supply.StandardModel, error) {
	w.calls++
	w.meta = meta
	w.data = data
	if w.err != nil {
		return nil, w.err
	}
	return supply.NewStandardModel("9007199254740993", meta.ActorID, data, supplyHTTPNow)
}
func (w *supplyHTTPWriter) Update(_ context.Context, meta supply.WriteMeta, _ string, revision uint64, data supply.ModelData) (*supply.StandardModel, error) {
	w.calls++
	w.meta = meta
	w.data = data
	w.revision = revision
	if w.err != nil {
		return nil, w.err
	}
	m := supplyHTTPModel()
	m.Revision = revision + 1
	m.VersionNo = 2
	return m, nil
}
func (w *supplyHTTPWriter) Verify(_ context.Context, meta supply.WriteMeta, _ string, revision uint64, date string) (*supply.StandardModel, error) {
	w.calls++
	w.meta = meta
	w.revision = revision
	w.date = date
	if w.err != nil {
		return nil, w.err
	}
	return supplyHTTPModel(), nil
}

// Persistence-only double for the real shared idempotency middleware.
type supplyHTTPIdemStore struct {
	rows map[string]*middleware.IdempotencyRow
}

func newSupplyHTTPIdemStore() *supplyHTTPIdemStore {
	return &supplyHTTPIdemStore{rows: map[string]*middleware.IdempotencyRow{}}
}
func (s *supplyHTTPIdemStore) DB() *gorm.DB { return nil }
func (s *supplyHTTPIdemStore) GetByRequestID(_ context.Context, key string) (*middleware.IdempotencyRow, error) {
	return s.rows[key], nil
}
func (s *supplyHTTPIdemStore) FindByBizKey(_ context.Context, key string) (*middleware.IdempotencyRow, error) {
	for _, row := range s.rows {
		if row.BizKey == key {
			return row, nil
		}
	}
	return nil, nil
}
func (s *supplyHTTPIdemStore) CreateProcessing(_ context.Context, key, hash, biz string, deadline time.Time) error {
	s.rows[key] = &middleware.IdempotencyRow{ID: int64(len(s.rows) + 1), RequestID: key, RequestHash: hash, BizKey: biz, Status: "PROCESSING", Deadline: &deadline}
	return nil
}
func (s *supplyHTTPIdemStore) ResetProcessing(_ context.Context, id int64, hash string, deadline time.Time) error {
	for _, row := range s.rows {
		if row.ID == id {
			row.RequestHash = hash
			row.Deadline = &deadline
			row.Status = "PROCESSING"
			return nil
		}
	}
	return errors.New("missing idempotency row")
}
func (s *supplyHTTPIdemStore) MarkResult(c *gin.Context, key string, status int) error {
	row := s.rows[key]
	row.HTTPStatus = status
	if status >= 200 && status < 300 {
		row.Status = "DONE"
		row.ResultJSON, _ = json.Marshal(middleware.GetIdempotencyResult(c))
	} else {
		row.Status = "FAILED"
	}
	return nil
}
func (s *supplyHTTPIdemStore) CleanExpired(time.Time) (int64, error) { return 0, nil }

func supplyHTTPRouter(op *middleware.Operator, writer *supplyHTTPWriter, idem middleware.IdempotencyStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{Reader: supply.NewModelReader(supplyHTTPReadStore{supplyHTTPModel()}), Writer: writer, IdemStore: idem})
	return r
}
func supplyHTTPStaff() *middleware.Operator {
	return &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: 7, OperatorID: 888, AccountID: 999}
}
func supplyHTTPCall(r *gin.Engine, method, path, body, key, trace string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set("X-Request-Id", trace)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
func supplyHTTPEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid envelope: %s", rec.Body.String())
	}
	return env
}

func TestSupplyHTTPReadsRequireInternalIdentityButNoLegacyRole(t *testing.T) {
	for _, tc := range []struct {
		name   string
		op     *middleware.Operator
		status int
	}{{"anonymous", nil, 401}, {"supplier", &middleware.Operator{PortalType: "SUPPLIER", OperatorType: "SUPPLIER", StaffID: 7}, 403}, {"internal without permissions", supplyHTTPStaff(), 200}} {
		t.Run(tc.name, func(t *testing.T) {
			r := supplyHTTPRouter(tc.op, &supplyHTTPWriter{}, newSupplyHTTPIdemStore())
			rec := supplyHTTPCall(r, "GET", "/api/internal/standard-models", "", "", "trace-read")
			if rec.Code != tc.status {
				t.Fatalf("want %d got %d: %s", tc.status, rec.Code, rec.Body.String())
			}
			if tc.status == 200 {
				env := supplyHTTPEnvelope(t, rec)
				page := env["data"].(map[string]any)
				row := page["list"].([]any)[0].(map[string]any)
				if row["id"] != "9007199254740993" || row["name"] != "待补齐模型" || row["revision"] != "1" {
					t.Fatalf("DTO lost identity/flat fields: %+v", row)
				}
				if _, nested := row["data"]; nested {
					t.Fatal("internal aggregate wrapper leaked into HTTP DTO")
				}
			}
		})
	}
}

func TestSupplyHTTPStrictCreateInputAndAuthenticatedStaffBinding(t *testing.T) {
	writer := &supplyHTTPWriter{}
	r := supplyHTTPRouter(supplyHTTPStaff(), writer, newSupplyHTTPIdemStore())
	body := `{"name":"草稿模型","reason":"手工登记"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/standard-models", body, "create-1", "trace-create")
	if rec.Code != 200 || writer.meta.ActorID != "7" || writer.meta.RequestID != "trace-create" || writer.data.Name != "草稿模型" {
		t.Fatalf("create did not use staff identity: %d %+v %s", rec.Code, writer.meta, rec.Body.String())
	}
	dto := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
	if dto["maintainer_id"] != "7" || dto["verify_status"] != "INCOMPLETE" {
		t.Fatalf("bad draft DTO: %+v", dto)
	}
	invalid := []string{`{"name":"draft","maintainer_id":"8","reason":"r"}`, `{"name":"draft","reason":"r","extra":true}`, `{"name":"first","name":"second","reason":"r"}`, `{"Name":"draft","reason":"r"}`, `{"name":"draft","reason":"r"} {}`, `null`, `{"name":"draft","reason":"r","price_items":[{"amount":0}]}`, `{"name":"draft","reason":"r","capacity":[{"key":"x","key":"y"}]}`}
	invalid = append(invalid, "{\"name\":\"\xff\",\"reason\":\"r\"}")
	for i, body := range invalid {
		rec = supplyHTTPCall(r, "POST", "/api/internal/standard-models", body, fmt.Sprintf("invalid-%d", i), "trace-invalid")
		if rec.Code != 400 {
			t.Fatalf("invalid body accepted (%d): %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if writer.calls != 1 {
		t.Fatalf("invalid requests reached write service: %d", writer.calls)
	}
}

func TestSupplyHTTPPutRequiresFullReplacementAndStringRevision(t *testing.T) {
	writer := &supplyHTTPWriter{}
	r := supplyHTTPRouter(supplyHTTPStaff(), writer, newSupplyHTTPIdemStore())
	payload := struct {
		supply.ModelData
		ExpectedRevision uint64 `json:"expected_revision,string"`
		Reason           string `json:"reason"`
	}{supplyHTTPModel().Data, 1, "完整替换"}
	raw, _ := json.Marshal(payload)
	rec := supplyHTTPCall(r, "PUT", "/api/internal/standard-models/9007199254740993", string(raw), "update-1", "trace-update")
	if rec.Code != 200 || writer.revision != 1 {
		t.Fatalf("valid replacement rejected: %d %s", rec.Code, rec.Body.String())
	}
	for i, body := range []string{`{"name":"partial","expected_revision":"1","reason":"r"}`, strings.Replace(string(raw), `"expected_revision":"1"`, `"expected_revision":1`, 1), strings.Replace(string(raw), `"expected_revision":"1"`, `"expected_revision":"0"`, 1), strings.Replace(string(raw), `"expected_revision":"1"`, `"expected_revision":"18446744073709551615"`, 1)} {
		rec = supplyHTTPCall(r, "PUT", "/api/internal/standard-models/9007199254740993", body, fmt.Sprintf("bad-put-%d", i), "trace-update")
		if rec.Code != 400 {
			t.Fatalf("invalid replacement accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	if writer.calls != 1 {
		t.Fatal("invalid replacement reached domain service")
	}
}

func TestSupplyHTTPVerifyForwardsDateAndRejectsInvalidRevision(t *testing.T) {
	writer := &supplyHTTPWriter{}
	r := supplyHTTPRouter(supplyHTTPStaff(), writer, newSupplyHTTPIdemStore())
	good := `{"expected_revision":"1","checked_at":"2026-10-20","reason":"人工核验"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/standard-models/9007199254740993/verify", good, "verify-1", "trace-verify")
	if rec.Code != 200 || writer.date != "2026-10-20" || writer.meta.ActorID != "7" || writer.revision != 1 {
		t.Fatalf("valid verify not forwarded: %d %s", rec.Code, rec.Body.String())
	}
	for i, body := range []string{`{"expected_revision":1,"checked_at":"2026-10-20","reason":"r"}`, `{"expected_revision":"18446744073709551615","checked_at":"2026-10-20","reason":"r"}`, `{"expected_revision":"1","reason":"r"}`} {
		rec = supplyHTTPCall(r, "POST", "/api/internal/standard-models/9007199254740993/verify", body, fmt.Sprintf("bad-verify-%d", i), "trace-verify")
		if rec.Code != 400 {
			t.Fatalf("invalid verification accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	if writer.calls != 1 {
		t.Fatal("invalid verify reached service")
	}
}

func TestSupplyHTTPErrorMappingAndNoInternalSecretEcho(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{supply.ErrForbidden, 403}, {supply.ErrNotFound, 404}, {supply.ErrRevisionConflict, 409}, {supply.ErrIdentityConflict, 409}, {supply.ErrIncomplete, 400}, {supply.ErrSourceRejected, 409}, {supply.ErrDependencyUnavailable, 503}, {errors.New("database password=private-fixture"), 500}} {
		writer := &supplyHTTPWriter{err: tc.err}
		r := supplyHTTPRouter(supplyHTTPStaff(), writer, newSupplyHTTPIdemStore())
		rec := supplyHTTPCall(r, "POST", "/api/internal/standard-models", `{"name":"draft","reason":"r"}`, "error-1", "trace-error")
		if rec.Code != tc.status || supplyHTTPEnvelope(t, rec)["data"] != nil || strings.Contains(rec.Body.String(), "private-fixture") {
			t.Fatalf("bad error mapping: want%d got%d %s", tc.status, rec.Code, rec.Body.String())
		}
	}
}

func TestSupplyHTTPSharedIdempotencyReplayAndMissingDependency(t *testing.T) {
	writer := &supplyHTTPWriter{}
	r := supplyHTTPRouter(supplyHTTPStaff(), writer, newSupplyHTTPIdemStore())
	body := `{"name":"draft","reason":"r"}`
	missing := supplyHTTPCall(r, "POST", "/api/internal/standard-models", body, "", "trace-a")
	if missing.Code != 400 || writer.calls != 0 {
		t.Fatal("write without idempotency was accepted")
	}
	first := supplyHTTPCall(r, "POST", "/api/internal/standard-models", body, "replay-1", "trace-a")
	second := supplyHTTPCall(r, "POST", "/api/internal/standard-models", body, "replay-1", "trace-b")
	if first.Code != 200 || second.Code != 200 || writer.calls != 1 || supplyHTTPEnvelope(t, second)["requestId"] != "trace-b" {
		t.Fatalf("replay repeated write or wrong trace: %d %d calls%d", first.Code, second.Code, writer.calls)
	}
	different := supplyHTTPCall(r, "POST", "/api/internal/standard-models", `{"name":"changed","reason":"r"}`, "replay-1", "trace-c")
	if different.Code != 400 || writer.calls != 1 {
		t.Fatal("different body with same key was accepted")
	}
	unavailable := supplyHTTPRouter(supplyHTTPStaff(), writer, nil)
	rec := supplyHTTPCall(unavailable, "POST", "/api/internal/standard-models", body, "no-store", "trace-d")
	if rec.Code != 503 || writer.calls != 1 {
		t.Fatal("missing idempotency store opened a write path")
	}
}

func TestSupplyHTTPHistoricalRecordsAreReadOnlyAndBadQueriesReject(t *testing.T) {
	r := supplyHTTPRouter(supplyHTTPStaff(), &supplyHTTPWriter{}, newSupplyHTTPIdemStore())
	rec := supplyHTTPCall(r, "GET", "/api/internal/standard-models/9007199254740993/versions", "", "", "trace-history")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	row := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)["list"].([]any)[0].(map[string]any)
	permissions := row["permissions"].(map[string]any)
	if permissions["can_edit"] != false || permissions["can_verify"] != false {
		t.Fatal("historical snapshot has write actions")
	}
	for _, query := range []string{"size=101", "page=0", "sort_by=unknown", "region=CN", "page=1&page=2"} {
		rec = supplyHTTPCall(r, "GET", "/api/internal/standard-models?"+query, "", "", "trace-query")
		if rec.Code != 400 {
			t.Fatalf("bad query %s accepted: %d", query, rec.Code)
		}
	}
}
