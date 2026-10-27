package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
)

type supplierMergePreviewHTTPFake struct {
	preview        *supply.SupplierMergePreview
	source, target string
}

func (f *supplierMergePreviewHTTPFake) Preview(_ context.Context, source, target string) (*supply.SupplierMergePreview, error) {
	f.source, f.target = source, target
	return f.preview, nil
}

func supplierMergePreviewHTTPRouter(op *middleware.Operator, reader SupplySupplierMergePreviewReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{SupplierMergePreviewReader: reader})
	return r
}

func TestSupplierMergePreviewHTTPReturnsReadOnlyReferenceSummary(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	zero := int64(0)
	fake := &supplierMergePreviewHTTPFake{preview: &supply.SupplierMergePreview{
		Source:                supply.SupplierMergePartySummary{ID: "21", Name: "来源甲", Region: "CN", BuyerID: "7", SettlementCurrency: "CNY", Revision: 2, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now},
		Target:                supply.SupplierMergePartySummary{ID: "22", Name: "目标乙", Region: "CN", BuyerID: "7", SettlementCurrency: "CNY", Revision: 3, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now},
		SourceReferences:      supply.SupplierMergeReferenceCounts{Resources: 1, PendingSubmissions: 1, Submissions: 1, WebsiteAccountConfigured: true, Aliases: &zero},
		TargetReferences:      supply.SupplierMergeReferenceCounts{Aliases: &zero},
		DuplicateResourceKeys: 1,
		ReviewItems:           []supply.SupplierMergeReviewItem{{Code: "DUPLICATE_RESOURCE_KEY", Title: "相同模型与区域存在重复资源", SourceCount: 1, TargetCount: 1}},
	}}
	rec := supplyHTTPCall(supplierMergePreviewHTTPRouter(supplyHTTPStaff(), fake), "POST", "/api/internal/supplier-entities/21/merge-preview", `{"target_supplier_id":"22"}`, "", "merge-preview-test")
	if rec.Code != 200 || fake.source != "21" || fake.target != "22" || !strings.Contains(rec.Body.String(), `"duplicate_resource_keys":1`) || !strings.Contains(rec.Body.String(), `"DUPLICATE_RESOURCE_KEY"`) || strings.Contains(rec.Body.String(), "can_merge") || strings.Contains(rec.Body.String(), "api_key") || strings.Contains(rec.Body.String(), "file_ref") {
		t.Fatalf("merge preview response or boundary was invalid: %d %s", rec.Code, rec.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if data["source"].(map[string]any)["revision"] != "2" || data["target"].(map[string]any)["revision"] != "3" {
		t.Fatalf("large revisions must stay as strings: %s", rec.Body.String())
	}
}

func TestSupplierMergePreviewHTTPRejectsSelfMergeAndUnknownFields(t *testing.T) {
	fake := &supplierMergePreviewHTTPFake{}
	router := supplierMergePreviewHTTPRouter(supplyHTTPStaff(), fake)
	for _, body := range []string{`{"target_supplier_id":"21"}`, `{"target_supplier_id":"22","api_key":"must-not-be-accepted"}`} {
		rec := supplyHTTPCall(router, "POST", "/api/internal/supplier-entities/21/merge-preview", body, "", "merge-preview-invalid")
		if rec.Code != 400 {
			t.Fatalf("invalid merge preview request accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	if fake.source != "" || fake.target != "" {
		t.Fatalf("invalid request reached the reader: %s/%s", fake.source, fake.target)
	}
	if rec := supplyHTTPCall(supplierMergePreviewHTTPRouter(nil, fake), "POST", "/api/internal/supplier-entities/21/merge-preview", `{"target_supplier_id":"22"}`, "", "merge-preview-auth"); rec.Code != 401 {
		t.Fatalf("unauthenticated preview returned %d", rec.Code)
	}
}
