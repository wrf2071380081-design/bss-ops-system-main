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

type supplierQualificationHTTPFake struct {
	profile *supply.Supplier
	query   supply.SupplierQualificationQuery
	fileRef string
}

func (f *supplierQualificationHTTPFake) Get(_ context.Context, id string) (*supply.Supplier, error) {
	if f.profile == nil || f.profile.ID != id {
		return nil, supply.ErrNotFound
	}
	return f.profile.Clone(), nil
}

func (f *supplierQualificationHTTPFake) List(context.Context, supply.SupplierListQuery) (*supply.SupplierPage, error) {
	return &supply.SupplierPage{List: []supply.Supplier{}, Page: 1, Size: 20}, nil
}

func (f *supplierQualificationHTTPFake) ListQualifications(_ context.Context, id string, query supply.SupplierQualificationQuery) (*supply.SupplierQualificationPage, error) {
	f.query = query
	return &supply.SupplierQualificationPage{List: []supply.SupplierQualification{{ID: "81", SupplierID: id, Name: "营业执照", HasAttachment: true, FileReference: f.fileRef, UploadedAt: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}}, Total: 1, Page: query.Page, Size: query.Size}, nil
}

func supplierQualificationHTTPRouter(op *middleware.Operator, fake *supplierQualificationHTTPFake, qualificationFiles ...*supply.SupplierQualificationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	deps := SupplyDeps{SupplierReader: supply.NewSupplierReader(fake)}
	if len(qualificationFiles) > 0 {
		deps.SupplierQualificationFiles = qualificationFiles[0]
	}
	RegisterSupplyRoutes(r.Group("/api/internal"), deps)
	return r
}

func TestSupplierQualificationHTTPAddsSafeFileNameForDownloadableAttachment(t *testing.T) {
	profile, err := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("%PDF-1.7\nbody")
	ref := "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake := &supplierQualificationHTTPFake{profile: profile, fileRef: ref}
	blob := &supply.SupplierQualificationFile{ID: ref, Name: "license.pdf", ContentType: "application/pdf", Size: len(content), Content: content}
	files := supply.NewSupplierQualificationService(&qualificationUploadStoreFake{ref: ref}, &qualificationUploadBlobFake{opened: blob}, time.Now)
	rec := supplyHTTPCall(supplierQualificationHTTPRouter(supplyHTTPStaff(), fake, files), "GET", "/api/internal/supplier-entities/21/qualifications", "", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"can_download":true`) || !strings.Contains(rec.Body.String(), `"file_name":"license.pdf"`) || strings.Contains(rec.Body.String(), ref) || strings.Contains(rec.Body.String(), "file_ref") {
		t.Fatalf("safe downloadable qualification metadata was not returned: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSupplierQualificationHTTPReturnsSafePagedMetadata(t *testing.T) {
	profile, err := supply.NewSupplier("21", "7", supply.SupplierData{Name: "供应商甲", Region: "CN", SettlementCurrency: "CNY"}, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	fake := &supplierQualificationHTTPFake{profile: profile}
	rec := supplyHTTPCall(supplierQualificationHTTPRouter(supplyHTTPStaff(), fake), "GET", "/api/internal/supplier-entities/21/qualifications?page=2&size=7", "", "", "")
	if rec.Code != 200 || fake.query.Page != 2 || fake.query.Size != 7 || !strings.Contains(rec.Body.String(), `"has_attachment":true`) || strings.Contains(rec.Body.String(), "file_ref") || strings.Contains(rec.Body.String(), "storage_path") {
		t.Fatalf("qualification metadata response was invalid or leaked a storage reference: %d query=%+v body=%s", rec.Code, fake.query, rec.Body.String())
	}
	for _, path := range []string{"/api/internal/supplier-entities/21/qualifications?file_ref=1", "/api/internal/supplier-entities/21/qualifications?page=0"} {
		if bad := supplyHTTPCall(supplierQualificationHTTPRouter(supplyHTTPStaff(), fake), "GET", path, "", "", ""); bad.Code != 400 {
			t.Fatalf("invalid qualification query accepted: %s => %d %s", path, bad.Code, bad.Body.String())
		}
	}
	if unauthorized := supplyHTTPCall(supplierQualificationHTTPRouter(nil, fake), "GET", "/api/internal/supplier-entities/21/qualifications", "", "", ""); unauthorized.Code != 401 {
		t.Fatalf("unauthenticated qualification request returned %d", unauthorized.Code)
	}
}
