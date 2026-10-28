package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/infra/supplyfiles"
	"model_bss/internal/repo"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type closeoutFixture struct{ Run, Supplier, TargetKey, TargetFile, RaceKey, RaceFile, CreditFile, FailedFile string }

// Explicitly marked daily data only. No new database/container or Inspector.
func TestACloseoutDaily44(t *testing.T) {
	mode, path := os.Getenv("BSS_A_CLOSEOUT_MODE"), os.Getenv("BSS_A_CLOSEOUT_MANIFEST")
	if mode == "" || path == "" {
		t.Skip("explicit daily closeout mode required")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load()
	must(err)
	if cfg.Database.DBName != "model_bss" {
		t.Fatal("daily model_bss required")
	}
	client, err := infradb.New(cfg.Database)
	must(err)
	defer client.Close()
	raw, err := client.DB.DB()
	must(err)
	ctx := context.Background()
	var version int
	var dirty bool
	must(raw.QueryRow(`SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty))
	if version != 45 || dirty {
		t.Fatal("45/clean required")
	}
	var f closeoutFixture
	if mode != "prepare" {
		b, err := os.ReadFile(path)
		must(err)
		must(json.Unmarshal(b, &f))
		if !strings.HasPrefix(f.Run, "QA-A-CLOSEOUT-") {
			t.Fatal("marked fixture required")
		}
		if id, err := strconv.ParseInt(f.Supplier, 10, 64); err != nil || id < 1 {
			t.Fatal("invalid supplier")
		}
	}
	trigger := "qa_a_closeout_fault_" + f.Supplier
	if mode == "fault-off" || mode == "cleanup" {
		_, err = raw.Exec(`DROP TRIGGER IF EXISTS ` + trigger + ` ON public.operation_log; DROP FUNCTION IF EXISTS public.` + trigger + `() `)
		must(err)
		if mode == "fault-off" {
			return
		}
	}
	if mode == "fault-on" {
		if strings.ContainsAny(f.Run, "'\\") {
			t.Fatal("invalid QA marker")
		}
		_, err = raw.Exec(fmt.Sprintf(`CREATE FUNCTION public.%s() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF ((NEW.entity_type='contract' AND NEW."after"->>'subject_id'='%s') OR (NEW.entity_type='supplier_qualification' AND NEW."after"->>'supplier_id'='%s')) AND NEW.reason='%s-fault' THEN RAISE EXCEPTION 'QA controlled attachment audit failure'; END IF; RETURN NEW; END$$; CREATE TRIGGER %s BEFORE INSERT ON public.operation_log FOR EACH ROW EXECUTE FUNCTION public.%s()`, trigger, f.Supplier, f.Supplier, f.Run, trigger, trigger))
		must(err)
		return
	}
	files, err := supplyfiles.NewF12AttachmentFileStore(os.Getenv("MODEL_BSS_F12_ATTACHMENT_STORE_DIR"))
	must(err)
	if mode == "cleanup" {
		refs := []string{}
		rows, err := raw.Query(`SELECT file_ref FROM supplier_qualification WHERE supplier_id=$1 AND file_ref IS NOT NULL`, f.Supplier)
		must(err)
		for rows.Next() {
			var ref string
			must(rows.Scan(&ref))
			refs = append(refs, ref)
		}
		must(rows.Err())
		rows.Close()
		tx, err := raw.BeginTx(ctx, nil)
		must(err)
		defer tx.Rollback()
		for _, q := range []string{
			`DELETE FROM operation_log WHERE (entity_type='contract' AND entity_id IN(SELECT id FROM contract WHERE subject_type='SUPPLIER' AND subject_id=$1)) OR (entity_type='credit_line' AND entity_id IN(SELECT id FROM credit_line WHERE subject_type='SUPPLIER' AND subject_id=$1)) OR (entity_type='supplier_qualification' AND entity_id IN(SELECT id FROM supplier_qualification WHERE supplier_id=$1)) OR (entity_type='supplier' AND entity_id=$1)`,
			`DELETE FROM contract WHERE subject_type='SUPPLIER' AND subject_id=$1`, `DELETE FROM credit_line WHERE subject_type='SUPPLIER' AND subject_id=$1`, `DELETE FROM supplier_qualification WHERE supplier_id=$1`, `DELETE FROM supplier_alias WHERE supplier_id=$1`, `DELETE FROM assignment_log WHERE entity_type='supplier' AND entity_id=$1`, `DELETE FROM supplier WHERE id=$1 AND name LIKE 'QA-A-CLOSEOUT-%'`,
		} {
			_, err = tx.ExecContext(ctx, q, f.Supplier)
			must(err)
		}
		must(tx.Commit())
		candidates, err := files.ListBefore(ctx, time.Now().Add(time.Minute))
		must(err)
		for _, item := range candidates {
			file, err := files.Open(ctx, item.ID)
			if err == nil && file.SubjectType == "SUPPLIER" && file.SubjectID == f.Supplier {
				must(files.Delete(ctx, item.ID))
			}
		}
		quals, err := supplyfiles.NewQualificationFileStore(os.Getenv("MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR"))
		must(err)
		for _, ref := range refs {
			must(quals.Delete(ctx, ref))
		}
		return
	}
	if mode != "prepare" {
		t.Fatal("unknown mode")
	}
	f.Run = "QA-A-CLOSEOUT-" + time.Now().Format("20060102-150405.000000")
	deps, err := newSupplyRuntimeDeps(client.DB, repo.NewIdempotencyRepo(client.DB))
	must(err)
	serial := 0
	write := func(fn func(context.Context, supply.WriteMeta) error) {
		serial++
		must(client.DB.Transaction(func(tx *gorm.DB) error {
			return fn(infradb.WithDB(ctx, tx), supply.WriteMeta{ActorID: "3", Reason: f.Run, RequestID: fmt.Sprintf("%s-%d", f.Run, serial)})
		}))
	}
	write(func(c context.Context, m supply.WriteMeta) error {
		s, err := deps.SupplierWriter.Create(c, m, supply.SupplierData{Name: f.Run, Region: "CN", SettlementCurrency: "CNY"})
		if err == nil {
			f.Supplier = s.ID
		}
		return err
	})
	initial, err := json.Marshal(f)
	must(err)
	must(os.WriteFile(path, initial, 0600))
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6s0sAAAAASUVORK5CYII=")
	must(err)
	create := func(name string) (string, string) {
		file, err := files.Save(ctx, "SUPPLIER", f.Supplier, "CONTRACT", "3", "contract.png", png)
		must(err)
		var key string
		write(func(c context.Context, m supply.WriteMeta) error {
			row, err := deps.F12Writer.CreateContract(c, m, supply.F12ContractWriteCommand{SubjectType: "SUPPLIER", SubjectID: f.Supplier, DocumentName: name, DocumentNo: name, FileID: file.ID, Reason: f.Run})
			if err == nil {
				key = row.ContractKey
			}
			return err
		})
		return key, file.ID
	}
	f.TargetKey, f.TargetFile = create(f.Run + " target")
	for i := 0; i < 21; i++ {
		create(fmt.Sprintf("%s filler%02d", f.Run, i))
	}
	f.RaceKey, f.RaceFile = create(f.Run + " race")
	credit, err := files.Save(ctx, "SUPPLIER", f.Supplier, "CREDIT", "3", "credit.png", png)
	must(err)
	f.CreditFile = credit.ID
	write(func(c context.Context, m supply.WriteMeta) error {
		_, err := deps.F12Writer.AddCreditVersion(c, m, 0, supply.F12CreditWriteCommand{SubjectType: "SUPPLIER", SubjectID: f.Supplier, Direction: "SUPPLIER_TO_US", Amount: "100.00", Currency: "CNY", DocumentName: f.Run + " credit", DocumentNo: "QA-CREDIT", FileID: credit.ID, Reason: f.Run})
		return err
	})
	b, err := json.Marshal(f)
	must(err)
	must(os.WriteFile(path, b, 0600))
}
