package api

import (
	"encoding/json"
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

func TestNormalizeAccountUpdateTrimsFreeText(t *testing.T) {
	got, err := normalizeAccountUpdate(supplierAccountUpdateRequest{
		Name:               "  初禾资源（新注册演示）  ",
		RegistrationNo:     " DEMO-COMPANY-NEW ",
		Contact:            " 吴安 ",
		ContactTitle:       " 商务负责人 ",
		ContactEmail:       " ops@example.com ",
		DefaultSettlement:  " 月结 30 天 ",
		DefaultPayment:     " 银行转账 ",
		InvoicePreference:  " 专用发票 ",
		MinConsumption:     " 1000 CNY / 月 ",
		DataTrainingPolicy: " 不用于训练 ",
		LogRetentionPolicy: " 留存 90 天 ",
		DefaultSLA:         " 99.9% ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "初禾资源（新注册演示）" || got.RegistrationNo != "DEMO-COMPANY-NEW" || got.Contact != "吴安" {
		t.Fatalf("fields not trimmed: %+v", got)
	}
	// 9 个自由文本字段只做 trim：不转大小写、不套邮箱正则（业务口径：邮箱暂不校验）。
	if got.ContactTitle != "商务负责人" || got.ContactEmail != "ops@example.com" {
		t.Fatalf("contact fields not trimmed: %+v", got)
	}
	if got.DefaultSettlement != "月结 30 天" || got.DefaultPayment != "银行转账" || got.InvoicePreference != "专用发票" ||
		got.MinConsumption != "1000 CNY / 月" || got.DataTrainingPolicy != "不用于训练" ||
		got.LogRetentionPolicy != "留存 90 天" || got.DefaultSLA != "99.9%" {
		t.Fatalf("commercial defaults not trimmed: %+v", got)
	}
}

func TestNormalizeAccountUpdateRejects(t *testing.T) {
	cases := []struct {
		name  string
		in    supplierAccountUpdateRequest
		want  apperr.Error
		label string
	}{
		{"empty-name", supplierAccountUpdateRequest{Name: "   "}, errAccountNameRequired, "企业资料必填"},
		{"long-name", supplierAccountUpdateRequest{Name: strings.Repeat("长", accountNameMaxLen+1)}, errAccountFieldTooLong, "字段长度超出上限"},
		{"long-reg-no", supplierAccountUpdateRequest{Name: "甲", RegistrationNo: strings.Repeat("A", accountRegNoMaxLen+1)}, errAccountFieldTooLong, "字段长度超出上限"},
		{"long-contact-title", supplierAccountUpdateRequest{Name: "甲", ContactTitle: strings.Repeat("职", accountTitleMaxLen+1)}, errAccountFieldTooLong, "字段长度超出上限"},
		{"long-contact-email", supplierAccountUpdateRequest{Name: "甲", ContactEmail: strings.Repeat("a", accountEmailMaxLen+1)}, errAccountFieldTooLong, "字段长度超出上限"},
		{"long-default-sla", supplierAccountUpdateRequest{Name: "甲", DefaultSLA: strings.Repeat("S", accountTermMaxLen+1)}, errAccountFieldTooLong, "字段长度超出上限"},
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

// 邮箱自由文本：格式非法也必须放行（业务口径：先不校验，暂定自由文本）。
func TestNormalizeAccountUpdateAcceptsAnyEmailText(t *testing.T) {
	for _, raw := range []string{"not-an-email", "a@b", "@", "  中文邮箱 ", ""} {
		if _, err := normalizeAccountUpdate(supplierAccountUpdateRequest{Name: "甲", ContactEmail: raw}); err != nil {
			t.Fatalf("email %q should be accepted as free text, got %v", raw, err)
		}
	}
}

func TestNormalizeAccountUpdateBoundaryLengths(t *testing.T) {
	// 边界值必须放行：128 / 64 / 64 是列宽，不是"小于"。
	if _, err := normalizeAccountUpdate(supplierAccountUpdateRequest{
		Name:               strings.Repeat("长", accountNameMaxLen),
		RegistrationNo:     strings.Repeat("A", accountRegNoMaxLen),
		Contact:            strings.Repeat("人", accountContactMaxLen),
		ContactTitle:       strings.Repeat("职", accountTitleMaxLen),
		ContactEmail:       strings.Repeat("a", accountEmailMaxLen),
		DefaultSettlement:  strings.Repeat("账", accountTermMaxLen),
		DefaultPayment:     strings.Repeat("付", accountTermMaxLen),
		InvoicePreference:  strings.Repeat("票", accountTermMaxLen),
		MinConsumption:     strings.Repeat("低", accountTermMaxLen),
		DataTrainingPolicy: strings.Repeat("训", accountTermMaxLen),
		LogRetentionPolicy: strings.Repeat("志", accountTermMaxLen),
		DefaultSLA:         strings.Repeat("S", accountTermMaxLen),
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

// 9 个新字段的 JSON 键必须与前端 SupplierAccountDTO 逐字对齐（camelCase）。
// 这类"后端 snake_case / 前端 camelCase"的错位不会编译报错，只会让页面永远显示空，
// 所以用一条断言把键名钉住。
func TestSupplierAccountDTOExposesCommercialDefaultsAsCamelCase(t *testing.T) {
	rec := &supplypg.AccountRecord{
		ID: 1001, Name: "澄海智算", Region: "CN", RegistrationNo: "DEMO", Contact: "顾宁", Phone: "13800000001",
		ContactTitle: "商务负责人", ContactEmail: "ops@example.com",
		DefaultSettlement: "月结 30 天", DefaultPayment: "银行转账", InvoicePreference: "专用发票",
		MinConsumption: "1000 CNY / 月", DataTrainingPolicy: "不用于训练",
		LogRetentionPolicy: "留存 90 天", DefaultSLA: "99.9%",
		UpdatedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(buildSupplierAccountDTO(rec))
	if err != nil {
		t.Fatalf("marshal dto: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal dto: %v", err)
	}
	want := map[string]string{
		"contactTitle": "商务负责人", "contactEmail": "ops@example.com",
		"defaultSettlement": "月结 30 天", "defaultPayment": "银行转账", "invoicePreference": "专用发票",
		"minConsumption": "1000 CNY / 月", "dataTrainingPolicy": "不用于训练",
		"logRetentionPolicy": "留存 90 天", "defaultSla": "99.9%",
	}
	for key, expected := range want {
		got, ok := payload[key]
		if !ok {
			t.Fatalf("DTO missing key %q: %s", key, string(raw))
		}
		if got != expected {
			t.Fatalf("DTO key %q = %v, want %q", key, got, expected)
		}
	}
	for _, leaked := range []string{"contact_title", "contact_email", "default_settlement", "default_sla"} {
		if _, ok := payload[leaked]; ok {
			t.Fatalf("snake_case key %q leaked into the portal DTO", leaked)
		}
	}
}

func TestSupplierAccountUpdateRequestBindsCamelCaseJSON(t *testing.T) {
	var req supplierAccountUpdateRequest
	body := `{"name":"甲","registrationNo":"R1","contact":"张三","contactTitle":"商务负责人","contactEmail":"ops@example.com",` +
		`"defaultSettlement":"T+30","defaultPayment":"银行转账","invoicePreference":"专用发票","minConsumption":"无",` +
		`"dataTrainingPolicy":"不用于训练","logRetentionPolicy":"不留存","defaultSla":"99.9%"}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("bind request: %v", err)
	}
	update, err := normalizeAccountUpdate(req)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if update.ContactTitle != "商务负责人" || update.ContactEmail != "ops@example.com" ||
		update.DefaultSettlement != "T+30" || update.DefaultPayment != "银行转账" ||
		update.InvoicePreference != "专用发票" || update.MinConsumption != "无" ||
		update.DataTrainingPolicy != "不用于训练" || update.LogRetentionPolicy != "不留存" || update.DefaultSLA != "99.9%" {
		t.Fatalf("fields not bound: %+v", update)
	}
}
