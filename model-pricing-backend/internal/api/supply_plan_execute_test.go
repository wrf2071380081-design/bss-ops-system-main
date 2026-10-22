package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

type planExecutionHTTPExecutor struct {
	calls     int
	meta      supply.WriteMeta
	id        string
	expected  uint64
	confirmed bool
	note      string
	result    *supply.PlanExecutionResult
	err       error
}

func (e *planExecutionHTTPExecutor) Execute(_ context.Context, meta supply.WriteMeta, id string, expected uint64, confirmed bool, note string) (*supply.PlanExecutionResult, error) {
	e.calls++
	e.meta, e.id, e.expected, e.confirmed, e.note = meta, id, expected, confirmed, note
	return e.result, e.err
}

func planExecutionHTTPPlan(status string, revision uint64) *supply.ResourcePlan {
	var executedAt *time.Time
	if status == "EXECUTED" {
		at := supplyHTTPNow
		executedAt = &at
	}
	return &supply.ResourcePlan{
		ID: "81", ResourceID: "11", Region: "CN", Kind: "OFFLINE", Status: status,
		CreatedBy: "7", Revision: revision, ExpectedResourceRevision: 2,
		DueAt: supplyHTTPNow.Add(time.Hour), ExecutedAt: executedAt,
		CreatedAt: supplyHTTPNow, Reason: "维护窗口",
	}
}

func planExecutionHTTPRouter(executor *planExecutionHTTPExecutor) (*gin.Engine, *supplyHTTPIdemStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	idem := newSupplyHTTPIdemStore()
	deps := SupplyDeps{IdemStore: idem}
	if executor != nil {
		deps.PlanExecutor = executor
	}
	RegisterSupplyRoutes(r.Group("/api/internal"), deps)
	return r, idem
}

const planExecutionBody = `{"expected_revision":"1","confirmed":true,"completion_note":"已在供应商控制台完成下架，工单 OFF-81","reason":"实际完成登记"}`

func TestPlanExecutionHTTPReturnsExecutedPlanAndUsesAuthenticatedInitiator(t *testing.T) {
	executor := &planExecutionHTTPExecutor{result: &supply.PlanExecutionResult{Plan: planExecutionHTTPPlan("EXECUTED", 2)}}
	r, idem := planExecutionHTTPRouter(executor)
	rec := supplyHTTPCall(r, "POST", "/api/internal/plans/81/execute", planExecutionBody, "execute-81", "trace")
	if rec.Code != 200 {
		t.Fatalf("execute %d %s", rec.Code, rec.Body)
	}
	if executor.calls != 1 || executor.meta.ActorID != "7" || executor.meta.Reason != "实际完成登记" || executor.id != "81" || executor.expected != 1 || !executor.confirmed || executor.note != "已在供应商控制台完成下架，工单 OFF-81" {
		t.Fatalf("bad execution inputs: %+v", executor)
	}
	var envelope struct {
		Data supply.ResourcePlan `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Data.Status != "EXECUTED" || envelope.Data.Revision != 2 {
		t.Fatalf("bad execution response %s err=%v", rec.Body, err)
	}
	if row := idem.rows["execute-81"]; row == nil || row.Status != "DONE" || row.HTTPStatus != 200 {
		t.Fatalf("success response not captured for replay: %+v", row)
	}
}

func TestPlanExecutionHTTPReturnsBlockedOutcomeAs409WithPlanData(t *testing.T) {
	executor := &planExecutionHTTPExecutor{result: &supply.PlanExecutionResult{Plan: planExecutionHTTPPlan("BLOCKED", 2), Conflict: true}}
	r, idem := planExecutionHTTPRouter(executor)
	rec := supplyHTTPCall(r, "POST", "/api/internal/plans/81/execute", planExecutionBody, "blocked-81", "trace")
	if rec.Code != 409 {
		t.Fatalf("blocked %d %s", rec.Code, rec.Body)
	}
	var envelope struct {
		Code    int                 `json:"code"`
		Data    supply.ResourcePlan `json:"data"`
		Message string              `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Data.Status != "BLOCKED" || envelope.Data.Revision != 2 || envelope.Message == "" {
		t.Fatalf("blocked response must expose the committed plan revision: %s err=%v", rec.Body, err)
	}
	if row := idem.rows["blocked-81"]; row == nil || row.Status != "DONE" {
		t.Fatalf("committed BLOCKED response must be a terminal idempotency result: %+v", row)
	}
	replay := supplyHTTPCall(r, "POST", "/api/internal/plans/81/execute", planExecutionBody, "blocked-81", "trace")
	if replay.Code != 409 || executor.calls != 1 || !strings.Contains(replay.Body.String(), `"status":"BLOCKED"`) {
		t.Fatalf("blocked response replay status=%d executor calls=%d body=%s", replay.Code, executor.calls, replay.Body)
	}
}

func TestPlanExecutionHTTPRejectsInvalidInputMissingDependencyAndInconsistentResult(t *testing.T) {
	for i, body := range []string{
		`{}`,
		`{"expected_revision":1,"confirmed":true,"completion_note":"证据","reason":"完成"}`,
		`{"expected_revision":"1","confirmed":false,"completion_note":"证据","reason":"完成"}`,
		`{"expected_revision":"1","confirmed":true,"completion_note":"","reason":"完成"}`,
		`{"expected_revision":"1","confirmed":true,"completion_note":"证据","reason":"完成","actor_id":"8"}`,
	} {
		executor := &planExecutionHTTPExecutor{result: &supply.PlanExecutionResult{Plan: planExecutionHTTPPlan("EXECUTED", 2)}}
		r, _ := planExecutionHTTPRouter(executor)
		rec := supplyHTTPCall(r, "POST", "/api/internal/plans/81/execute", body, "invalid-"+string(rune('a'+i)), "trace")
		if rec.Code != 400 || executor.calls != 0 {
			t.Fatalf("invalid request %d reached executor=%d: %d %s", i, executor.calls, rec.Code, rec.Body)
		}
	}
	r, _ := planExecutionHTTPRouter(nil)
	if rec := supplyHTTPCall(r, "POST", "/api/internal/plans/81/execute", planExecutionBody, "missing-executor", "trace"); rec.Code != 503 {
		t.Fatalf("missing executor %d %s", rec.Code, rec.Body)
	}
	for _, result := range []*supply.PlanExecutionResult{
		{Plan: planExecutionHTTPPlan("BLOCKED", 2)},
		{Plan: planExecutionHTTPPlan("EXECUTED", 1)},
		{Plan: planExecutionHTTPPlan("EXECUTED", 2), Conflict: true},
		{Plan: func() *supply.ResourcePlan { p := planExecutionHTTPPlan("EXECUTED", 2); p.CreatedBy = "8"; return p }()},
	} {
		executor := &planExecutionHTTPExecutor{result: result}
		r, _ := planExecutionHTTPRouter(executor)
		if rec := supplyHTTPCall(r, "POST", "/api/internal/plans/81/execute", planExecutionBody, "inconsistent", "trace"); rec.Code != 500 {
			t.Fatalf("inconsistent result %d %s", rec.Code, rec.Body)
		}
	}
}

var _ SupplyPlanExecutor = (*planExecutionHTTPExecutor)(nil)
