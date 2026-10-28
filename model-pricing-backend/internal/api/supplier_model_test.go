package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/infra/supplypg"
)

// fakeSupplierModelStore 记录收到的 supplier_id 与分页，用来验证隔离与参数兜底。
type fakeSupplierModelStore struct {
	gotSupplierID int64
	gotPage       int
	gotSize       int
	rows          []supplypg.SupplierModelRecord
	total         int64
	allTotal      int64
	options       supplypg.SupplierModelFilterOptions
}

func (f *fakeSupplierModelStore) List(_ context.Context, supplierID int64, page, size int) ([]supplypg.SupplierModelRecord, int64, error) {
	f.gotSupplierID, f.gotPage, f.gotSize = supplierID, page, size
	return f.rows, f.total, nil
}

func (f *fakeSupplierModelStore) CountAll(_ context.Context, supplierID int64) (int64, error) {
	f.gotSupplierID = supplierID
	return f.allTotal, nil
}

func (f *fakeSupplierModelStore) FilterOptions(_ context.Context, supplierID int64) (*supplypg.SupplierModelFilterOptions, error) {
	f.gotSupplierID = supplierID
	return &f.options, nil
}

func newSupplierModelTestContext(t *testing.T, operator *middleware.Operator, rawURL string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	if rawURL == "" {
		rawURL = "/api/supplier/models"
	}
	c.Request = httptest.NewRequest("GET", rawURL, nil)
	if operator != nil {
		// 与 middleware.OperatorFrom 读的是同一个键（auth.operator）。
		c.Set("auth.operator", operator)
	}
	return c, recorder
}

// 供应商只能看到自己的模型：查询必须带登录态的 supplier_id。
func TestSupplierModelListScopesToLoggedInSupplier(t *testing.T) {
	store := &fakeSupplierModelStore{allTotal: 3}
	handler := &supplierModelHandler{svc: NewSupplierModelService(store)}
	c, recorder := newSupplierModelTestContext(t, &middleware.Operator{OperatorType: "SUPPLIER", PortalType: "SUPPLIER", OperatorID: 3, AccountID: 20}, "")

	handler.list(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	if store.gotSupplierID != 3 {
		t.Fatalf("查询没有按登录态收敛，supplier_id=%d", store.gotSupplierID)
	}
	var body struct {
		Data supplierModelPageDTO `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if body.Data.List == nil || len(body.Data.List) != 0 {
		t.Fatalf("空结果应回空数组而不是 null：%+v", body.Data)
	}
	if body.Data.Page != 1 || body.Data.Size != supplierModelDefaultSize {
		t.Fatalf("默认分页不符：%+v", body.Data)
	}
	// 可见 0 条、名下共 3 条 → 页面才知道要提示「还有 3 个在流程中」。
	if body.Data.Total != 0 || body.Data.HiddenTotal != 3 {
		t.Fatalf("hidden_total 不符：total=%d hidden=%d", body.Data.Total, body.Data.HiddenTotal)
	}
}

// 筛选候选值走独立接口，且取自全部自有资源（不按可见性过滤），
// 否则页面一空、三个下拉就没有候选值了。
func TestSupplierModelFilterOptions(t *testing.T) {
	store := &fakeSupplierModelStore{options: supplypg.SupplierModelFilterOptions{
		Companies: []string{"Anthropic"}, ModelTypes: []string{"TEXT"}, States: []string{"PENDING_TEST"},
	}}
	handler := &supplierModelHandler{svc: NewSupplierModelService(store)}
	c, recorder := newSupplierModelTestContext(t, &middleware.Operator{OperatorType: "SUPPLIER", PortalType: "SUPPLIER", OperatorID: 3}, "/api/supplier/models/filter-options")

	handler.filterOptions(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data supplierModelFilterOptionsDTO `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if len(body.Data.Companies) != 1 || body.Data.Companies[0] != "Anthropic" ||
		len(body.Data.ModelTypes) != 1 || len(body.Data.States) != 1 {
		t.Fatalf("候选值不符：%+v", body.Data)
	}

	// 空主体也要回空数组而不是 null
	empty := &supplierModelHandler{svc: NewSupplierModelService(&fakeSupplierModelStore{})}
	c2, recorder2 := newSupplierModelTestContext(t, &middleware.Operator{OperatorType: "SUPPLIER", OperatorID: 3}, "/api/supplier/models/filter-options")
	empty.filterOptions(c2)
	var body2 struct {
		Data supplierModelFilterOptionsDTO `json:"data"`
	}
	if err := json.Unmarshal(recorder2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if body2.Data.Companies == nil || body2.Data.ModelTypes == nil || body2.Data.States == nil {
		t.Fatalf("空候选值应为 []：%+v", body2.Data)
	}
}

// 没有供应商登录态一律 401，且不查库。
func TestSupplierModelListRequiresSupplierLogin(t *testing.T) {
	store := &fakeSupplierModelStore{}
	handler := &supplierModelHandler{svc: NewSupplierModelService(store)}
	c, recorder := newSupplierModelTestContext(t, &middleware.Operator{OperatorType: "INTERNAL", OperatorID: 3}, "")

	handler.list(c)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("非供应商身份应 401，实际 %d", recorder.Code)
	}
	if store.gotSupplierID != 0 {
		t.Fatal("未认证时不应发起查询")
	}
}

// 非法筛选值在入口就拦掉。
func TestSupplierModelListRejectsBadFilters(t *testing.T) {
	store := &fakeSupplierModelStore{}
	handler := &supplierModelHandler{svc: NewSupplierModelService(store)}
	for _, target := range []string{"/api/supplier/models?region=DOMESTIC", "/api/supplier/models?state=APPROVED"} {
		c, recorder := newSupplierModelTestContext(t, &middleware.Operator{OperatorType: "SUPPLIER", OperatorID: 3}, target)
		handler.list(c)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s 期望 400，实际 %d", target, recorder.Code)
		}
	}
}

// 分页参数兜底：不传按默认，超上限按上限。gin 会缓存 query，所以每种情况新建 Context。
func TestSupplierModelQueryDefaults(t *testing.T) {
	query := func(raw string) supplierModelQuery {
		c, _ := newSupplierModelTestContext(t, nil, "/api/supplier/models"+raw)
		return supplierModelList(c)
	}
	if got := query(""); got.Page != 1 || got.Size != supplierModelDefaultSize {
		t.Fatalf("默认分页不符：%+v", got)
	}
	if got := query("?page=0&size=999"); got.Page != 1 || got.Size != supplierModelMaxSize {
		t.Fatalf("分页兜底不符：%+v", got)
	}
	if got := query("?page=3&size=5"); got.Page != 3 || got.Size != 5 {
		t.Fatalf("正常分页不符：%+v", got)
	}
}

// 记录 → 前端契约：测没测、过没过、指标、报价阻断原因。
func TestBuildSupplierModelDTO(t *testing.T) {
	ms, rps, tps := "87.00", "28.40", "24500.00"
	tested := supplypg.SupplierModelRecord{
		ID: "9", StandardModelID: "15", ModelName: "Claude Sonnet 4", OfficialID: "claude-sonnet-4",
		SupplierModelID: "vendor-x", Region: "CN", Coefficient: "0.6800", State: "ADMITTED", EverAdmitted: true,
		HasReport: true, ReportID: "77", ConnectionPassed: true, StressPassed: false,
		ConnectionMS: &ms, RPS: &rps, TPS: &tps,
	}
	dto := buildSupplierModelDTO(tested)
	if dto.Revision != "1" || dto.Coefficient != "0.6800" || dto.Region != "CN" {
		t.Fatalf("基础字段映射不符：%+v", dto)
	}
	if !dto.Test.ConnectivityTested || !dto.Test.ConnectivityPassed || !dto.Test.StressTested || dto.Test.StressPassed {
		t.Fatalf("测试结论映射不符：%+v", dto.Test)
	}
	if dto.Test.ConnectionMS == nil || *dto.Test.ConnectionMS != "87.00" || dto.Test.TPS == nil {
		t.Fatalf("指标未带出：%+v", dto.Test)
	}
	if dto.QuoteReadiness.Eligible || len(dto.QuoteReadiness.BlockedReasons) != 1 || dto.QuoteReadiness.BlockedReasons[0] != "TESTS_NOT_PASSED" {
		t.Fatalf("报价阻断原因不符：%+v", dto.QuoteReadiness)
	}

	// 已接入 + 测试双通过 → 可供报价
	ready := tested
	ready.StressPassed = true
	if got := buildSupplierModelDTO(ready); !got.QuoteReadiness.Eligible || len(got.QuoteReadiness.BlockedReasons) != 0 {
		t.Fatalf("双通过应可报价：%+v", got.QuoteReadiness)
	}

	// 待测试、无报告 → 不可报价，原因里同时有「未接入」「无测试」
	untested := supplypg.SupplierModelRecord{ID: "10", State: "PENDING_TEST", EverAdmitted: false}
	got := buildSupplierModelDTO(untested)
	if got.Test.ConnectivityTested || got.Test.Valid || got.Test.ConnectionMS != nil {
		t.Fatalf("未测试资源不应带测试结论：%+v", got.Test)
	}
	if got.QuoteReadiness.Eligible {
		t.Fatal("未接入资源不应可供报价")
	}
	if len(got.QuoteReadiness.BlockedReasons) != 2 || got.QuoteReadiness.BlockedReasons[0] != "RESOURCE_NOT_ADMITTED" {
		t.Fatalf("阻断原因不符：%+v", got.QuoteReadiness.BlockedReasons)
	}
}
