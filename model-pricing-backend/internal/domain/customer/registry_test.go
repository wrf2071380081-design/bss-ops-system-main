package customer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"model_bss/internal/domain/auth"
)

// 客户创建与基础档案的领域测试（G4-B1）。
//
// 覆盖：校验矩阵（每条一个用例）、默认值与归一化、重名透传、数据域判定。
// 「事务回滚不留孤儿主体」必须靠真实数据库证明，放在
// internal/infra/supplypg/customer_registry_store_integration_test.go。

// fakeRegistryStore 记录落到仓储的参数，并按需返回预设错误/结果。
type fakeRegistryStore struct {
	created      []CreateCustomerRecord
	createErr    error
	createResult *CustomerProfile

	profile   *CustomerProfile
	found     bool
	inScope   bool
	getErr    error
	scopeSeen OwnerScope

	updated   []UpdateCustomerProfileRecord
	updateErr error
}

func (f *fakeRegistryStore) UpdateCustomerProfile(_ context.Context, in UpdateCustomerProfileRecord) error {
	f.updated = append(f.updated, in)
	return f.updateErr
}

func (f *fakeRegistryStore) CreateCustomer(_ context.Context, in CreateCustomerRecord) (*CustomerProfile, error) {
	f.created = append(f.created, in)
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createResult != nil {
		return f.createResult, nil
	}
	return &CustomerProfile{ID: 1, LegalName: in.LegalName}, nil
}

func (f *fakeRegistryStore) GetCustomerProfile(_ context.Context, _ int64, scope OwnerScope) (*CustomerProfile, bool, bool, error) {
	f.scopeSeen = scope
	if f.getErr != nil {
		return nil, false, false, f.getErr
	}
	return f.profile, f.found, f.inScope, nil
}

// validCreateInput 是一份最小合法入参，各用例只改一个字段。
func validCreateInput() CreateCustomerInput {
	return CreateCustomerInput{
		LegalName:            "蓝海电商",
		Region:               "CN",
		SettlementCurrency:   "CNY",
		OwnerSalesOperatorID: 7,
	}
}

// TestRegistryCreateNormalizesInput 正常路径：默认值、去空格、税归一化、核验状态。
func TestRegistryCreateNormalizesInput(t *testing.T) {
	store := &fakeRegistryStore{}
	svc := NewRegistryService(store)

	profile, err := svc.Create(context.Background(), CreateCustomerInput{
		LegalName:            "  蓝海电商  ",
		USCC:                 " 91310000test001xyz ", // 小写 + 首尾空格，应被归一化
		Region:               " cn ",
		SettlementCurrency:   "cny",
		TaxRate:              "0.06",
		OwnerSalesOperatorID: 7,
		Reason:               "  新签客户  ",
	}, 42, "req-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if profile == nil || profile.ID == 0 {
		t.Fatal("应返回带 id 的档案")
	}
	if len(store.created) != 1 {
		t.Fatalf("期望落库 1 次，got %d", len(store.created))
	}
	record := store.created[0]
	if record.LegalName != "蓝海电商" {
		t.Fatalf("名称应去首尾空格，got %q", record.LegalName)
	}
	if record.SubjectType != SubjectTypeCompany {
		t.Fatalf("主体类型默认 COMPANY，got %q", record.SubjectType)
	}
	if record.Region != "CN" || record.SettlementCurrency != "CNY" {
		t.Fatalf("区域/币种应大写归一，got %q/%q", record.Region, record.SettlementCurrency)
	}
	if record.USCC == nil || *record.USCC != "91310000TEST001XYZ" {
		t.Fatalf("USCC 应大写去空格，got %v", record.USCC)
	}
	if record.TaxRate == nil || *record.TaxRate != "0.0600" {
		t.Fatalf("税率应归一化到 4 位小数，got %v", record.TaxRate)
	}
	if record.PaymentType != defaultPaymentType {
		t.Fatalf("结算方式默认 PREPAID，got %q", record.PaymentType)
	}
	if record.BillingCycle != defaultBillingCycle {
		t.Fatalf("账期默认 30，got %d", record.BillingCycle)
	}
	if record.VerificationStatus != "VERIFIED" {
		t.Fatalf("人工创建的客户应直接 VERIFIED，got %q", record.VerificationStatus)
	}
	if record.OperatorID != 42 || record.RequestID != "req-1" || record.Reason != "新签客户" {
		t.Fatalf("操作员/请求号/原因未透传：%+v", record)
	}
}

// TestRegistryCreateValidationMatrix 校验矩阵：每条一个用例，错误必须是可读中文。
func TestRegistryCreateValidationMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(in *CreateCustomerInput)
		want   error
	}{
		{"名称为空", func(in *CreateCustomerInput) { in.LegalName = "   " }, ErrCustomerNameRequired},
		{"名称超 128", func(in *CreateCustomerInput) { in.LegalName = strings.Repeat("名", 129) }, ErrCustomerNameTooLong},
		{"主体类型非法", func(in *CreateCustomerInput) { in.SubjectType = "GOV" }, ErrCustomerSubjectType},
		{"个人主体 v1 不支持", func(in *CreateCustomerInput) { in.SubjectType = "INDIVIDUAL" }, ErrCustomerIndividualUnsupported},
		{"USCC 17 位", func(in *CreateCustomerInput) { in.USCC = "91310000TEST001XY" }, ErrCustomerUSCCInvalid},
		{"USCC 含小写以外的非法字符", func(in *CreateCustomerInput) { in.USCC = "91310000TEST001-X" }, ErrCustomerUSCCInvalid},
		{"区域缺失", func(in *CreateCustomerInput) { in.Region = "" }, ErrCustomerRegionInvalid},
		{"区域非法", func(in *CreateCustomerInput) { in.Region = "APAC" }, ErrCustomerRegionInvalid},
		{"币种非法", func(in *CreateCustomerInput) { in.SettlementCurrency = "EUR" }, ErrCustomerCurrencyInvalid},
		{"币种位数不对", func(in *CreateCustomerInput) { in.SettlementCurrency = "CN" }, ErrCustomerCurrencyInvalid},
		{"税率为负", func(in *CreateCustomerInput) { in.TaxRate = "-0.1" }, ErrCustomerTaxRateInvalid},
		{"税率大于 1", func(in *CreateCustomerInput) { in.TaxRate = "1.01" }, ErrCustomerTaxRateInvalid},
		{"税率超过 4 位小数", func(in *CreateCustomerInput) { in.TaxRate = "0.12345" }, ErrCustomerTaxRateInvalid},
		{"税率非数字", func(in *CreateCustomerInput) { in.TaxRate = "六个点" }, ErrCustomerTaxRateInvalid},
		{"缺销售", func(in *CreateCustomerInput) { in.OwnerSalesOperatorID = 0 }, ErrCustomerOwnerRequired},
		{"销售为负", func(in *CreateCustomerInput) { in.OwnerSalesOperatorID = -3 }, ErrCustomerOwnerRequired},
		{"结算方式非法", func(in *CreateCustomerInput) { in.PaymentType = "WEEKLY" }, ErrCustomerPaymentType},
		{"账期 0 天", func(in *CreateCustomerInput) { in.BillingCycle = intPtr(0) }, ErrCustomerBillingCycle},
		{"账期 366 天", func(in *CreateCustomerInput) { in.BillingCycle = intPtr(366) }, ErrCustomerBillingCycle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeRegistryStore{}
			svc := NewRegistryService(store)
			in := validCreateInput()
			c.mutate(&in)
			_, err := svc.Create(context.Background(), in, 42, "req")
			if !errors.Is(err, c.want) {
				t.Fatalf("期望 %v，got %v", c.want, err)
			}
			if len(store.created) != 0 {
				t.Fatal("校验失败不得触发落库")
			}
			if err.Error() == "" {
				t.Fatal("错误文案不能为空")
			}
		})
	}
}

// TestRegistryCreateAcceptsValidBillingCycleEdges 边界值 1 与 365 必须放行。
func TestRegistryCreateAcceptsValidBillingCycleEdges(t *testing.T) {
	for _, cycle := range []int{1, 30, 365} {
		store := &fakeRegistryStore{}
		svc := NewRegistryService(store)
		in := validCreateInput()
		in.BillingCycle = &cycle
		if _, err := svc.Create(context.Background(), in, 42, "req"); err != nil {
			t.Fatalf("账期 %d 应放行：%v", cycle, err)
		}
		if store.created[0].BillingCycle != cycle {
			t.Fatalf("账期未透传：want %d got %d", cycle, store.created[0].BillingCycle)
		}
	}
}

// TestRegistryCreatePropagatesDuplicateName 重名由仓储判重后透传，且不得产生任何写入。
func TestRegistryCreatePropagatesDuplicateName(t *testing.T) {
	store := &fakeRegistryStore{createErr: ErrCustomerNameDuplicate}
	svc := NewRegistryService(store)
	_, err := svc.Create(context.Background(), validCreateInput(), 42, "req")
	if !errors.Is(err, ErrCustomerNameDuplicate) {
		t.Fatalf("got %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("期望透传 1 次（由仓储判重），got %d", len(store.created))
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("重名文案要能直接给用户看：%s", err.Error())
	}
}

// TestRegistryProfileScope 404 / 403 / 正常三分支。
func TestRegistryProfileScope(t *testing.T) {
	profile := &CustomerProfile{ID: 3, LegalName: "蓝海电商", OwnerSalesOperatorID: 7}

	t.Run("不存在 404", func(t *testing.T) {
		svc := NewRegistryService(&fakeRegistryStore{found: false})
		if _, err := svc.Profile(context.Background(), 3, OwnerScope{}); !errors.Is(err, ErrCustomerProfileNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("越权 403", func(t *testing.T) {
		svc := NewRegistryService(&fakeRegistryStore{profile: profile, found: true, inScope: false})
		if _, err := svc.Profile(context.Background(), 3, OwnerScope{}); !errors.Is(err, ErrCustomerOutOfScope) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("正常返回", func(t *testing.T) {
		store := &fakeRegistryStore{profile: profile, found: true, inScope: true}
		svc := NewRegistryService(store)
		got, err := svc.Profile(context.Background(), 3, OwnerScope{DataScope: auth.ScopeALL})
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if got.LegalName != "蓝海电商" {
			t.Fatalf("got %+v", got)
		}
		if store.scopeSeen.DataScope != auth.ScopeALL {
			t.Fatal("数据域应原样传给仓储")
		}
	})
	t.Run("非法 id", func(t *testing.T) {
		svc := NewRegistryService(&fakeRegistryStore{})
		if _, err := svc.Profile(context.Background(), 0, OwnerScope{}); !errors.Is(err, ErrCustomerProfileNotFound) {
			t.Fatalf("got %v", err)
		}
	})
}

// TestOwnerScopeAllowsOwner 数据域判定与客户列表口径一致。
func TestOwnerScopeAllowsOwner(t *testing.T) {
	cases := []struct {
		name  string
		scope OwnerScope
		owner int64
		want  bool
	}{
		{"SELF 命中自己", OwnerScope{DataScope: auth.ScopeSELF, StaffID: 7}, 7, true},
		{"SELF 看别人", OwnerScope{DataScope: auth.ScopeSELF, StaffID: 7}, 8, false},
		{"SELF 未注入 staff", OwnerScope{DataScope: auth.ScopeSELF}, 0, false},
		{"DEPT_SUB 命中自己", OwnerScope{DataScope: auth.ScopeDEPTSUB, StaffID: 7}, 7, true},
		{"DEPT_SUB 看别人", OwnerScope{DataScope: auth.ScopeDEPTSUB, StaffID: 7}, 8, false},
		{"ALL 全量可见", OwnerScope{DataScope: auth.ScopeALL}, 99, true},
		{"未注入数据域时全量可见（系统内部调用）", OwnerScope{}, 99, true},
	}
	for _, c := range cases {
		if got := c.scope.AllowsOwner(c.owner); got != c.want {
			t.Fatalf("%s：got %v want %v", c.name, got, c.want)
		}
	}
}

// TestRegistryWithoutStoreFailsLoudly 未装配仓储时必须显式报错。
func TestRegistryWithoutStoreFailsLoudly(t *testing.T) {
	var svc *RegistryService
	if _, err := svc.Create(context.Background(), validCreateInput(), 1, ""); !errors.Is(err, ErrCustomerCreateUnavailable) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewRegistryService(nil).Profile(context.Background(), 1, OwnerScope{}); !errors.Is(err, ErrCustomerCreateUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func intPtr(value int) *int { return &value }
