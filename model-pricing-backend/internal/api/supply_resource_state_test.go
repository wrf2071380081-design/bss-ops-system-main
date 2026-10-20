package api

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"testing"
)

type stateHTTPWriter struct {
	calls int
	meta  supply.WriteMeta
	cmd   supply.ResourceStateCommand
	err   error
}

func (w *stateHTTPWriter) Change(_ context.Context, meta supply.WriteMeta, cmd supply.ResourceStateCommand) (*supply.Resource, error) {
	w.calls++
	w.meta = meta
	w.cmd = cmd
	if w.err != nil {
		return nil, w.err
	}
	return &supply.Resource{ID: cmd.ResourceID, State: "OFFLINE", Revision: 3, EverAdmitted: true}, nil
}
func stateHTTPRouter(w *stateHTTPWriter, missing bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	deps := SupplyDeps{IdemStore: newSupplyHTTPIdemStore()}
	if !missing {
		deps.ResourceStates = w
	}
	RegisterSupplyRoutes(r.Group("/api/internal"), deps)
	return r
}

const stateHTTPBody = `{"expected_revision":"2","action":"OFFLINE","external_confirmed":true,"completion_note":"平台已操作","reason":"维护"}`

func TestResourceStateHTTPAuthenticatedCommandAndReplay(t *testing.T) {
	w := &stateHTTPWriter{}
	r := stateHTTPRouter(w, false)
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/state", stateHTTPBody, "state-key", "trace")
		if rec.Code != 200 {
			t.Fatalf("failed %d %s", rec.Code, rec.Body)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		data := body["data"].(map[string]any)
		if data["revision"] != "3" || data["state"] != "OFFLINE" {
			t.Fatalf("bad result %+v", data)
		}
	}
	if w.calls != 1 || w.meta.ActorID != "7" || w.cmd.ResourceID != "11" || w.cmd.ExpectedRevision != 2 || !w.cmd.ExternalConfirmed {
		t.Fatalf("bad command %+v", w)
	}
}
func TestResourceStateHTTPRejectsUnsafeInputBeforeWriter(t *testing.T) {
	w := &stateHTTPWriter{}
	r := stateHTTPRouter(w, false)
	for i, body := range []string{`{}`, `{"expected_revision":2,"action":"OFFLINE"}`, `{"expected_revision":"2","action":"ADMIT","external_confirmed":true,"completion_note":"操作","reason":"维护"}`, `{"expected_revision":"2","action":"OFFLINE","external_confirmed":false,"completion_note":"操作","reason":"维护"}`, `{"expected_revision":"2","action":"OFFLINE","external_confirmed":true,"completion_note":"操作","reason":"维护","actor_id":"8"}`, `{"expected_revision":"2","expected_revision":"3"}`, `null`} {
		rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/state", body, "invalid-"+string(rune('a'+i)), "trace")
		if rec.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/state?plan_id=81", stateHTTPBody, "query-key", "trace")
	if rec.Code != 400 || w.calls != 0 {
		t.Fatalf("unsafe delegation %d %+v", rec.Code, w)
	}
}
func TestResourceStateHTTPMissingDependenciesAndConflict(t *testing.T) {
	rec := supplyHTTPCall(stateHTTPRouter(nil, true), "POST", "/api/internal/resources/11/state", stateHTTPBody, "missing-key", "trace")
	if rec.Code != 503 {
		t.Fatalf("missing dependency %d", rec.Code)
	}
	w := &stateHTTPWriter{err: supply.ErrRevisionConflict}
	rec = supplyHTTPCall(stateHTTPRouter(w, false), "POST", "/api/internal/resources/11/state", stateHTTPBody, "conflict-key", "trace")
	if rec.Code != 409 {
		t.Fatalf("conflict %d", rec.Code)
	}
	rec = supplyHTTPCall(stateHTTPRouter(w, false), "POST", "/api/internal/resources/11/state", stateHTTPBody, "", "trace")
	if rec.Code != 400 {
		t.Fatalf("missing idem %d", rec.Code)
	}
}
