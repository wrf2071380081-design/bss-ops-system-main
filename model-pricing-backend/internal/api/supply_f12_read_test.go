package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
)

type f12HTTPReaderFake struct {
	contractQuery supply.F12ContractQuery
	creditQuery   supply.F12CreditQuery
}

func (f *f12HTTPReaderFake) ListContracts(_ context.Context, q supply.F12ContractQuery) (*supply.F12ContractPage, error) {
	f.contractQuery = q
	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	return &supply.F12ContractPage{List: []*supply.F12ContractRecord{{ID: "8", SubjectType: q.SubjectType, SubjectID: q.SubjectID, Status: "PROCESSING", Version: 2, HasAttachment: true, CreatedAt: now, UpdatedAt: now}}, Total: 1, Page: q.Page, Size: q.Size}, nil
}

func TestF12ReadHTTPForwardsArchiveLookup(t *testing.T) {
	reader := &f12HTTPReaderFake{}
	r := f12HTTPRouter(supplyHTTPStaff(), reader)
	rec := supplyHTTPCall(r, "GET", "/api/internal/contracts?subject_type=CUSTOMER&subject_id=21&contract_key=8&latest_only=true&page=1&size=2", "", "", "")
	if rec.Code != 200 || reader.contractQuery.ContractKey != "8" || !reader.contractQuery.LatestOnly || reader.contractQuery.SubjectType != "CUSTOMER" {
		t.Fatalf("archive lookup context lost: %d %+v", rec.Code, reader.contractQuery)
	}
	for _, query := range []string{"latest_only=true", "contract_key=0", "contract_key=8&latest_only=1", "contract_key=8%20OR%201%3D1"} {
		rec = supplyHTTPCall(r, "GET", "/api/internal/contracts?subject_type=SUPPLIER&subject_id=21&"+query, "", "", "")
		if rec.Code != 400 { t.Fatalf("invalid archive query accepted: %s %d", query, rec.Code) }
	}
}
func (f *f12HTTPReaderFake) ListCredits(_ context.Context, q supply.F12CreditQuery) (*supply.F12CreditPage, error) {
	f.creditQuery = q
	return &supply.F12CreditPage{List: []*supply.F12CreditRecord{}, Current: []*supply.F12CurrentCredit{}, Total: 0, Page: q.Page, Size: q.Size}, nil
}

func f12HTTPRouter(op *middleware.Operator, reader SupplyF12Reader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{F12Reader: reader})
	return r
}

func TestF12ReadHTTPProvidesOnlySafeV36Projection(t *testing.T) {
	reader := &f12HTTPReaderFake{}
	r := f12HTTPRouter(supplyHTTPStaff(), reader)
	rec := supplyHTTPCall(r, "GET", "/api/internal/contracts?subject_type=SUPPLIER&subject_id=21&page=1&size=20", "", "", "")
	if rec.Code != 200 || reader.contractQuery.SubjectID != "21" || !strings.Contains(rec.Body.String(), `"has_attachment":true`) || strings.Contains(rec.Body.String(), "file_ref") || strings.Contains(rec.Body.String(), "note") {
		t.Fatalf("unsafe or invalid contract projection: %d %s", rec.Code, rec.Body.String())
	}

	rec = supplyHTTPCall(r, "GET", "/api/internal/credits?subject_type=SUPPLIER&subject_id=21&direction=SUPPLIER_TO_US&currency=CNY&as_of=2026-10-22T08%3A00%3A00Z", "", "", "")
	if rec.Code != 200 || reader.creditQuery.Direction != "SUPPLIER_TO_US" || reader.creditQuery.Currency != "CNY" || reader.creditQuery.AsOf == nil {
		t.Fatalf("invalid credit read query: %d %s %+v", rec.Code, rec.Body.String(), reader.creditQuery)
	}
	rec = supplyHTTPCall(r, "GET", "/api/internal/contracts?subject_type=CUSTOMER&subject_id=21", "", "", "")
	if rec.Code != 200 || reader.contractQuery.SubjectType != "CUSTOMER" || !strings.Contains(rec.Body.String(), `"subject_type":"CUSTOMER"`) {
		t.Fatalf("customer contract history should use the customer subject context: %d %s %+v", rec.Code, rec.Body.String(), reader.contractQuery)
	}
	rec = supplyHTTPCall(r, "GET", "/api/internal/credits?subject_type=CUSTOMER&subject_id=21&direction=US_TO_CUSTOMER", "", "", "")
	if rec.Code != 200 || reader.creditQuery.SubjectType != "CUSTOMER" || reader.creditQuery.Direction != "US_TO_CUSTOMER" {
		t.Fatalf("customer credit history should use the customer direction: %d %s %+v", rec.Code, rec.Body.String(), reader.creditQuery)
	}
}

func TestF12ReadHTTPRejectsCrossSubjectAndUnknownQuery(t *testing.T) {
	r := f12HTTPRouter(supplyHTTPStaff(), &f12HTTPReaderFake{})
	for _, path := range []string{
		"/api/internal/contracts?subject_type=BROKER&subject_id=21",
		"/api/internal/contracts?subject_type=SUPPLIER&subject_id=21&note=leak",
		"/api/internal/credits?subject_type=SUPPLIER&subject_id=21&as_of=tomorrow",
		"/api/internal/credits?subject_type=SUPPLIER&subject_id=21&currency=cny",
		"/api/internal/credits?subject_type=SUPPLIER&subject_id=21&direction=US_TO_CUSTOMER",
		"/api/internal/credits?subject_type=CUSTOMER&subject_id=21&direction=SUPPLIER_TO_US",
	} {
		rec := supplyHTTPCall(r, "GET", path, "", "", "")
		if rec.Code != 400 {
			t.Fatalf("accepted invalid F12 query %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	unauthorized := f12HTTPRouter(nil, &f12HTTPReaderFake{})
	if rec := supplyHTTPCall(unauthorized, "GET", "/api/internal/contracts?subject_type=SUPPLIER&subject_id=21", "", "", ""); rec.Code != 401 {
		t.Fatalf("unauthenticated F12 read returned %d", rec.Code)
	}
}
