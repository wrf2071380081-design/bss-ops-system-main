package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"model_bss/internal/domain/auth"
	"model_bss/internal/domain/customer"
)

// 客户创建与基础档案的仓储集成校验（opt-in）：只在显式给了测试库 DSN 时跑。
//
//	CUSTOMER_REGISTRY_TEST_DSN="postgres://<user>:<password>@localhost:5433/model_bss?sslmode=disable" \
//	  go test ./internal/infra/supplypg -run TestCustomerRegistryStore -v
//
// 这些是「只有真库能证明」的点：
//   - legal_subject + customer_profile + operation_log 三写同事务，任一步失败**不留孤儿主体**；
//   - 重名判定（忽略大小写与首尾空格）真的拦得住，且拦下时不产生任何行；
//   - USCC 唯一索引（uk_subject_uscc）冲突被翻译成可读的 409 语义；
//   - 数据域（SELF / ALL）在详情读上真的生效。
//
// 隔离纪律：每次运行用带纳秒时间戳的客户名做前缀，清理按该前缀扫，绝不批量删真实客户。

func openRegistryTestStore(t *testing.T) (*CustomerRegistryStore, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("CUSTOMER_REGISTRY_TEST_DSN")
	if dsn == "" {
		t.Skip("set CUSTOMER_REGISTRY_TEST_DSN to run customer registry integration tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store, err := NewCustomerRegistryStore(db, "public")
	if err != nil {
		_ = db.Close()
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return store, db
}

type registryFixture struct {
	ownerID   int64
	ownerName string
	prefix    string
}

// newRegistryFixture 取一个 ACTIVE 的内部员工做销售归属，并注册按前缀清理。
func newRegistryFixture(t *testing.T, db *sql.DB) *registryFixture {
	t.Helper()
	ctx := context.Background()
	f := &registryFixture{prefix: fmt.Sprintf("QA-G4B1-%d", time.Now().UnixNano())}
	if err := db.QueryRowContext(ctx,
		`SELECT id, name FROM public.internal_staff WHERE status = 'ACTIVE' ORDER BY id LIMIT 1`).
		Scan(&f.ownerID, &f.ownerName); err != nil {
		t.Skipf("database has no ACTIVE internal_staff: %v", err)
	}
	t.Cleanup(func() {
		like := f.prefix + "%"
		// 审计先删（无外键但有 entity_id 关联），再删档案，最后删主体。
		// 最后一条按名字删 legal_subject 是为了兜住「回滚失效」时可能留下的孤儿主体，
		// 否则跑一次坏代码就会让后续运行的重名判断永久误命中。
		if _, err := db.ExecContext(context.Background(), `
DELETE FROM public.operation_log
 WHERE entity_type = 'customer_profile'
   AND entity_id IN (SELECT cp.id FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id = cp.subject_id WHERE ls.legal_name LIKE $1)`, like); err != nil {
			t.Logf("cleanup operation_log failed: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `
DELETE FROM public.customer_profile
 WHERE id IN (SELECT cp.id FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id = cp.subject_id WHERE ls.legal_name LIKE $1)`, like); err != nil {
			t.Logf("cleanup customer_profile failed: %v", err)
		}
		if _, err := db.ExecContext(context.Background(),
			`DELETE FROM public.legal_subject WHERE legal_name LIKE $1`, like); err != nil {
			t.Logf("cleanup legal_subject failed: %v", err)
		}
	})
	return f
}

func (f *registryFixture) name(suffix string) string { return f.prefix + "-" + suffix }

func (f *registryFixture) record(name string) customer.CreateCustomerRecord {
	return customer.CreateCustomerRecord{
		LegalName:            name,
		SubjectType:          customer.SubjectTypeCompany,
		Region:               "CN",
		SettlementCurrency:   "CNY",
		OwnerSalesOperatorID: f.ownerID,
		PaymentType:          "PREPAID",
		BillingCycle:         30,
		VerificationStatus:   "VERIFIED",
		OperatorID:           f.ownerID,
		RequestID:            "qa-g4b1",
		Reason:               "集成测试建客户",
	}
}

// countSubjects 统计名字完全匹配（忽略大小写）的主体行数。
func countSubjects(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM public.legal_subject WHERE lower(btrim(legal_name)) = lower(btrim($1))`, name).Scan(&count); err != nil {
		t.Fatalf("count legal_subject: %v", err)
	}
	return count
}

// TestCustomerRegistryStoreCreateAndRead 正常创建：三写同事务 + 详情读 + 数据域。
func TestCustomerRegistryStoreCreateAndRead(t *testing.T) {
	store, db := openRegistryTestStore(t)
	f := newRegistryFixture(t, db)
	ctx := context.Background()

	name := f.name("ok")
	record := f.record(name)
	taxRate := "0.0600"
	record.TaxRate = &taxRate

	profile, err := store.CreateCustomer(ctx, record)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if profile.ID <= 0 || profile.SubjectID <= 0 {
		t.Fatalf("应返回 id 与 subject_id：%+v", profile)
	}
	if profile.LegalName != name || profile.Region != "CN" || profile.SettlementCurrency != "CNY" {
		t.Fatalf("档案字段不符：%+v", profile)
	}
	if profile.TaxRate != "0.0600" {
		t.Fatalf("税率应原样落 4 位小数，got %q", profile.TaxRate)
	}
	if profile.Status != "ACTIVE" || profile.ContractStatus != "待记录" {
		t.Fatalf("默认状态不符：%+v", profile)
	}
	if profile.OwnerSalesName != f.ownerName {
		t.Fatalf("销售姓名未回填：want %q got %q", f.ownerName, profile.OwnerSalesName)
	}

	// 主体：人工创建直接 VERIFIED。
	var verification string
	var uscc sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT verification_status, uscc FROM public.legal_subject WHERE id = $1`, profile.SubjectID).
		Scan(&verification, &uscc); err != nil {
		t.Fatalf("load legal_subject: %v", err)
	}
	if verification != "VERIFIED" {
		t.Fatalf("人工创建的主体应为 VERIFIED，got %q", verification)
	}
	if uscc.Valid {
		t.Fatalf("未填 USCC 时应为 NULL，got %q", uscc.String)
	}

	// 审计：有且仅有一条 CREATE。
	var auditCount int
	var action, requestID, afterName string
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE action = 'CREATE'), max(action), max(request_id), max("after"->>'legal_name')
  FROM public.operation_log WHERE entity_type = 'customer_profile' AND entity_id = $1`, profile.ID).
		Scan(&auditCount, &action, &requestID, &afterName); err != nil {
		t.Fatalf("load operation_log: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("审计应有且仅有 1 条 CREATE，got %d", auditCount)
	}
	if action != "CREATE" || requestID != "qa-g4b1" || afterName != name {
		t.Fatalf("审计内容不符：action=%q request_id=%q after.legal_name=%q", action, requestID, afterName)
	}

	// 详情读：
	found, inScope := false, false
	read, found, inScope, err := store.GetCustomerProfile(ctx, profile.ID, customer.OwnerScope{DataScope: auth.ScopeALL})
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if !found || !inScope || read == nil || read.LegalName != name {
		t.Fatalf("ALL 数据域应可见：found=%v inScope=%v profile=%+v", found, inScope, read)
	}
	if read.USCC != "" || read.TaxRate != "0.0600" {
		t.Fatalf("详情字段不符：%+v", read)
	}

	// SELF：只有归属人能看。
	_, found, inScope, err = store.GetCustomerProfile(ctx, profile.ID, customer.OwnerScope{DataScope: auth.ScopeSELF, StaffID: f.ownerID})
	if err != nil || !found || !inScope {
		t.Fatalf("归属人应可见：err=%v found=%v inScope=%v", err, found, inScope)
	}
	_, found, inScope, err = store.GetCustomerProfile(ctx, profile.ID, customer.OwnerScope{DataScope: auth.ScopeSELF, StaffID: f.ownerID + 999999})
	if err != nil || !found || inScope {
		t.Fatalf("非归属人应不可见：err=%v found=%v inScope=%v", err, found, inScope)
	}

	// 不存在的客户：found=false。
	_, found, _, err = store.GetCustomerProfile(ctx, 9223372036854775806, customer.OwnerScope{DataScope: auth.ScopeALL})
	if err != nil || found {
		t.Fatalf("不存在的客户应 found=false：err=%v found=%v", err, found)
	}
}

// TestCustomerRegistryStoreDuplicateName 重名（忽略大小写/空格）必须 409 语义，且不产生任何行。
func TestCustomerRegistryStoreDuplicateName(t *testing.T) {
	store, db := openRegistryTestStore(t)
	f := newRegistryFixture(t, db)
	ctx := context.Background()

	name := f.name("dup")
	if _, err := store.CreateCustomer(ctx, f.record(name)); err != nil {
		t.Fatalf("首次创建失败：%v", err)
	}
	// 大小写 + 首尾空格变化后仍应判为同一个名字。
	if _, err := store.CreateCustomer(ctx, f.record("  "+strings.ToLower(name)+"  ")); !errors.Is(err, customer.ErrCustomerNameDuplicate) {
		t.Fatalf("同名应返回 ErrCustomerNameDuplicate，got %v", err)
	}
	if got := countSubjects(t, db, name); got != 1 {
		t.Fatalf("重名被拦下后不应新增主体，期望 1 行，got %d", got)
	}
}

// TestCustomerRegistryStoreRollsBackWhenProfileInsertFails 最关键的一条：
// profile 插入失败（销售不存在触发外键）时，前面插入的 legal_subject 必须一起回滚。
func TestCustomerRegistryStoreRollsBackWhenProfileInsertFails(t *testing.T) {
	store, db := openRegistryTestStore(t)
	f := newRegistryFixture(t, db)
	ctx := context.Background()

	name := f.name("rollback")
	record := f.record(name)
	record.OwnerSalesOperatorID = 9223372036854775806 // 不存在的内部员工

	if _, err := store.CreateCustomer(ctx, record); !errors.Is(err, customer.ErrCustomerOwnerNotFound) {
		t.Fatalf("销售不存在应返回 ErrCustomerOwnerNotFound，got %v", err)
	}
	if got := countSubjects(t, db, name); got != 0 {
		t.Fatalf("profile 失败必须整体回滚：期望 legal_subject 0 行，got %d（孤儿主体会让重名判断永久误命中）", got)
	}
	var profiles int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM public.customer_profile cp JOIN public.legal_subject ls ON ls.id = cp.subject_id
 WHERE lower(btrim(ls.legal_name)) = lower(btrim($1))`, name).Scan(&profiles); err != nil {
		t.Fatalf("count customer_profile: %v", err)
	}
	if profiles != 0 {
		t.Fatalf("不应留下客户档案行，got %d", profiles)
	}
}

// TestCustomerRegistryStoreDuplicateUSCC 同一 USCC 不能建两个客户主体，且失败不留孤儿。
func TestCustomerRegistryStoreDuplicateUSCC(t *testing.T) {
	store, db := openRegistryTestStore(t)
	f := newRegistryFixture(t, db)
	ctx := context.Background()

	// 18 位大写字母数字，取纳秒尾数保证本次运行唯一。
	uscc := fmt.Sprintf("QA%016d", time.Now().UnixNano()%10000000000000000)
	uscc = strings.ToUpper(uscc)
	if len(uscc) != 18 {
		t.Fatalf("构造的 USCC 长度不对：%q", uscc)
	}

	first := f.record(f.name("uscc-a"))
	first.USCC = &uscc
	if _, err := store.CreateCustomer(ctx, first); err != nil {
		t.Fatalf("首次创建失败：%v", err)
	}

	secondName := f.name("uscc-b")
	second := f.record(secondName)
	second.USCC = &uscc
	if _, err := store.CreateCustomer(ctx, second); !errors.Is(err, customer.ErrCustomerUSCCDuplicate) {
		t.Fatalf("同 USCC 应返回 ErrCustomerUSCCDuplicate，got %v", err)
	}
	if got := countSubjects(t, db, secondName); got != 0 {
		t.Fatalf("USCC 冲突必须整体回滚：期望第二个名字 0 行主体，got %d", got)
	}
	if got := countSubjects(t, db, first.LegalName); got != 1 {
		t.Fatalf("首个客户不应受影响，期望 1 行，got %d", got)
	}
}
