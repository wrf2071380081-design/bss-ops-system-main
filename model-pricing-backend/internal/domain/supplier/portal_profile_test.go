package supplier

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ------------------------------------------------------------
// fake store
// ------------------------------------------------------------

type fakePortalStore struct {
	row *PortalProfileRow
	err error
	// 观测点：确认 service 用的是「登录态解析出来的 supplierID」，不是请求参数。
	gotID int64
}

func (f *fakePortalStore) GetPortalProfile(_ context.Context, supplierID int64) (*PortalProfileRow, error) {
	f.gotID = supplierID
	return f.row, f.err
}

func fullRow() *PortalProfileRow {
	return &PortalProfileRow{
		ID:                 1,
		LegalName:          "供应商甲",
		USCC:               "91310000MA1FL0Q23X",
		Status:             "ACTIVE",
		QualStatus:         "VALID",
		SettleType:         "PREPAID",
		BillingCycle:       30,
		SettlementCurrency: "CNY",
		ContactName:        "甲操作员",
		ContactMobile:      "13800000001",
		UpdatedAt:          time.Date(2026, 9, 9, 16, 20, 0, 0, time.UTC),
	}
}

// ------------------------------------------------------------
// service
// ------------------------------------------------------------

// TestPortalProfileGet_MasksSensitiveFields 脱敏是本批的核心安全要求：
// USCC 与手机号必须打星，`pkg/fieldmask` 做不了（它只删键）。
func TestPortalProfileGet_MasksSensitiveFields(t *testing.T) {
	st := &fakePortalStore{row: fullRow()}
	got, err := NewPortalProfileService(st).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.RegistrationNoMasked != "9131************3X" {
		t.Fatalf("USCC 脱敏错误（应保留前 4 后 2、中间按长度打星）：%q", got.RegistrationNoMasked)
	}
	if got.ContactPhoneMasked != "138****0001" {
		t.Fatalf("手机号脱敏错误：%q", got.ContactPhoneMasked)
	}
	// 明文绝不能出现在响应里（防回归：别哪天把原值直接塞进 Masked 字段）
	if got.RegistrationNoMasked == fullRow().USCC || got.ContactPhoneMasked == fullRow().ContactMobile {
		t.Fatal("脱敏字段回退成了明文")
	}
}

// TestPortalProfileGet_DerivesPaymentTerms paymentTerms 是**推导**字段：
// 库里没有 payment_terms 列，只能由 settle_type + billing_cycle 算出来。
func TestPortalProfileGet_DerivesPaymentTerms(t *testing.T) {
	cases := []struct {
		settleType string
		cycle      int
		want       string
	}{
		{"PREPAID", 30, "预付"},
		{"MONTHLY", 30, "月结 30 天"},
		{"MONTHLY", 15, "月结 15 天"},
		{"WEIRD_NEW_TYPE", 7, "WEIRD_NEW_TYPE"}, // 未知值原样透传，不猜
	}
	for _, c := range cases {
		row := fullRow()
		row.SettleType, row.BillingCycle = c.settleType, c.cycle
		got, err := NewPortalProfileService(&fakePortalStore{row: row}).Get(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.PaymentTerms != c.want {
			t.Fatalf("PaymentTermsOf(%q,%d)=%q，期望 %q", c.settleType, c.cycle, got.PaymentTerms, c.want)
		}
	}
}

// TestPortalProfileGet_PassesResolvedSupplierID 传给 store 的必须是登录态解析出的 id。
func TestPortalProfileGet_PassesResolvedSupplierID(t *testing.T) {
	st := &fakePortalStore{row: fullRow()}
	if _, err := NewPortalProfileService(st).Get(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if st.gotID != 42 {
		t.Fatalf("应把解析出的 supplierID=42 传给 store，实际 %d", st.gotID)
	}
}

// TestPortalProfileGet_NotFoundReturnsNilNil 档案不存在 → (nil, nil)，由 handler 转 404，
// 不能当成系统错误（否则会返回 500）。
func TestPortalProfileGet_NotFoundReturnsNilNil(t *testing.T) {
	got, err := NewPortalProfileService(&fakePortalStore{row: nil}).Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("不存在应返回 nil error，实际 %v", err)
	}
	if got != nil {
		t.Fatalf("不存在应返回 nil profile，实际 %+v", got)
	}
}

func TestPortalProfileGet_StoreErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	if _, err := NewPortalProfileService(&fakePortalStore{err: want}).Get(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("仓储错误应原样上抛，实际 %v", err)
	}
}

// TestPortalProfileGet_NoPrimaryOperator 没有主操作员时联系人两列应为空串，
// 档案本身仍要能返回（LEFT JOIN，不能整行丢失）。
func TestPortalProfileGet_NoPrimaryOperator(t *testing.T) {
	row := fullRow()
	row.ContactName, row.ContactMobile = "", ""
	got, err := NewPortalProfileService(&fakePortalStore{row: row}).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContactName != "" || got.ContactPhoneMasked != "" {
		t.Fatalf("无主操作员时联系人应为空串，实际 %q / %q", got.ContactName, got.ContactPhoneMasked)
	}
	if got.LegalName != "供应商甲" {
		t.Fatalf("档案整行不应该丢：%q", got.LegalName)
	}
}

// ------------------------------------------------------------
// 脱敏函数
// ------------------------------------------------------------

func TestMaskPhone(t *testing.T) {
	cases := map[string]string{
		"13800000001":   "138****0001",
		"13912345678":   "139****5678",
		"123456":        "***",         // 不足 7 位
		"":              "",            // 空值不输出 ***
		" 13800000001 ": "138****0001", // 前后空格先 trim（trim 后长度按 11 算）
	}
	for in, want := range cases {
		if got := MaskPhone(in); got != want {
			t.Fatalf("MaskPhone(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestMaskRegistrationNo(t *testing.T) {
	// 18 位 USCC：前 4 + 12 星 + 后 2 = 18 字符
	got := MaskRegistrationNo("91310000MA1FL0Q23X")
	if got != "9131************3X" {
		t.Fatalf("USCC 脱敏错误：%q", got)
	}
	if len(got) != 18 {
		t.Fatalf("脱敏后长度应与原 USCC 一致（18），实际 %d", len(got))
	}
	if got == "91310000MA1FL0Q23X" {
		t.Fatal("脱敏后不应等于明文")
	}
	// 短值整体打星，避免几乎原样透出
	if got := MaskRegistrationNo("9131"); got != "****" {
		t.Fatalf("短值应整体打星，实际 %q", got)
	}
	if got := MaskRegistrationNo(""); got != "" {
		t.Fatalf("空值应返回空串，实际 %q", got)
	}
}
