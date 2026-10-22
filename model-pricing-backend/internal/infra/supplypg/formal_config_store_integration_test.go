package supplypg

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

// Real authenticated encryption is a test provider, not a production key setup.
type configPGSealer struct{ aead cipher.AEAD }

func (s configPGSealer) Seal(ctx context.Context, text string, binding supply.SecretBinding) (supply.SecretEnvelope, error) {
	if err := ctx.Err(); err != nil {
		return supply.SecretEnvelope{}, err
	}
	aad, _ := json.Marshal(binding)
	return supply.SecretEnvelope{Ciphertext: s.aead.Seal(nil, nil, []byte(text), aad), KeyVersion: "test-config-key-v1"}, nil
}

type configPGURL struct{}

func (configPGURL) ValidateFormalURL(_ context.Context, value string) error {
	if value != "https://formal.example/v1" && value != "https://formal.example/v2" {
		return errors.New("URL rejected")
	}
	return nil
}

func TestFormalConfigServicePostgres34Native(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"supplier", "supplier_formal_config", "assignment_log", "operation_log", "resource", "standard_model", "standard_model_version"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		`ALTER TABLE pg_temp.resource ADD PRIMARY KEY(id),ADD UNIQUE(supplier_id,standard_model_id,region)`,
		`INSERT INTO pg_temp.supplier(id,name,region,created_by) OVERRIDING SYSTEM VALUE VALUES(21,'配置供应商','CN',7),(22,'其他供应商','OVERSEAS',8),(23,'历史无归属','CN',7)`,
		`INSERT INTO pg_temp.assignment_log(entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by) VALUES('supplier',21,'buyer_id',NULL,7,7),('supplier',22,'buyer_id',NULL,8,8)`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	failLog, dropLog := false, false
	store, err := NewFormalConfigStore("pg_temp", func(c context.Context, current *sql.Tx, e FormalConfigLog) error {
		if current != tx {
			t.Fatal("logger escaped shared transaction")
		}
		if dropLog {
			return nil
		}
		if failLog {
			e.RequestID = strings.Repeat("x", 65)
		}
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte("dedicated-test-key-never-production"))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		t.Fatal(err)
	}
	work := NewFormalConfigWork(store, func(context.Context) (*sql.Tx, error) { return tx, nil })
	now := time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC)
	service := supply.NewFormalConfigService(work, configPGSealer{aead}, configPGURL{}, func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "7", Reason: "外部配置已完成", RequestID: "pg-formal-config"}
	secret := "private-provider-key-for-test"
	input := supply.FormalConfigInput{Scope: "全部模型", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true}
	first, err := service.Create(ctx, meta, "21", input)
	if err != nil || first == nil {
		t.Fatalf("native config create unavailable: %v", err)
	}
	if first.Revision != 1 || first.ConfirmedRevision != 1 || first.ConfirmedBy != "7" || !first.HasAPIKey || first.APIKeyMask != "********" {
		t.Fatalf("bad safe config %+v", first)
	}
	config, err := store.Current(ctx, tx, "21", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	readStore, err := NewFormalConfigReadStore("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, store)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := supply.NewFormalConfigReader(readStore).List(ctx, "21")
	if err != nil || len(listed) != 1 || listed[0].ID != first.ID || !listed[0].HasAPIKey || listed[0].APIKeyMask != "********" {
		t.Fatalf("safe formal config list failed: %+v %v", listed, err)
	}
	listedOther, err := supply.NewFormalConfigReader(readStore).List(ctx, "22")
	if err != nil || len(listedOther) != 0 {
		t.Fatalf("other supplier list leaked configs: %+v %v", listedOther, err)
	}
	if got, err := supply.NewFormalConfigReader(readStore).List(ctx, "999"); got != nil || !errors.Is(err, supply.ErrNotFound) {
		t.Fatalf("missing supplier did not return 404 contract: %+v %v", got, err)
	}
	binding := supply.SecretBinding{Purpose: "SUPPLIER_FORMAL_API_KEY", SupplierID: "21", ConfigID: first.ID}
	aad, _ := json.Marshal(binding)
	plain, err := aead.Open(nil, nil, config.Secret.Ciphertext, aad)
	if err != nil || string(plain) != secret {
		t.Fatal("envelope round trip failed", err)
	}
	binding.ConfigID = "999"
	wrongAAD, _ := json.Marshal(binding)
	if _, err := aead.Open(nil, nil, config.Secret.Ciphertext, wrongAAD); err == nil {
		t.Fatal("test provider did not authenticate config identity")
	}
	var stored, note string
	if err := tx.QueryRowContext(ctx, `SELECT api_key_enc,COALESCE(note,'') FROM pg_temp.supplier_formal_config WHERE id=$1`, first.ID).Scan(&stored, &note); err != nil {
		t.Fatal(err)
	}
	if stored == secret || strings.Contains(stored, secret) || note != "" {
		t.Fatal("plaintext stored or note repurposed")
	}
	assertSafe := func() {
		t.Helper()
		var logs string
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg("after")::text,'') FROM pg_temp.operation_log WHERE entity_type='supplier_formal_config'`).Scan(&logs); err != nil {
			t.Fatal(err)
		}
		view, _ := json.Marshal(first)
		for _, bad := range []string{secret, stored, "test-config-key-v1", "ciphertext"} {
			if strings.Contains(logs, bad) || strings.Contains(string(view), bad) {
				t.Fatal("credential leaked into log/view")
			}
		}
	}
	assertSafe()
	if _, err := service.Create(ctx, supply.WriteMeta{ActorID: "8", Reason: meta.Reason, RequestID: meta.RequestID}, "21", input); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("wrong buyer accepted", err)
	}
	if _, err := service.Create(ctx, meta, "23", input); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("historical created_by treated as current buyer", err)
	}
	if _, err := store.Current(ctx, tx, "22", first.ID); !errors.Is(err, supply.ErrNotFound) {
		t.Fatal("cross supplier read", err)
	}
	input.APIKey = nil
	noop, err := service.Update(ctx, meta, "21", first.ID, 1, input)
	if err != nil || noop.Revision != 1 {
		t.Fatal("NOOP changed version", err)
	}
	now = now.Add(time.Second)
	input.Scope = "文本模型"
	input.BaseURL = "https://formal.example/v2"
	updated, err := service.Update(ctx, meta, "21", first.ID, 1, input)
	if err != nil || updated.Revision != 2 || updated.ConfirmedRevision != 2 {
		t.Fatal("update failed", err)
	}
	var preserved string
	if err := tx.QueryRowContext(ctx, `SELECT api_key_enc FROM pg_temp.supplier_formal_config WHERE id=$1`, first.ID).Scan(&preserved); err != nil || preserved != stored {
		t.Fatal("omitted key was replaced", err)
	}
	if _, err := service.Update(ctx, meta, "21", first.ID, 1, input); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatal("stale version accepted", err)
	}
	// Explicit transfer changes write eligibility, not the initial creator.
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.assignment_log(entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by) VALUES('supplier',21,'buyer_id',7,8,7)`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(ctx, meta, "21", first.ID, 2, input); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("former buyer still authorized", err)
	}
	meta.ActorID = "8"
	rotatedSecret := "rotated-private-test-key"
	input.APIKey = &rotatedSecret
	now = now.Add(time.Second)
	rotated, err := service.Update(ctx, meta, "21", first.ID, 2, input)
	if err != nil || rotated.Revision != 3 {
		t.Fatal("new buyer cannot rotate", err)
	}
	for _, failure := range []string{"log_sql", "empty_logger", "wire_drift", "bad_head", "outer_rollback", "legacy_resource"} {
		t.Run(failure, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, "SAVEPOINT config_failure"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				failLog = false
				dropLog = false
				if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT config_failure"); err != nil {
					t.Fatal(err)
				}
			}()
			switch failure {
			case "log_sql", "empty_logger":
				failLog = failure == "log_sql"
				dropLog = failure == "empty_logger"
				if got, err := service.Disable(ctx, meta, "21", first.ID, 3); err == nil || got != nil {
					t.Fatal("missing/failed log accepted")
				}
			case "wire_drift":
				if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.supplier_formal_config SET api_key_enc=$1 WHERE id=$2`, stored, first.ID); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Current(ctx, tx, "21", first.ID); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("old ciphertext paired with new revision", err)
				}
			case "bad_head":
				if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,"after") VALUES('supplier_formal_config',$1,'FORMAL_CONFIG_REVISION','{}')`, first.ID); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Current(ctx, tx, "21", first.ID); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("bad head fell back", err)
				}
			case "outer_rollback":
				if got, err := service.Disable(ctx, meta, "21", first.ID, 3); err != nil || got.Status != "INACTIVE" {
					t.Fatal("logical disable unavailable", err)
				}
			case "legacy_resource":
				if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.resource(standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,buyer_id) VALUES(31,21,'legacy','legacy','CN',0.7,7)`); err != nil {
					t.Fatal(err)
				}
				if got, err := service.Disable(ctx, meta, "21", first.ID, 3); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("missing resource control treated as unreferenced", err)
				}
			}
		})
		got, err := store.Current(ctx, tx, "21", first.ID)
		if err != nil || got.Status != "ACTIVE" || got.Revision != 3 {
			t.Fatal("failed operation escaped rollback", err)
		}
	}
	disabled, err := service.Disable(ctx, meta, "21", first.ID, 3)
	if err != nil || disabled.Status != "INACTIVE" || disabled.Revision != 4 {
		t.Fatal("disable failed", err)
	}
	if repeated, err := service.Disable(ctx, meta, "21", first.ID, 4); err != nil || repeated.Revision != 4 {
		t.Fatal("disable NOOP changed revision", err)
	}
	assertSafe()
}
