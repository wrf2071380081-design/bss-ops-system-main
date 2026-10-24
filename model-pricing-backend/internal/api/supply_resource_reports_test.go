package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

type resourceReportReaderHTTPFake struct {
	page          *supply.ResourceReportPage
	detail        *supply.ResourceReportDetail
	file          *supply.ResourceReportFileContent
	err           error
	wrongResource bool
	listActor     string
	getActor      string
	fileActor     string
	openCalls     int
}

func (f *resourceReportReaderHTTPFake) List(_ context.Context, actorID, resourceID string, page, size int) (*supply.ResourceReportPage, error) {
	f.listActor = actorID
	if f.err != nil {
		return nil, f.err
	}
	result := *f.page
	result.Page, result.Size = page, size
	if !f.wrongResource {
		for i := range result.List {
			result.List[i].ResourceID = resourceID
		}
	}
	return &result, nil
}
func (f *resourceReportReaderHTTPFake) Get(_ context.Context, actorID, resourceID, reportID string) (*supply.ResourceReportDetail, error) {
	f.getActor = actorID
	if f.err != nil {
		return nil, f.err
	}
	result := *f.detail
	if !f.wrongResource {
		result.ResourceID = resourceID
	}
	result.ID = reportID
	result.CacheStats.ReportID = reportID
	return &result, nil
}
func (f *resourceReportReaderHTTPFake) OpenFile(_ context.Context, actorID, _, _, _ string) (*supply.ResourceReportFileContent, error) {
	f.fileActor = actorID
	f.openCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.file, nil
}

func resourceReportHTTPFixture() supply.ResourceReportView {
	finished := time.Date(2026, 10, 22, 10, 1, 0, 0, time.UTC)
	testedAt := finished.Add(-30 * time.Second)
	connectivity, pressure := true, true
	targetMatches := true
	return supply.ResourceReportView{
		ID: "21", ResourceID: "11", Status: "COMPLETED", CreatedAt: finished.Add(-time.Minute), TestedAt: testedAt, TestItem: "stress", OperatorName: "测评人员甲", Scene: "压力组合场景", FinishedAt: &finished,
		TargetMatches: &targetMatches, ConnectivityPassed: &connectivity, PressurePassed: &pressure,
		Metrics:    supply.ResourceReportMetrics{ConnectionMS: reportTestStringPtr("0"), RPS: reportTestStringPtr("12.50")},
		CacheStats: supply.ResourceCacheStats{ReportID: "21", Status: "NOT_PROVIDED"},
	}
}

func resourceReportHTTPRouter(op *middleware.Operator, reader SupplyResourceReportReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		if op != nil {
			c.Set("auth.operator", op)
		}
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{ResourceReports: reader})
	return r
}

func TestResourceReportHTTPListsAndReadsSafeProjectionForCurrentStaff(t *testing.T) {
	row := resourceReportHTTPFixture()
	fake := &resourceReportReaderHTTPFake{
		page:   &supply.ResourceReportPage{List: []supply.ResourceReportView{row}, Total: 1},
		detail: &supply.ResourceReportDetail{ResourceReportView: row, Files: []supply.ResourceReportFile{}},
	}
	r := resourceReportHTTPRouter(supplyHTTPStaff(), fake)
	list := supplyHTTPCall(r, http.MethodGet, "/api/internal/resources/11/test-reports", "", "", "report-list")
	if list.Code != http.StatusOK || fake.listActor != "7" {
		t.Fatalf("list status=%d actor=%s body=%s", list.Code, fake.listActor, list.Body.String())
	}
	data := supplyHTTPEnvelope(t, list)["data"].(map[string]any)
	item := data["list"].([]any)[0].(map[string]any)
	if item["id"] != "21" || item["resource_id"] != "11" || item["target_matches"] != true || item["report_file"] != nil {
		t.Fatalf("unsafe or incorrect report projection: %+v", item)
	}
	if item["test_item"] != "stress" || item["operator_name"] != "测评人员甲" || item["scene"] != "压力组合场景" || item["source"] != "" {
		t.Fatalf("report projection invented provenance or omitted tested facts: %+v", item)
	}
	detail := supplyHTTPCall(r, http.MethodGet, "/api/internal/resources/11/test-reports/21", "", "", "report-detail")
	if detail.Code != http.StatusOK || fake.getActor != "7" {
		t.Fatalf("detail status=%d actor=%s body=%s", detail.Code, fake.getActor, detail.Body.String())
	}
	detailData := supplyHTTPEnvelope(t, detail)["data"].(map[string]any)
	if files, ok := detailData["files"].([]any); !ok || len(files) != 0 {
		t.Fatalf("detail did not return a safe empty file list: %+v", detailData["files"])
	}
}

func TestResourceReportHTTPStreamsOnlyListedDownloadableFile(t *testing.T) {
	row := resourceReportHTTPFixture()
	fake := &resourceReportReaderHTTPFake{
		detail: &supply.ResourceReportDetail{ResourceReportView: row, Files: []supply.ResourceReportFile{{ID: "31", Name: "report.pdf", CanDownload: true}}},
		file:   &supply.ResourceReportFileContent{Name: "report.pdf", ContentType: "application/pdf", Size: 7, Body: io.NopCloser(strings.NewReader("pdfdata"))},
	}
	r := resourceReportHTTPRouter(supplyHTTPStaff(), fake)
	res := supplyHTTPCall(r, http.MethodGet, "/api/internal/resources/11/test-reports/21/files/31", "", "", "report-file")
	if res.Code != http.StatusOK || fake.getActor != "7" || fake.fileActor != "7" || fake.openCalls != 1 {
		t.Fatalf("download status=%d actor=%s/%s calls=%d body=%s", res.Code, fake.getActor, fake.fileActor, fake.openCalls, res.Body.String())
	}
	if res.Body.String() != "pdfdata" || res.Header().Get("Content-Type") != "application/pdf" || res.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(res.Header().Get("Content-Disposition"), "report.pdf") {
		t.Fatalf("unsafe or incorrect download headers/body: headers=%v body=%q", res.Header(), res.Body.String())
	}
	fake.detail.Files[0].CanDownload = false
	blocked := supplyHTTPCall(r, http.MethodGet, "/api/internal/resources/11/test-reports/21/files/31", "", "", "report-file-blocked")
	if blocked.Code != http.StatusForbidden || fake.openCalls != 1 {
		t.Fatalf("non-downloadable attachment reached provider: status=%d calls=%d", blocked.Code, fake.openCalls)
	}
}

func TestResourceReportHTTPRejectsMissingProviderAndMismatchedResource(t *testing.T) {
	missing := supplyHTTPCall(resourceReportHTTPRouter(supplyHTTPStaff(), nil), http.MethodGet, "/api/internal/resources/11/test-reports", "", "", "report-missing")
	if missing.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing report provider returned %d: %s", missing.Code, missing.Body.String())
	}
	row := resourceReportHTTPFixture()
	row.ResourceID = "99"
	fake := &resourceReportReaderHTTPFake{page: &supply.ResourceReportPage{List: []supply.ResourceReportView{row}, Total: 1}, wrongResource: true}
	mismatch := supplyHTTPCall(resourceReportHTTPRouter(supplyHTTPStaff(), fake), http.MethodGet, "/api/internal/resources/11/test-reports", "", "", "report-mismatch")
	if mismatch.Code != http.StatusInternalServerError {
		t.Fatalf("mismatched report resource returned %d: %s", mismatch.Code, mismatch.Body.String())
	}
}

func reportTestStringPtr(value string) *string { return &value }
