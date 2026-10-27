package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

type resourceHTTPStore struct {
	row   *supply.ResourceRecord
	query supply.ResourceListQuery
	empty bool
}

func (s *resourceHTTPStore) ResourceFilterOptions(_ context.Context, q supply.ResourceOptionsQuery) (supply.ResourceFilterOptions, error) {
	return supply.ResourceFilterOptions{Companies: []string{"示例公司"}, Suppliers: []supply.ResourceSupplierOption{{ID: "21", Name: "示例供应商", Region: "CN"}}}, nil
}

func (s *resourceHTTPStore) ListResourceGroups(_ context.Context, q supply.ResourceListQuery) ([][]*supply.ResourceRecord, int64, error) {
	s.query = q
	return [][]*supply.ResourceRecord{{s.row}}, 1, nil
}
func (s *resourceHTTPStore) ListResourceRows(_ context.Context, q supply.ResourceListQuery) ([]*supply.ResourceRecord, int64, error) {
	s.query = q
	if s.empty {
		return []*supply.ResourceRecord{}, 0, nil
	}
	return []*supply.ResourceRecord{s.row}, 1, nil
}

func TestSupplyResourceHTTPMultiFilters(t *testing.T) {
	store := resourceHTTPFixture(t)
	store.empty = true
	values := url.Values{"view": {"flat"}, "companies": {`["Company, LLC","示例公司"]`}, "model_types": {`["TEXT","IMAGE"]`}, "supplier_ids": {`["21","9007199254740993"]`}, "regions": {`["CN","OVERSEAS"]`}, "states": {`["ADMITTED","PENDING_TEST"]`}, "coefficient_min": {"0"}, "coefficient_max": {"1"}, "connection_max": {"0"}, "rps_min": {"9007199254740993"}, "tps_max": {"9007199254740994"}, "sort_by": {"admitted_at"}, "order": {"desc"}}
	router := resourceHTTPRouter(store, "staff", false)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/internal/resources?"+values.Encode(), nil))
	if w.Code != 200 || len(store.query.Companies) != 2 || store.query.Companies[0] != "Company, LLC" || store.query.SupplierIDs[1] != "9007199254740993" || store.query.ConnectionMax != "0" || store.query.RPSMin != "9007199254740993" || store.query.SortBy != "admitted_at" {
		t.Fatalf("query=%+v status=%d body=%s", store.query, w.Code, w.Body.String())
	}
	for _, query := range []url.Values{{"supplier_ids": {`[21]`}}, {"supplier_ids": {`["21","21"]`}}, {"supplier_id": {"21"}, "supplier_ids": {`["21"]`}}, {"regions": {`["UNKNOWN"]`}}, {"coefficient_min": {"1.1"}}, {"connection_max": {"-1"}}, {"rps_min": {"2"}, "rps_max": {"1"}}, {"tps_max": {"NaN"}}} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/internal/resources?"+query.Encode(), nil))
		if w.Code != 400 {
			t.Fatalf("accepted query %s: %d %s", query.Encode(), w.Code, w.Body.String())
		}
	}
}
func (s *resourceHTTPStore) GetResource(_ context.Context, id string) (*supply.ResourceRecord, error) {
	if id != s.row.Resource.ID {
		return nil, supply.ErrNotFound
	}
	return s.row, nil
}
func resourceHTTPFixture(t *testing.T) *resourceHTTPStore {
	t.Helper()
	m, err := supply.NewStandardModel("11", "7", supply.ModelData{Company: "示例公司", Name: "示例模型", OfficialID: "model-v1", ModelType: "TEXT"}, time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return &resourceHTTPStore{row: &supply.ResourceRecord{Resource: &supply.Resource{ID: "9007199254740993", SupplierID: "21", StandardModelID: "11", Region: "CN", SupplierModelID: "supplier-model", Coefficient: "0.68", BuyerID: "7", State: "PENDING_TEST", Revision: 1, OriginRowID: "41", OriginRowVersion: 1, TargetVersion: 1}, Model: m, Supplier: supply.ResourceSupplier{ID: "21", Name: "示例供应商", Region: "CN", Status: "ACTIVE"}}}
}
func resourceHTTPRouter(store *resourceHTTPStore, identity string, missing bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	group := router.Group("/api/internal")
	group.Use(func(c *gin.Context) {
		if identity != "anonymous" {
			portal, kind := "INTERNAL", "STAFF"
			if identity == "supplier" {
				portal = "SUPPLIER"
				kind = "SUPPLIER"
			}
			staffID := int64(7)
			if identity == "other-staff" {
				staffID = 8
			}
			c.Set("auth.operator", &middleware.Operator{PortalType: portal, OperatorType: kind, StaffID: staffID, OperatorID: 88, AccountID: 99})
		}
		c.Next()
	})
	deps := SupplyDeps{}
	if !missing {
		deps.Resources = supply.NewResourceReader(store)
	}
	RegisterSupplyRoutes(group, deps)
	return router
}
func TestSupplyResourceHTTPReadViewsAndIdentity(t *testing.T) {
	store := resourceHTTPFixture(t)
	for _, tc := range []struct {
		identity, path string
		status         int
		missing        bool
	}{{"staff", "/resources", 200, false}, {"staff", "/resources?view=flat&standard_model_id=11", 200, false}, {"other-staff", "/resources?view=flat", 200, false}, {"staff", "/resources/9007199254740993?region=CN", 200, false}, {"anonymous", "/resources", 401, false}, {"supplier", "/resources", 403, false}, {"staff", "/resources", 503, true}, {"staff", "/resources/9007199254740993?region=OVERSEAS", 404, false}, {"staff", "/resources?unknown=1", 400, false}, {"staff", "/resources?region=CN&region=OVERSEAS", 400, false}, {"staff", "/resources?view=flat&sort_by=supplier_count", 400, false}} {
		t.Run(tc.identity+tc.path, func(t *testing.T) {
			router := resourceHTTPRouter(store, tc.identity, tc.missing)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/internal"+tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.status != 200 && body["data"] != nil {
				t.Fatal("error exposed data")
			}
			if tc.status == 200 {
				data := body["data"].(map[string]any)
				if strings.Contains(tc.path, "/resources/") {
					if data["id"] != "9007199254740993" || data["test"].(map[string]any)["status"] != "NOT_TESTED" {
						t.Fatalf("detail response wrong: %+v", data)
					}
					return
				}
				rows := data["list"].([]any)
				row := rows[0].(map[string]any)
				if strings.Contains(tc.path, "view=flat") {
					if row["id"] != "9007199254740993" || row["coefficient"] != "0.6800" || row["permissions"].(map[string]any)["can_edit"] != (tc.identity != "other-staff") {
						t.Fatalf("flat shape lost IDs or binding: %+v", row)
					}
				} else if row["id"] != "11" || row["resource_count"] != float64(1) {
					t.Fatalf("grouped shape wrong: %+v", row)
				}
			}
		})
	}
}

func TestSupplyResourceHTTPFilterOptions(t *testing.T) {
	for _, tc := range []struct {
		identity, path string
		status         int
		missing        bool
	}{{"staff", "/resources/filter-options?region=CN&keyword=示例", 200, false}, {"anonymous", "/resources/filter-options", 401, false}, {"supplier", "/resources/filter-options", 403, false}, {"staff", "/resources/filter-options", 503, true}, {"staff", "/resources/filter-options?unknown=1", 400, false}} {
		router := resourceHTTPRouter(resourceHTTPFixture(t), tc.identity, tc.missing)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/internal"+tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("options returned %d want%d: %s", w.Code, tc.status, w.Body.String())
		}
		if tc.status == 200 {
			var body struct {
				Data supply.ResourceFilterOptions `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data.Suppliers) != 1 || body.Data.Suppliers[0].ID != "21" {
				t.Fatalf("unsafe option shape: %+v %v", body, err)
			}
		}
	}
}
