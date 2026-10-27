package supplypg

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// 身份链路集成校验（opt-in）：只在显式给了测试库 DSN 时跑。
//
//	SUPPLIER_ACCOUNT_TEST_DSN="postgres://<user>:<password>@localhost:5433/model_bss?sslmode=disable" go test ./internal/infra/supplypg -run TestAccountStoreIdentity -v
//
// 目的：000043 之后，两条账号链路必须收敛到同一个 supplier 行——
//   - account.owner_type='SUPPLIER' → owner_id 即 supplier.id（手机号登录/注册的新链路）
//   - account.owner_type='OPERATOR' → owner_id 是 subject_operator.id → supplier.subject_id（老链路）
func TestAccountStoreIdentityResolution(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_ACCOUNT_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUPPLIER_ACCOUNT_TEST_DSN to run identity resolution test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	store, err := NewAccountStore(db, "public")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := t.Context()

	var newOwnerSupplier, legacyAccount, legacyOperator int64
	// 取一个 owner_type='SUPPLIER' 的账号。
	_ = db.QueryRowContext(ctx, `
SELECT a.owner_id FROM account a JOIN supplier s ON s.id = a.owner_id
 WHERE a.portal_type = 'SUPPLIER' AND a.owner_type = 'SUPPLIER' LIMIT 1`).Scan(&newOwnerSupplier)
	// 取一个 owner_type='OPERATOR' 且已回填 subject_id 的老账号。
	_ = db.QueryRowContext(ctx, `
SELECT a.id, a.owner_id FROM account a
  JOIN subject_operator so ON so.id = a.owner_id
  JOIN supplier s ON s.subject_id = so.subject_id
 WHERE a.portal_type = 'SUPPLIER' AND a.owner_type = 'OPERATOR' LIMIT 1`).Scan(&legacyAccount, &legacyOperator)

	if newOwnerSupplier == 0 && legacyAccount == 0 {
		t.Skip("database has no supplier account to resolve")
	}

	if newOwnerSupplier > 0 {
		var accountID int64
		if err := db.QueryRowContext(ctx, `
SELECT a.id FROM account a WHERE a.owner_type = 'SUPPLIER' AND a.owner_id = $1 LIMIT 1`,
			newOwnerSupplier).Scan(&accountID); err != nil {
			t.Fatalf("load account of supplier %d: %v", newOwnerSupplier, err)
		}
		got, err := store.ResolveSupplierID(ctx, accountID, newOwnerSupplier)
		if err != nil {
			t.Fatalf("resolve new-link account: %v", err)
		}
		if got != newOwnerSupplier {
			t.Fatalf("new link: want supplier %d, got %d", newOwnerSupplier, got)
		}
		rec, err := store.Load(ctx, got)
		if err != nil {
			t.Fatalf("load account %d: %v", got, err)
		}
		if rec.Name == "" {
			t.Fatal("loaded account has empty name")
		}
	}

	if legacyAccount > 0 {
		got, err := store.ResolveSupplierID(ctx, legacyAccount, legacyOperator)
		if err != nil {
			t.Fatalf("resolve legacy account: %v", err)
		}
		var want int64
		if err := db.QueryRowContext(ctx, `
SELECT s.id FROM subject_operator so JOIN supplier s ON s.subject_id = so.subject_id WHERE so.id = $1`,
			legacyOperator).Scan(&want); err != nil {
			t.Fatalf("expected supplier for operator %d: %v", legacyOperator, err)
		}
		if got != want {
			t.Fatalf("legacy link: want supplier %d, got %d", want, got)
		}
	}
}
