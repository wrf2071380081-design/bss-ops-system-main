package api

import (
	"context"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type resourceAdmitterHTTPFake struct {
	calls int
	meta  supply.WriteMeta
	cmd   supply.ResourceAdmissionCommand
	err   error
}

func (f *resourceAdmitterHTTPFake) Admit(_ context.Context, meta supply.WriteMeta, cmd supply.ResourceAdmissionCommand) (*supply.Resource, error) {
	f.calls++
	f.meta, f.cmd = meta, cmd
	if f.err != nil {
		return nil, f.err
	}
	return &supply.Resource{ID: cmd.ResourceID, State: "ADMITTED", EverAdmitted: true, Revision: cmd.ExpectedRevision + 1, UpdatedAt: time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)}, nil
}

type resourceConfigBinderHTTPFake struct {
	calls int
	meta  supply.WriteMeta
	cmd   supply.ResourceConfigBindingCommand
	noOp  bool
	err   error
}

func (f *resourceConfigBinderHTTPFake) BindFormalConfig(_ context.Context, meta supply.WriteMeta, cmd supply.ResourceConfigBindingCommand) (*supply.ResourceConfigBindingChange, error) {
	f.calls++
	f.meta, f.cmd = meta, cmd
	if f.err != nil {
		return nil, f.err
	}
	revision := cmd.ExpectedRevision
	if !f.noOp {
		revision++
	}
	return &supply.ResourceConfigBindingChange{Resource: &supply.Resource{ID: cmd.ResourceID, State: "PENDING_ACCESS", FormalConfigID: cmd.ConfigID, FormalConfigRevision: cmd.ExpectedConfigRevision, Revision: revision, UpdatedAt: time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)}, Changed: !f.noOp}, nil
}

func resourceOperationsHTTPRouter(op *middleware.Operator, admitter SupplyResourceAdmitter, binder SupplyResourceConfigBinder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), ResourceAdmitter: admitter, ResourceConfigBinder: binder})
	return r
}

func TestResourceAdmissionHTTPUsesActorStrictRequestAndIdempotency(t *testing.T) {
	fake := &resourceAdmitterHTTPFake{}
	r := resourceOperationsHTTPRouter(supplyHTTPStaff(), fake, nil)
	body := `{"expected_revision":"2","external_confirmed":true,"completion_note":"外部配置已完成","reason":"完成首次接入"}`
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/resources/9007199254740993/admit", body, "admit-once", "trace-admit")
		if rec.Code != 200 {
			t.Fatalf("admit %d: %s", rec.Code, rec.Body.String())
		}
		data := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
		if data["resource_id"] != "9007199254740993" || data["state"] != "ADMITTED" || data["ever_admitted"] != true || data["revision"] != "3" {
			t.Fatalf("unsafe or incorrect admission receipt: %+v", data)
		}
	}
	if fake.calls != 1 || fake.meta.ActorID != "7" || fake.meta.RequestID != "trace-admit" || fake.cmd.ResourceID != "9007199254740993" || fake.cmd.ExpectedRevision != 2 || !fake.cmd.ExternalConfirmed || fake.cmd.CompletionNote != "外部配置已完成" || fake.meta.Reason != "完成首次接入" {
		t.Fatalf("wrong service input: %+v", fake)
	}
}

func TestResourceAdmissionHTTPRejectsSelfReportedAuthorityAndMissingDependency(t *testing.T) {
	fake := &resourceAdmitterHTTPFake{}
	r := resourceOperationsHTTPRouter(supplyHTTPStaff(), fake, nil)
	for i, body := range []string{
		`{"expected_revision":2,"external_confirmed":true,"completion_note":"已完成","reason":"说明"}`,
		`{"expected_revision":"2","external_confirmed":false,"completion_note":"已完成","reason":"说明"}`,
		`{"expected_revision":"2","external_confirmed":true,"completion_note":"已完成","reason":"说明","assignee_id":"7"}`,
		`{"expected_revision":"2","external_confirmed":true,"completion_note":"","reason":"说明"}`,
	} {
		rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/admit", body, "bad-admit-"+string(rune('a'+i)), "trace-admit")
		if rec.Code != 400 {
			t.Fatalf("unsafe admission body %d: status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	if fake.calls != 0 {
		t.Fatalf("invalid admission reached service %d times", fake.calls)
	}
	rec := supplyHTTPCall(resourceOperationsHTTPRouter(supplyHTTPStaff(), nil, nil), "POST", "/api/internal/resources/11/admit", `{"expected_revision":"2","external_confirmed":true,"completion_note":"已完成","reason":"说明"}`, "missing-admit", "trace-admit")
	if rec.Code != 503 {
		t.Fatalf("missing admission dependency: %d %s", rec.Code, rec.Body.String())
	}
}

func TestResourceAdmissionHTTPRequiresInternalStaffAndMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   *middleware.Operator
		err  error
		want int
	}{{"anonymous", nil, nil, 401}, {"supplier", &middleware.Operator{PortalType: "SUPPLIER", OperatorType: "SUPPLIER", StaffID: 7}, nil, 403}, {"wrong assignee", supplyHTTPStaff(), supply.ErrForbidden, 403}, {"stale proof", supplyHTTPStaff(), supply.ErrRevisionConflict, 409}} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &resourceAdmitterHTTPFake{err: tc.err}
			r := resourceOperationsHTTPRouter(tc.op, fake, nil)
			rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/admit", `{"expected_revision":"2","external_confirmed":true,"completion_note":"已完成","reason":"说明"}`, "admit-"+tc.name, "trace-admit")
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.op == nil || tc.op.PortalType != "INTERNAL" {
				if fake.calls != 0 {
					t.Fatalf("unauthorized call reached service: %d", fake.calls)
				}
			}
		})
	}
}

func TestResourceConfigBindingHTTPUsesStrictStringIDsAndReturnsNoOpRevision(t *testing.T) {
	fake := &resourceConfigBinderHTTPFake{noOp: true}
	r := resourceOperationsHTTPRouter(supplyHTTPStaff(), nil, fake)
	body := `{"expected_revision":"2","formal_config_id":"9007199254740995","expected_config_revision":"5","reason":"核对正式配置"}`
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "PUT", "/api/internal/resources/9007199254740993/formal-config", body, "bind-once", "trace-bind")
		if rec.Code != 200 {
			t.Fatalf("bind %d: %s", rec.Code, rec.Body.String())
		}
		data := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
		if data["resource_id"] != "9007199254740993" || data["formal_config_id"] != "9007199254740995" || data["formal_config_revision"] != "5" || data["resource_revision"] != "2" || data["changed"] != false {
			t.Fatalf("unsafe or incorrect binding receipt: %+v", data)
		}
	}
	if fake.calls != 1 || fake.meta.ActorID != "7" || fake.meta.RequestID != "trace-bind" || fake.cmd.ResourceID != "9007199254740993" || fake.cmd.ConfigID != "9007199254740995" || fake.cmd.ExpectedRevision != 2 || fake.cmd.ExpectedConfigRevision != 5 || fake.meta.Reason != "核对正式配置" {
		t.Fatalf("wrong binder input: %+v", fake)
	}
}

func TestResourceConfigBindingHTTPRejectsUnknownFieldsMissingDependencyAndMapsConflict(t *testing.T) {
	r := resourceOperationsHTTPRouter(supplyHTTPStaff(), nil, &resourceConfigBinderHTTPFake{})
	for i, body := range []string{
		`{"expected_revision":2,"formal_config_id":"11","expected_config_revision":"5","reason":"说明"}`,
		`{"expected_revision":"2","formal_config_id":"11","expected_config_revision":"5","reason":"说明","api_key":"secret"}`,
		`{"expected_revision":"2","formal_config_id":"11","expected_config_revision":"5","reason":" "}`,
	} {
		rec := supplyHTTPCall(r, "PUT", "/api/internal/resources/11/formal-config", body, "bad-bind-"+string(rune('a'+i)), "trace-bind")
		if rec.Code != 400 {
			t.Fatalf("unsafe binding body %d: status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	rec := supplyHTTPCall(resourceOperationsHTTPRouter(supplyHTTPStaff(), nil, nil), "PUT", "/api/internal/resources/11/formal-config", `{"expected_revision":"2","formal_config_id":"12","expected_config_revision":"5","reason":"绑定"}`, "missing-bind", "trace-bind")
	if rec.Code != 503 {
		t.Fatalf("missing binding dependency: %d %s", rec.Code, rec.Body.String())
	}
	fake := &resourceConfigBinderHTTPFake{err: supply.ErrRevisionConflict}
	rec = supplyHTTPCall(resourceOperationsHTTPRouter(supplyHTTPStaff(), nil, fake), "PUT", "/api/internal/resources/11/formal-config", `{"expected_revision":"2","formal_config_id":"12","expected_config_revision":"5","reason":"绑定"}`, "conflict-bind", "trace-bind")
	if rec.Code != 409 {
		t.Fatalf("binding conflict: %d %s", rec.Code, rec.Body.String())
	}
}
