package customer

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"model_bss/internal/domain/auth"
)

// 客户创建与基础档案（G4-B1）。
//
// 背景：`customer_profile` / `legal_subject` 此前只有读路径（List / Transfer / 报价），
// 没有任何创建入口。G4 的「完整开通」需要先把客户建出来，否则流程没有对象可挂。
//
// 一条客户 = 两条记录，必须同事务：
//   - legal_subject(sid)      ：主体（名称 / USCC / 主体类型）
//   - customer_profile(id)    ：客户档案（区域 / 币种 / 税率 / 销售归属 / 账期）
//
// 为什么重名用「查询判重」而不是唯一索引（用户与 DBA 口径）：
// 库里已有种子与历史数据，legal_name 上直接加唯一索引会因历史重复数据让迁移失败；
// 而且重名判定是「忽略大小写 + 去首尾空格」的语义，普通唯一索引表达不了。
// 代价是并发下两个请求可能同时通过检查——这属于可接受的极小概率（人工建客户），
// 真要根治需要表达式唯一索引，本轮不做（不动既有表结构是硬要求）。

// 客户主体类型（legal_subject.subject_type）。
const (
	SubjectTypeCompany    = "COMPANY"
	SubjectTypeIndividual = "INDIVIDUAL"
)

// 人工创建的主体直接标记为已核验：这类客户是我们自己录入并核对过营业执照的，
// 不是供应商/客户自助注册，若落 PENDING 会让后续开通流程卡在「主体未核验」。
const verificationVerified = "VERIFIED"

var (
	ErrCustomerCreateUnavailable = errors.New("客户创建能力不可用")
	ErrCustomerNameRequired      = errors.New("请填写客户名称")
	ErrCustomerNameTooLong       = errors.New("客户名称不能超过 128 个字符")
	ErrCustomerSubjectType       = errors.New("主体类型只能是 COMPANY 或 INDIVIDUAL")
	// ErrCustomerIndividualUnsupported：v1 只支持企业主体。
	// 个人主体需要 mobile / real_name 才能落库（uk_subject_mobile 是按手机号建的唯一索引），
	// 当前入参没有这两个字段，宁可明确拒绝，也不要建出一个没有联系方式的半成品主体。
	ErrCustomerIndividualUnsupported = errors.New("暂不支持个人客户，请按企业主体创建")
	ErrCustomerUSCCInvalid           = errors.New("统一社会信用代码必须是 18 位大写字母或数字")
	ErrCustomerUSCCDuplicate         = errors.New("该统一社会信用代码已被其他主体占用")
	ErrCustomerRegionInvalid         = errors.New("请选择国内或海外主体")
	ErrCustomerCurrencyInvalid       = errors.New("结算币种仅支持 CNY 或 USD")
	ErrCustomerTaxRateInvalid        = errors.New("税率必须是 0 到 1 之间、最多 4 位小数的数字")
	ErrCustomerOwnerRequired         = errors.New("请选择归属销售")
	ErrCustomerOwnerNotFound         = errors.New("指定的归属销售不存在")
	ErrCustomerPaymentType           = errors.New("结算方式只能是 PREPAID / MONTHLY / PAYG")
	ErrCustomerBillingCycle          = errors.New("账期天数必须在 1 到 365 之间")
	ErrCustomerNameDuplicate         = errors.New("该客户名称已存在")
	ErrCustomerProfileNotFound       = errors.New("客户不存在")
)

const (
	customerNameMaxLen   = 128
	billingCycleMin      = 1
	billingCycleMax      = 365
	defaultBillingCycle  = 30
	defaultPaymentType   = "PREPAID"
	defaultSubjectType   = SubjectTypeCompany
	supportedCurrencyCNY = "CNY"
	supportedCurrencyUSD = "USD"
)

var (
	usccPattern     = regexp.MustCompile(`^[0-9A-Z]{18}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// 结算方式（customer_profile.payment_type，000002 DDL：PREPAID/MONTHLY/PAYG）。
var customerPaymentTypes = map[string]bool{"PREPAID": true, "MONTHLY": true, "PAYG": true}

// 主体区域（000037 ck_customer_region）。
var customerRegions = map[string]bool{"CN": true, "OVERSEAS": true}

// CreateCustomerInput 是创建客户的入参。
//
// TaxRate 用字符串而不是 float：与本仓库「金额/比例一律字符串传输」的口径一致，
// 也避免 0.06 被 JSON float 转成 0.059999999999999998。
type CreateCustomerInput struct {
	LegalName            string `json:"legal_name"`
	SubjectType          string `json:"subject_type"`
	USCC                 string `json:"uscc"`
	Region               string `json:"region"`
	SettlementCurrency   string `json:"settlement_currency"`
	TaxRate              string `json:"tax_rate"`
	OwnerSalesOperatorID int64  `json:"owner_sales_operator_id"`
	PaymentType          string `json:"payment_type"`
	// BillingCycle 用指针：不传 → 默认 30；传 0 必须报错（0 天账期没有业务含义），
	// 普通 int 无法区分「没传」与「传了 0」。
	BillingCycle *int   `json:"billing_cycle"`
	Reason       string `json:"reason"`
}

// CreateCustomerRecord 是校验并归一化后的落库参数（仓储不再做业务判断）。
type CreateCustomerRecord struct {
	LegalName            string
	SubjectType          string
	USCC                 *string // nil = 不填
	Region               string
	SettlementCurrency   string
	TaxRate              *string // nil = 未填写；否则已归一化到 4 位小数
	OwnerSalesOperatorID int64
	PaymentType          string
	BillingCycle         int
	VerificationStatus   string
	OperatorID           int64
	RequestID            string
	Reason               string
}

// CustomerProfile 是客户基础档案（客户详情页用）。
type CustomerProfile struct {
	ID                   int64     `json:"id"`
	SubjectID            int64     `json:"subject_id"`
	LegalName            string    `json:"legal_name"`
	USCC                 string    `json:"uscc"`
	SubjectType          string    `json:"subject_type"`
	Region               string    `json:"region"`
	SettlementCurrency   string    `json:"settlement_currency"`
	TaxRate              string    `json:"tax_rate"`
	ContractStatus       string    `json:"contract_status"`
	OwnerSalesOperatorID int64     `json:"owner_sales_operator_id"`
	OwnerSalesName       string    `json:"owner_sales_name"`
	PaymentType          string    `json:"payment_type"`
	BillingCycle         int       `json:"billing_cycle"`
	CreditLimit          string    `json:"credit_limit"`
	CreditUsed           string    `json:"credit_used"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// AllowsOwner 判断该数据域能否看到「归属为 ownerSalesOperatorID」的客户。
//
// 与客户列表 api.CustomerWorkspaceRuntime.ListCustomers 的过滤口径一一对应：
// SELF / DEPT_SUB 只能看自己归属的客户，ALL（或未注入，例如系统内部调用）全量可见。
// 放在领域层而不是各仓储里，是为了避免「列表一套判定、详情另一套」的漂移。
func (s OwnerScope) AllowsOwner(ownerSalesOperatorID int64) bool {
	switch s.DataScope {
	case auth.ScopeSELF, auth.ScopeDEPTSUB:
		return s.StaffID > 0 && s.StaffID == ownerSalesOperatorID
	default:
		return true
	}
}

// CustomerRegistryStore 是客户创建/档案的持久化端口。
// GetCustomerProfile 返回 (profile, found, inScope, err)：
// found=false → 404；found=true 但 inScope=false → 403（与列表的 owner 过滤口径一致）。
type CustomerRegistryStore interface {
	CreateCustomer(ctx context.Context, in CreateCustomerRecord) (*CustomerProfile, error)
	GetCustomerProfile(ctx context.Context, customerID int64, scope OwnerScope) (*CustomerProfile, bool, bool, error)
}

// RegistryService 客户创建与基础档案服务。
type RegistryService struct {
	store CustomerRegistryStore
}

// NewRegistryService 构造。
func NewRegistryService(store CustomerRegistryStore) *RegistryService {
	return &RegistryService{store: store}
}

// Create 校验 + 归一化后落库（legal_subject 与 customer_profile 同事务，详见仓储）。
func (s *RegistryService) Create(ctx context.Context, in CreateCustomerInput, operatorID int64, requestID string) (*CustomerProfile, error) {
	if s == nil || s.store == nil {
		return nil, ErrCustomerCreateUnavailable
	}
	record, err := normalizeCreateInput(in, operatorID, requestID)
	if err != nil {
		return nil, err
	}
	return s.store.CreateCustomer(ctx, *record)
}

// Profile 读客户基础档案，带数据域校验。
func (s *RegistryService) Profile(ctx context.Context, customerID int64, scope OwnerScope) (*CustomerProfile, error) {
	if s == nil || s.store == nil {
		return nil, ErrCustomerCreateUnavailable
	}
	if customerID <= 0 {
		return nil, ErrCustomerProfileNotFound
	}
	profile, found, inScope, err := s.store.GetCustomerProfile(ctx, customerID, scope)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCustomerProfileNotFound
	}
	if !inScope {
		// 与客户列表同一口径：SELF/DEPT_SUB 只能看自己归属的客户。
		return nil, ErrCustomerOutOfScope
	}
	return profile, nil
}

// normalizeCreateInput 是校验矩阵的唯一实现处（接口层不再重复校验）。
//
// 顺序：先做「不合法就一定拒绝」的格式校验，最后才归一化默认值——
// 这样错误文案能精确指向字段，而不是笼统的「参数错误」。
func normalizeCreateInput(in CreateCustomerInput, operatorID int64, requestID string) (*CreateCustomerRecord, error) {
	name := strings.TrimSpace(in.LegalName)
	if name == "" {
		return nil, ErrCustomerNameRequired
	}
	if utf8.RuneCountInString(name) > customerNameMaxLen {
		return nil, ErrCustomerNameTooLong
	}

	subjectType := strings.ToUpper(strings.TrimSpace(in.SubjectType))
	if subjectType == "" {
		subjectType = defaultSubjectType
	}
	if subjectType != SubjectTypeCompany && subjectType != SubjectTypeIndividual {
		return nil, ErrCustomerSubjectType
	}
	// v1 只放行企业主体（口径已定）：INDIVIDUAL 缺 mobile/real_name 入参，
	// 建出来就是个没有联系方式的孤儿主体，等需求真的出现再把字段补齐一起开放。
	if subjectType != SubjectTypeCompany {
		return nil, ErrCustomerIndividualUnsupported
	}

	var uscc *string
	if trimmed := strings.ToUpper(strings.TrimSpace(in.USCC)); trimmed != "" {
		if !usccPattern.MatchString(trimmed) {
			return nil, ErrCustomerUSCCInvalid
		}
		uscc = &trimmed
	}

	region := strings.ToUpper(strings.TrimSpace(in.Region))
	if !customerRegions[region] {
		return nil, ErrCustomerRegionInvalid
	}

	currency := strings.ToUpper(strings.TrimSpace(in.SettlementCurrency))
	if !currencyPattern.MatchString(currency) || (currency != supportedCurrencyCNY && currency != supportedCurrencyUSD) {
		return nil, ErrCustomerCurrencyInvalid
	}

	var taxRate *string
	if trimmed := strings.TrimSpace(in.TaxRate); trimmed != "" {
		parsed, err := decimal.NewFromString(trimmed)
		if err != nil || parsed.IsNegative() || parsed.GreaterThan(decimal.NewFromInt(1)) || parsed.Exponent() < -4 {
			return nil, ErrCustomerTaxRateInvalid
		}
		// 归一化到 numeric(6,4) 口径，避免 "0.06" 与 "0.0600" 两种写法在库里表现不一致。
		normalized := parsed.StringFixed(4)
		taxRate = &normalized
	}

	if in.OwnerSalesOperatorID <= 0 {
		return nil, ErrCustomerOwnerRequired
	}

	paymentType := strings.ToUpper(strings.TrimSpace(in.PaymentType))
	if paymentType == "" {
		paymentType = defaultPaymentType
	}
	if !customerPaymentTypes[paymentType] {
		return nil, ErrCustomerPaymentType
	}

	cycle := defaultBillingCycle
	if in.BillingCycle != nil {
		cycle = *in.BillingCycle
	}
	if cycle < billingCycleMin || cycle > billingCycleMax {
		return nil, ErrCustomerBillingCycle
	}

	return &CreateCustomerRecord{
		LegalName:            name,
		SubjectType:          subjectType,
		USCC:                 uscc,
		Region:               region,
		SettlementCurrency:   currency,
		TaxRate:              taxRate,
		OwnerSalesOperatorID: in.OwnerSalesOperatorID,
		PaymentType:          paymentType,
		BillingCycle:         cycle,
		VerificationStatus:   verificationVerified,
		OperatorID:           operatorID,
		RequestID:            requestID,
		Reason:               strings.TrimSpace(in.Reason),
	}, nil
}
