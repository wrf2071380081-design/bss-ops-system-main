package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
)

// newQueryContext 构造一个只带 query 的 gin 上下文，用于查询参数边界测试。
func newQueryContext(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	return c
}

func TestNormalizeAccountUpdateTrimsAndUppercases(t *testing.T) {
	got, err := normalizeAccountUpdate(supplierAccountUpdateRequest{
		Name:           "  初禾资源（新注册演示）  ",
		RegistrationNo: " DEMO-COMPANY-NEW ",
		Contact:        " 吴安 ",
		Region:         " overseas ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "初禾资源（新注册演示）" || got.RegistrationNo != "DEMO-COMPANY-NEW" || got.Contact != "吴安" {
		t.Fatalf("fields not trimmed: %+v", got)
	}
	if got.Region != "OVERSEAS" {
		t.Fatalf("region should be upper-cased, got %q", got.Region)
	}
}

func TestNormalizeAccountUpdateRejects(t *testing.T) {
	cases := []struct {
		name  string
		in    supplierAccountUpdateRequest
		want  apperr.Error
		label string
	}{
		{"empty-name", supplierAccountUpdateRequest{Name: "   ", Region: "CN"}, errAccountNameRequired, "企业资料必填"},
		{"bad-region", supplierAccountUpdateRequest{Name: "甲", Region: "MARS"}, errAccountRegionInvalid, "主体属性取值不合法"},
		{"blank-region", supplierAccountUpdateRequest{Name: "甲", Region: ""}, errAccountRegionInvalid, "主体属性取值不合法"},
		{"long-name", supplierAccountUpdateRequest{Name: strings.Repeat("长", accountNameMaxLen+1), Region: "CN"}, errAccountFieldTooLong, "字段长度超出上限"},
		{"long-reg-no", supplierAccountUpdateRequest{Name: "甲", RegistrationNo: strings.Repeat("A", accountRegNoMaxLen+1), Region: "CN"}, errAccountFieldTooLong, "字段长度超出上限"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeAccountUpdate(tc.in)
			var appErr apperr.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("want apperr, got %v", err)
			}
			if appErr.Message != tc.label || appErr.Status != tc.want.Status {
				t.Fatalf("want %s/%d, got %s/%d", tc.label, tc.want.Status, appErr.Message, appErr.Status)
			}
		})
	}
}

func TestNormalizeAccountUpdateBoundaryLengths(t *testing.T) {
	// 边界值必须放行：128 / 64 / 64 是列宽，不是"小于"。
	if _, err := normalizeAccountUpdate(supplierAccountUpdateRequest{
		Name:           strings.Repeat("长", accountNameMaxLen),
		RegistrationNo: strings.Repeat("A", accountRegNoMaxLen),
		Contact:        strings.Repeat("人", accountContactMaxLen),
		Region:         "CN",
	}); err != nil {
		t.Fatalf("boundary lengths should pass, got %v", err)
	}
}

func TestBuildSupplierAccountDTOMarksRegionLocked(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)
	uploaded := time.Date(2026, 9, 22, 14, 20, 0, 0, time.UTC)

	cases := []struct {
		name       string
		rec        supplypg.AccountRecord
		wantLocked bool
		wantReason string
	}{
		{
			name:       "ever-admitted",
			rec:        supplypg.AccountRecord{ID: 1001, Name: "澄海智算", Region: "CN", EverAdmitted: true, ResourceCount: 2, UpdatedAt: now},
			wantLocked: true,
			wantReason: "有上架历史，已锁定",
		},
		{
			name:       "resource-only",
			rec:        supplypg.AccountRecord{ID: 1002, Name: "甲", Region: "CN", ResourceCount: 1, UpdatedAt: now},
			wantLocked: true,
			wantReason: "已有资源或申报，已锁定",
		},
		{
			name:       "submission-only",
			rec:        supplypg.AccountRecord{ID: 1003, Name: "乙", Region: "OVERSEAS", SubmissionCount: 1, UpdatedAt: now},
			wantLocked: true,
			wantReason: "已有资源或申报，已锁定",
		},
		{
			name:       "fresh-subject",
			rec:        supplypg.AccountRecord{ID: 1004, Name: "初禾资源", Region: "OVERSEAS", UpdatedAt: now},
			wantLocked: false,
			wantReason: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dto := buildSupplierAccountDTO(&tc.rec)
			if dto.RegionLocked != tc.wantLocked {
				t.Fatalf("regionLocked want %v got %v", tc.wantLocked, dto.RegionLocked)
			}
			if dto.RegionLockReason != tc.wantReason {
				t.Fatalf("reason want %q got %q", tc.wantReason, dto.RegionLockReason)
			}
			if dto.License != nil {
				t.Fatalf("license should be null when not uploaded")
			}
			if dto.UpdatedAt != now.Format(time.RFC3339) {
				t.Fatalf("updatedAt format: %q", dto.UpdatedAt)
			}
		})
	}

	rec := supplypg.AccountRecord{
		ID: 1001, Name: "澄海智算", Region: "CN", EverAdmitted: true,
		LicenseName: "澄海智算企业证明.txt", LicenseRef: "supplier-license/3/1.txt",
		LicenseUploadedAt: &uploaded, UpdatedAt: now,
	}
	dto := buildSupplierAccountDTO(&rec)
	if dto.License == nil || dto.License.Name != "澄海智算企业证明.txt" || dto.License.FileRef != "supplier-license/3/1.txt" {
		t.Fatalf("license not mapped: %+v", dto.License)
	}
	if dto.License.UploadedAt != uploaded.Format(time.RFC3339) {
		t.Fatalf("license uploadedAt: %q", dto.License.UploadedAt)
	}
}

func TestPositiveIntQueryBounds(t *testing.T) {
	// 这里的核心是"越界不静默截断"：page=0 / size=101 必须报错而不是被改成默认值。
	if got, err := positiveIntQuery(newQueryContext(""), "page", 1, 10000); err != nil || got != 1 {
		t.Fatalf("default page: %d %v", got, err)
	}
	if _, err := positiveIntQuery(newQueryContext("page=0"), "page", 1, 10000); err == nil {
		t.Fatal("page=0 should fail")
	}
	if _, err := positiveIntQuery(newQueryContext("size=101"), "size", 1, 100); err == nil {
		t.Fatal("size=101 should fail")
	}
	if _, err := positiveIntQuery(newQueryContext("page=abc"), "page", 1, 10000); err == nil {
		t.Fatal("page=abc should fail")
	}
}
