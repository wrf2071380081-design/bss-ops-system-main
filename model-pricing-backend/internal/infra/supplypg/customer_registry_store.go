package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"model_bss/internal/domain/customer"
)

// 客户创建与基础档案的仓储（G4-B1）。
//
// 事务纪律：`legal_subject` 与 `customer_profile` 必须在同一个事务里落库。
// 只有 profile 成功、审计也写成功，才提交；任何一步失败整体回滚，
// **绝不留下「主体表多一条、客户档案没建出来」的孤儿主体**——那会让重名判断永久误命中，
// 而且没有入口能清理。集成测试 TestCustomerRegistryStoreCreateRollsBackOnProfileFailure 专门咬住这点。

// CustomerRegistryStore 客户创建/档案仓储。
type CustomerRegistryStore struct {
	db     *sql.DB
	schema string
}

// 编译期断言。
var _ customer.CustomerRegistryStore = (*CustomerRegistryStore)(nil)

// NewCustomerRegistryStore 构造。
func NewCustomerRegistryStore(db *sql.DB, schema string) (*CustomerRegistryStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("customer registry store")
	}
	return &CustomerRegistryStore{db: db, schema: schema}, nil
}

func (s *CustomerRegistryStore) table(name string) string { return s.schema + "." + name }

// mapCustomerRegistryError 把 PG 错误翻译成领域错误。
//
// 23505 按约束名区分：只有 USCC 唯一索引是可预期的用户错误（同一张营业执照不能建两个客户主体），
// 其它唯一冲突（uk_customer_subject 等）说明是内部逻辑问题，不伪装成用户错误。
// 23503 只可能是 sales 归属写错（customer_profile_owner_sales_operator_id_fkey）——
// 这条映射同时也让「事务回滚」路径有明确文案。
func mapCustomerRegistryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return customer.ErrCustomerProfileNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "uk_subject_uscc" {
				return customer.ErrCustomerUSCCDuplicate
			}
			return err
		case "23503":
			if pgErr.ConstraintName == "customer_profile_owner_sales_operator_id_fkey" {
				return customer.ErrCustomerOwnerNotFound
			}
			return err
		case "23514":
			// CHECK 约束（region / contract_status 枚举）：说明上层校验漏了，按参数非法上报。
			return customer.ErrCustomerRegionInvalid
		case "42703", "42P01", "42804", "42846", "42883":
			return customer.ErrCustomerCreateUnavailable
		}
	}
	return err
}

// CreateCustomer 单事务创建主体 + 客户档案 + 审计。
func (s *CustomerRegistryStore) CreateCustomer(ctx context.Context, in customer.CreateCustomerRecord) (*customer.CustomerProfile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapCustomerRegistryError(err)
	}
	defer func() { _ = tx.Rollback() }()

	// 1) 重名判重（忽略大小写、忽略首尾空格）。
	//    只和「已经是客户」的主体比：同一家公司既做供应商又做客户是允许的，
	//    拿 supplier 的 legal_subject 来挡会让合法客户建不出来，且提示文案也对不上。
	//
	//    【已知取舍，已与产品确认接受，不要"顺手修干净"】
	//    这里是**查询判重**而不是唯一索引：并发下两个请求可能同时通过检查、各建一个同名客户。
	//    不加表达式唯一索引（lower(btrim(legal_name))）的原因是不动既有表结构与既有数据——
	//    库里已有种子/历史数据，加索引可能直接让迁移失败；而人工新建客户是低频操作，
	//    偶发同名的代价（两个同名客户，可人工合并）远小于迁移失败的风险。
	//    真要根治：新增独立迁移、先清洗历史重复数据、再建表达式唯一索引，属于独立需求。
	duplicateQuery := fmt.Sprintf(`
SELECT EXISTS (
    SELECT 1
      FROM %s ls
      JOIN %s cp ON cp.subject_id = ls.id
     WHERE lower(btrim(ls.legal_name)) = lower(btrim($1))
)`, s.table("legal_subject"), s.table("customer_profile"))
	var duplicate bool
	if err := tx.QueryRowContext(ctx, duplicateQuery, in.LegalName).Scan(&duplicate); err != nil {
		return nil, mapCustomerRegistryError(err)
	}
	if duplicate {
		return nil, customer.ErrCustomerNameDuplicate
	}

	// 2) 主体。
	//    verification_status 直接 VERIFIED：人工录入的客户已核对营业执照，
	//    落 PENDING 会让后续开通流程卡在「主体未核验」而无人可推进。
	subjectQuery := fmt.Sprintf(`
INSERT INTO %s (subject_type, legal_name, uscc, verification_status, request_id, created_by)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
RETURNING id`, s.table("legal_subject"))
	var subjectID int64
	if err := tx.QueryRowContext(ctx, subjectQuery,
		in.SubjectType, in.LegalName, nullableRegistryText(in.USCC), in.VerificationStatus, in.RequestID, in.OperatorID,
	).Scan(&subjectID); err != nil {
		return nil, mapCustomerRegistryError(err)
	}

	// 3) 客户档案。
	profileQuery := fmt.Sprintf(`
INSERT INTO %s (subject_id, region, settlement_currency, tax_rate, owner_sales_operator_id,
                payment_type, billing_cycle, request_id, created_by, updated_by)
VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, NULLIF($8, ''), $9, $9)
RETURNING id, credit_limit::text, credit_used::text, status, contract_status, created_at, updated_at`,
		s.table("customer_profile"))
	profile := customer.CustomerProfile{
		SubjectID:            subjectID,
		LegalName:            in.LegalName,
		SubjectType:          in.SubjectType,
		Region:               in.Region,
		SettlementCurrency:   in.SettlementCurrency,
		OwnerSalesOperatorID: in.OwnerSalesOperatorID,
		PaymentType:          in.PaymentType,
		BillingCycle:         in.BillingCycle,
	}
	if in.USCC != nil {
		profile.USCC = *in.USCC
	}
	if in.TaxRate != nil {
		profile.TaxRate = *in.TaxRate
	}
	if err := tx.QueryRowContext(ctx, profileQuery,
		subjectID, in.Region, in.SettlementCurrency, nullableRegistryText(in.TaxRate),
		in.OwnerSalesOperatorID, in.PaymentType, in.BillingCycle, in.RequestID, in.OperatorID,
	).Scan(&profile.ID, &profile.CreditLimit, &profile.CreditUsed, &profile.Status,
		&profile.ContractStatus, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
		return nil, mapCustomerRegistryError(err)
	}

	// 4) 审计（红线：状态变更必须留痕；before 为 null，after 记关键字段快照）。
	snapshot := map[string]any{
		"legal_name":              profile.LegalName,
		"subject_id":              subjectID,
		"subject_type":            profile.SubjectType,
		"uscc":                    profile.USCC,
		"region":                  profile.Region,
		"settlement_currency":     profile.SettlementCurrency,
		"tax_rate":                profile.TaxRate,
		"owner_sales_operator_id": profile.OwnerSalesOperatorID,
		"payment_type":            profile.PaymentType,
		"billing_cycle":           profile.BillingCycle,
		"verification_status":     in.VerificationStatus,
	}
	after, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	auditQuery := fmt.Sprintf(`
INSERT INTO %s (entity_type, entity_id, action, operator_id, operator_name_snapshot,
                "before", "after", reason, request_id, created_at)
VALUES ('customer_profile', $1, 'CREATE', $2,
        (SELECT name FROM %s WHERE id = $2), NULL, $3::jsonb, NULLIF($4, ''), NULLIF($5, ''), now())`,
		s.table("operation_log"), s.table("internal_staff"))
	if _, err := tx.ExecContext(ctx, auditQuery, profile.ID, in.OperatorID, string(after), in.Reason, in.RequestID); err != nil {
		return nil, mapCustomerRegistryError(err)
	}

	// 销售姓名回填（读一次，失败不影响创建结果：档案已经落库了）。
	ownerQuery := fmt.Sprintf(`SELECT name FROM %s WHERE id = $1`, s.table("internal_staff"))
	if err := tx.QueryRowContext(ctx, ownerQuery, in.OwnerSalesOperatorID).Scan(&profile.OwnerSalesName); err != nil {
		profile.OwnerSalesName = ""
	}

	if err := tx.Commit(); err != nil {
		return nil, mapCustomerRegistryError(err)
	}
	return &profile, nil
}

// GetCustomerProfile 读客户基础档案。
//
// 数据域口径与客户列表（api.CustomerWorkspaceRuntime.ListCustomers）一致：
// SELF / DEPT_SUB 只能看自己归属的客户，其余（ALL）不过滤。
// 这里把「是否存在」与「是否在域内」分开返回，让上层能区分 404 与 403。
func (s *CustomerRegistryStore) GetCustomerProfile(ctx context.Context, customerID int64, scope customer.OwnerScope) (*customer.CustomerProfile, bool, bool, error) {
	query := fmt.Sprintf(`
SELECT cp.id, cp.subject_id, ls.legal_name, COALESCE(ls.uscc, ''), ls.subject_type,
       cp.region, cp.settlement_currency, COALESCE(cp.tax_rate::text, ''), cp.contract_status,
       cp.owner_sales_operator_id, st.name, cp.payment_type, cp.billing_cycle,
       cp.credit_limit::text, cp.credit_used::text, cp.status, cp.created_at, cp.updated_at
  FROM %s cp
  JOIN %s ls ON ls.id = cp.subject_id
  JOIN %s st ON st.id = cp.owner_sales_operator_id
 WHERE cp.id = $1`, s.table("customer_profile"), s.table("legal_subject"), s.table("internal_staff"))

	var profile customer.CustomerProfile
	err := s.db.QueryRowContext(ctx, query, customerID).Scan(
		&profile.ID, &profile.SubjectID, &profile.LegalName, &profile.USCC, &profile.SubjectType,
		&profile.Region, &profile.SettlementCurrency, &profile.TaxRate, &profile.ContractStatus,
		&profile.OwnerSalesOperatorID, &profile.OwnerSalesName, &profile.PaymentType, &profile.BillingCycle,
		&profile.CreditLimit, &profile.CreditUsed, &profile.Status, &profile.CreatedAt, &profile.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, false, nil
		}
		return nil, false, false, mapCustomerRegistryError(err)
	}
	inScope := scope.AllowsOwner(profile.OwnerSalesOperatorID)
	return &profile, true, inScope, nil
}

// nullableString 把 *string 转成 SQL 友好参数（nil → NULL）。
func nullableRegistryText(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
