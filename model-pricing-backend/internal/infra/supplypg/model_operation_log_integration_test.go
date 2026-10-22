package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestAppendModelOperationLogUsesSharedTableAndTransaction(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	label := fmt.Sprintf("f1-log-%d", time.Now().UnixNano())
	var orgID, staffID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, orgID, label, fmt.Sprint(time.Now().UnixNano())).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	requestID := label + "-request"
	work, err := NewModelUnitOfWork("public", func(context.Context) (*sql.Tx, error) { return tx, nil }, AppendModelOperationLog)
	if err != nil {
		t.Fatal(err)
	}
	meta := supply.WriteMeta{ActorID: staffID, Reason: "资料核查", RequestID: requestID}
	created, err := supply.NewModelService(work, nil, func() time.Time { return time.Now().UTC() }).Create(ctx, meta, supply.ModelData{Name: label})
	if err != nil {
		t.Fatal(err)
	}
	var action, operatorID, operatorName, reason, gotRequestID, afterID string
	if err := tx.QueryRowContext(ctx, `SELECT action,operator_id::text,operator_name_snapshot,reason,request_id,"after"->>'id' FROM operation_log WHERE entity_type='standard_model' AND entity_id=$1`, created.ID).Scan(&action, &operatorID, &operatorName, &reason, &gotRequestID, &afterID); err != nil {
		t.Fatal(err)
	}
	if action != "CREATE" || operatorID != staffID || operatorName != label || reason != "资料核查" || gotRequestID != requestID || afterID != created.ID {
		t.Fatalf("unexpected shared operation record action=%q operator=%q reason=%q request=%q after_id=%q", action, operatorID, reason, gotRequestID, afterID)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var persisted int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM operation_log WHERE request_id=$1`, requestID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != 0 {
		t.Fatalf("operation log escaped the business transaction: rows=%d", persisted)
	}
}
