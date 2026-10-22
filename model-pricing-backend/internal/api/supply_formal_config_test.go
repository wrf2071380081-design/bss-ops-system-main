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

type formalConfigWriterHTTPFake struct {
	createCalls  int
	updateCalls  int
	disableCalls int
	meta         supply.WriteMeta
	supplierID   string
	configID     string
	expected     uint64
	createInput  supply.FormalConfigInput
	updateInput  supply.FormalConfigInput
	result       *supply.FormalConfigView
	err          error
}

type formalConfigReaderHTTPFake struct {
	views      []*supply.FormalConfigView
	supplierID string
	err        error
}

func (f *formalConfigReaderHTTPFake) List(_ context.Context, supplierID string) ([]*supply.FormalConfigView, error) {
	f.supplierID = supplierID
	return f.views, f.err
}

func (f *formalConfigWriterHTTPFake) Create(_ context.Context, meta supply.WriteMeta, supplierID string, input supply.FormalConfigInput) (*supply.FormalConfigView, error) {
	f.createCalls++
	f.meta, f.supplierID, f.createInput = meta, supplierID, input
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}
func (f *formalConfigWriterHTTPFake) Update(_ context.Context, meta supply.WriteMeta, supplierID, configID string, expected uint64, input supply.FormalConfigInput) (*supply.FormalConfigView, error) {
	f.updateCalls++
	f.meta, f.supplierID, f.configID, f.expected, f.updateInput = meta, supplierID, configID, expected, input
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}
func (f *formalConfigWriterHTTPFake) Disable(_ context.Context, meta supply.WriteMeta, supplierID, configID string, expected uint64) (*supply.FormalConfigView, error) {
	f.disableCalls++
	f.meta, f.supplierID, f.configID, f.expected = meta, supplierID, configID, expected
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func formalConfigHTTPRouter(op *middleware.Operator, writer SupplyFormalConfigWriter, readers ...SupplyFormalConfigReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	var reader SupplyFormalConfigReader
	if len(readers) > 0 {
		reader = readers[0]
	}
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), FormalConfigs: writer, FormalConfigReader: reader})
	return r
}

func formalConfigHTTPView(id, supplierID string, revision uint64, status string) *supply.FormalConfigView {
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	confirmed := revision
	if status == "INACTIVE" && revision > 1 {
		confirmed--
	}
	return &supply.FormalConfigView{ID: id, SupplierID: supplierID, Region: "CN", Scope: "生产接口", BaseURL: "https://api.vendor.example/v1", HasAPIKey: true, APIKeyMask: "********", Status: status, Revision: revision, CreatedBy: "7", UpdatedBy: "7", CreatedAt: now, UpdatedAt: now, ConfirmedBy: "7", ConfirmedAt: now, ConfirmedRevision: confirmed}
}

func TestFormalConfigListHTTPReturnsOnlySafeViews(t *testing.T) {
	reader := &formalConfigReaderHTTPFake{views: []*supply.FormalConfigView{
		formalConfigHTTPView("91", "21", 2, "ACTIVE"),
		formalConfigHTTPView("92", "21", 3, "INACTIVE"),
	}}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), nil, reader)
	rec := supplyHTTPCall(r, "GET", "/api/internal/suppliers/21/formal-configs", "", "", "trace-formal-list")
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	data := supplyHTTPEnvelope(t, rec)["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("wrong list length: %+v", data)
	}
	first, second := data[0].(map[string]any), data[1].(map[string]any)
	if first["id"] != "91" || first["supplier_id"] != "21" || first["api_key_mask"] != "********" || first["status"] != "ACTIVE" || second["status"] != "INACTIVE" || reader.supplierID != "21" {
		t.Fatalf("incorrect or cross-supplier projection: %+v", data)
	}
	for _, row := range data {
		if _, ok := row.(map[string]any)["api_key"]; ok {
			t.Fatalf("plaintext credential returned: %+v", row)
		}
	}
}

func TestFormalConfigListHTTPRejectsQueryAndMapsDependencyOrNotFound(t *testing.T) {
	reader := &formalConfigReaderHTTPFake{views: []*supply.FormalConfigView{}}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), nil, reader)
	for i, path := range []string{"/api/internal/suppliers/21/formal-configs?page=1", "/api/internal/suppliers/021/formal-configs"} {
		rec := supplyHTTPCall(r, "GET", path, "", "", "trace-bad-config-list-"+string(rune('a'+i)))
		if rec.Code != 400 {
			t.Fatalf("bad list query/path %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := supplyHTTPCall(formalConfigHTTPRouter(supplyHTTPStaff(), nil), "GET", "/api/internal/suppliers/21/formal-configs", "", "", "missing-config-list")
	if rec.Code != 503 {
		t.Fatalf("missing reader dependency: %d %s", rec.Code, rec.Body.String())
	}
	reader.err = supply.ErrNotFound
	rec = supplyHTTPCall(formalConfigHTTPRouter(supplyHTTPStaff(), nil, reader), "GET", "/api/internal/suppliers/999/formal-configs", "", "", "unknown-supplier-config-list")
	if rec.Code != 404 {
		t.Fatalf("unknown supplier: %d %s", rec.Code, rec.Body.String())
	}
}

func TestFormalConfigCreateHTTPUsesAuthenticatedActorMasksCredentialAndReplays(t *testing.T) {
	fake := &formalConfigWriterHTTPFake{result: formalConfigHTTPView("9007199254740995", "21", 1, "ACTIVE")}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), fake)
	body := `{"scope":"生产接口","base_url":"https://api.vendor.example/v1","api_key":"test-secret-value","external_confirmed":true,"reason":"外部平台配置已完成"}`
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/suppliers/21/formal-configs", body, "formal-create-once", "trace-formal-create")
		if rec.Code != 200 {
			t.Fatalf("create %d: %d %s", i, rec.Code, rec.Body.String())
		}
		data := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
		if data["id"] != "9007199254740995" || data["supplier_id"] != "21" || data["revision"] != "1" || data["api_key_mask"] != "********" {
			t.Fatalf("unsafe or incorrect config receipt: %+v", data)
		}
		if _, ok := data["api_key"]; ok {
			t.Fatalf("plaintext key present in receipt: %+v", data)
		}
	}
	if fake.createCalls != 1 || fake.meta.ActorID != "7" || fake.meta.RequestID != "trace-formal-create" || fake.meta.Reason != "外部平台配置已完成" || fake.supplierID != "21" || fake.createInput.APIKey == nil || *fake.createInput.APIKey != "test-secret-value" || !fake.createInput.ExternalConfirmed {
		t.Fatalf("wrong create service input: %+v", fake)
	}
}

func TestFormalConfigUpdateUsesBigintRevisionAndPreservesOmittedKey(t *testing.T) {
	fake := &formalConfigWriterHTTPFake{result: formalConfigHTTPView("9007199254740995", "21", 4, "ACTIVE")}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), fake)
	body := `{"expected_revision":"3","scope":"生产接口","base_url":"https://api.vendor.example/v2","external_confirmed":true,"reason":"更新正式接口地址"}`
	rec := supplyHTTPCall(r, "PUT", "/api/internal/suppliers/21/formal-configs/9007199254740995", body, "formal-update", "trace-formal-update")
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	data := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
	if data["revision"] != "4" || data["has_api_key"] != true || data["api_key_mask"] != "********" {
		t.Fatalf("bad update view: %+v", data)
	}
	if fake.updateCalls != 1 || fake.meta.ActorID != "7" || fake.supplierID != "21" || fake.configID != "9007199254740995" || fake.expected != 3 || fake.updateInput.APIKey != nil || !fake.updateInput.ExternalConfirmed {
		t.Fatalf("omitted key was not preserved by service contract: %+v", fake)
	}
}

func TestFormalConfigUpdateAllowsNoopAtMaximumSupportedRevision(t *testing.T) {
	const maxRevision = "9223372036854775807"
	fake := &formalConfigWriterHTTPFake{result: formalConfigHTTPView("91", "21", 9223372036854775807, "ACTIVE")}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), fake)
	body := `{"expected_revision":"9223372036854775807","scope":"生产接口","base_url":"https://api.vendor.example/v1","external_confirmed":true,"reason":"核对配置"}`
	rec := supplyHTTPCall(r, "PUT", "/api/internal/suppliers/21/formal-configs/91", body, "formal-max-noop", "trace-formal-max")
	if rec.Code != 200 {
		t.Fatalf("max revision no-op: %d %s", rec.Code, rec.Body.String())
	}
	data := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
	if data["revision"] != maxRevision || fake.expected != 9223372036854775807 {
		t.Fatalf("max revision was not preserved: %+v fake=%+v", data, fake)
	}
}

func TestFormalConfigDisableReturnsLogicalInactiveSnapshot(t *testing.T) {
	fake := &formalConfigWriterHTTPFake{result: formalConfigHTTPView("91", "21", 5, "INACTIVE")}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), fake)
	rec := supplyHTTPCall(r, "DELETE", "/api/internal/suppliers/21/formal-configs/91", `{"expected_revision":"4","reason":"配置已废弃"}`, "formal-disable", "trace-formal-disable")
	if rec.Code != 200 {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	data := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
	if data["status"] != "INACTIVE" || data["revision"] != "5" || data["supplier_id"] != "21" {
		t.Fatalf("bad logical-disable view: %+v", data)
	}
	if fake.disableCalls != 1 || fake.expected != 4 || fake.meta.ActorID != "7" || fake.meta.Reason != "配置已废弃" {
		t.Fatalf("bad disable invocation: %+v", fake)
	}
}

func TestFormalConfigHTTPRejectsUnknownOrNumericFieldsAndMapsInactiveConflict(t *testing.T) {
	fake := &formalConfigWriterHTTPFake{result: formalConfigHTTPView("91", "21", 2, "ACTIVE")}
	r := formalConfigHTTPRouter(supplyHTTPStaff(), fake)
	for i, body := range []string{
		`{"scope":"生产接口","base_url":"https://api.vendor.example/v1","api_key":"secret","external_confirmed":true,"reason":"说明","buyer_id":"7"}`,
		`{"scope":"生产接口","base_url":"https://api.vendor.example/v1","api_key":"secret","external_confirmed":true,"reason":"说明","region":"CN"}`,
	} {
		rec := supplyHTTPCall(r, "POST", "/api/internal/suppliers/21/formal-configs", body, "formal-bad-"+string(rune('a'+i)), "trace-formal-bad")
		if rec.Code != 400 {
			t.Fatalf("unknown create field %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	for i, body := range []string{
		`{"expected_revision":3,"scope":"生产接口","base_url":"https://api.vendor.example/v1","external_confirmed":true,"reason":"说明"}`,
		`{"expected_revision":"3","scope":"生产接口","base_url":"https://api.vendor.example/v1","external_confirmed":true,"reason":"说明","buyer_id":"7"}`,
	} {
		rec := supplyHTTPCall(r, "PUT", "/api/internal/suppliers/21/formal-configs/91", body, "formal-bad-update-"+string(rune('a'+i)), "trace-formal-bad")
		if rec.Code != 400 {
			t.Fatalf("bad update body %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	fake.err = supply.ErrConfigInUse
	rec := supplyHTTPCall(r, "DELETE", "/api/internal/suppliers/21/formal-configs/91", `{"expected_revision":"3","reason":"资源仍在使用"}`, "formal-in-use", "trace-formal-disable")
	if rec.Code != 409 {
		t.Fatalf("referenced config disable must conflict: %d %s", rec.Code, rec.Body.String())
	}
	if fake.createCalls != 0 || fake.updateCalls != 0 || fake.disableCalls != 1 || fake.expected != 3 {
		t.Fatalf("invalid requests reached writer or disable input wrong: %+v", fake)
	}
}

func TestFormalConfigHTTPRequiresInternalStaffAndMapsRevisionConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   *middleware.Operator
		err  error
		want int
	}{
		{name: "anonymous", want: 401},
		{name: "supplier", op: &middleware.Operator{PortalType: "SUPPLIER", OperatorType: "SUPPLIER", StaffID: 7}, want: 403},
		{name: "assignment denied", op: supplyHTTPStaff(), err: supply.ErrForbidden, want: 403},
		{name: "revision conflict", op: supplyHTTPStaff(), err: supply.ErrRevisionConflict, want: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &formalConfigWriterHTTPFake{result: formalConfigHTTPView("91", "21", 4, "ACTIVE"), err: tc.err}
			r := formalConfigHTTPRouter(tc.op, fake)
			rec := supplyHTTPCall(r, "PUT", "/api/internal/suppliers/21/formal-configs/91", `{"expected_revision":"3","scope":"生产接口","base_url":"https://api.vendor.example/v1","external_confirmed":true,"reason":"更新"}`, "formal-auth-"+tc.name, "trace-formal-auth")
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.op == nil || tc.op.PortalType != "INTERNAL" {
				if fake.updateCalls != 0 {
					t.Fatalf("unauthorized request reached writer: %d", fake.updateCalls)
				}
			}
		})
	}
}
