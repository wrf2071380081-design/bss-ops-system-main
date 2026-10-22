package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	appdb "model_bss/internal/infra/db"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGORMModelTxFromContextRejectsMissingTransaction(t *testing.T) {
	for _, ctx := range []context.Context{
		context.Background(),
		appdb.WithDB(context.Background(), &gorm.DB{}),
	} {
		if tx, err := GORMModelTxFromContext(ctx); tx != nil || !errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("missing public transaction returned tx=%v err=%v", tx, err)
		}
	}
}

func TestGORMModelTxFromContextJoinsIdempotencyTransaction(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated test database required")
	}

	raw, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(2)

	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: raw}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parent := orm.WithContext(ctx).Begin()
	if parent.Error != nil {
		t.Fatal(parent.Error)
	}
	defer parent.Rollback()

	requestContext := appdb.WithDB(ctx, parent)
	modelTx, err := GORMModelTxFromContext(requestContext)
	if err != nil {
		t.Fatal(err)
	}
	identity := fmt.Sprintf("tx-join-%d", time.Now().UnixNano())
	if _, err := modelTx.ExecContext(ctx, `INSERT INTO standard_model(company,name,official_id,model_type) VALUES('TxJoin','回滚验证',$1,'TEXT')`, identity); err != nil {
		t.Fatal(err)
	}
	if err := parent.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	var count int
	if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM standard_model WHERE company='TxJoin' AND official_id=$1`, identity).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("A write escaped the public idempotency transaction: rows=%d", count)
	}
}
