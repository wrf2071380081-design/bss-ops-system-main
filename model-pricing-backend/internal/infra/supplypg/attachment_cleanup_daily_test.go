package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/infra/supplyfiles"
)

// Close a dedicated real connection pool after schema discovery, so the
// reference query itself fails. The running API's pool is never touched.
type closeoutCleanupConnectionFailure struct {
	*supplyfiles.F12AttachmentFileStore
	db *sql.DB
}

func (b closeoutCleanupConnectionFailure) ListBefore(ctx context.Context, cutoff time.Time) ([]supply.F12AttachmentMetadata, error) {
	rows, err := b.F12AttachmentFileStore.ListBefore(ctx, cutoff)
	if err == nil {
		err = b.db.Close()
	}
	return rows, err
}

func TestAttachmentCleanupDaily44(t *testing.T) {
	if os.Getenv("BSS_A_CLOSEOUT_CLEANUP") != "1" {
		t.Skip("explicit daily fixture opt-in required")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var f struct{ Run, Supplier string }
	data, err := os.ReadFile(os.Getenv("BSS_A_CLOSEOUT_MANIFEST"))
	must(err)
	must(json.Unmarshal(data, &f))
	if !strings.HasPrefix(f.Run, "QA-A-CLOSEOUT-") || !modelIdentity(f.Supplier) {
		t.Fatal("marked fixture required")
	}
	cfg, err := config.Load()
	must(err)
	if cfg.Database.DBName != "model_bss" {
		t.Fatal("daily database required")
	}
	client, err := infradb.New(cfg.Database)
	must(err)
	defer client.Close()
	db, err := client.DB.DB()
	must(err)
	ctx := context.Background()
	var version int
	var dirty bool
	must(db.QueryRow(`SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty))
	if version != 45 || dirty {
		t.Fatal("45/clean required")
	}
	var name string
	must(db.QueryRow(`SELECT name FROM supplier WHERE id=$1`, f.Supplier).Scan(&name))
	if name != f.Run {
		t.Fatal("fixture identity mismatch")
	}
	content := []byte("%PDF-1.7\nQA local attachment cleanup")
	age := func(root, ref string) {
		t.Helper()
		parts := strings.Split(ref, ":")
		if len(parts) != 3 {
			t.Fatal("opaque identity required")
		}
		at := time.Now().Add(-48 * time.Hour)
		must(os.Chtimes(filepath.Join(root, parts[2]+".blob"), at, at))
	}

	t.Run("F12OldOrphanOnlyAndReferenceQueryFailure", func(t *testing.T) {
		root := t.TempDir()
		files, err := supplyfiles.NewF12AttachmentFileStore(root)
		must(err)
		save := func() *supply.F12AttachmentContent {
			v, err := files.Save(ctx, "SUPPLIER", f.Supplier, "CONTRACT", "3", "qa.pdf", content)
			must(err)
			return v
		}
		bound, orphan, recent := save(), save(), save()
		age(root, bound.ID)
		age(root, orphan.ID)
		// A legacy reference is valid evidence for retention too; do not require a
		// new-version record to protect an existing file.
		var record string
		must(db.QueryRow(`INSERT INTO contract(subject_type,subject_id,file_id) VALUES('SUPPLIER',$1,$2) RETURNING id::text`, f.Supplier, bound.ID).Scan(&record))
		defer db.Exec(`DELETE FROM contract WHERE id=$1 AND subject_id=$2`, record, f.Supplier)
		cleaner := &F12AttachmentService{db: db, schema: "public", blobs: files}
		removed, err := cleaner.CleanupUnreferenced(ctx)
		must(err)
		if removed != 1 {
			t.Fatalf("removed=%d, want old orphan only", removed)
		}
		_, err = files.Open(ctx, orphan.ID)
		if err == nil {
			t.Fatal("old orphan retained")
		}
		for _, id := range []string{bound.ID, recent.ID} {
			_, err = files.Open(ctx, id)
			must(err)
		}
		// Force an actual sql: database is closed error specifically after listing.
		failed := save()
		age(root, failed.ID)
		conn, err := infradb.New(cfg.Database)
		must(err)
		defer conn.Close()
		broken, err := conn.DB.DB()
		must(err)
		failing := &F12AttachmentService{db: broken, schema: "public", blobs: closeoutCleanupConnectionFailure{files, broken}}
		removed, err = failing.CleanupUnreferenced(ctx)
		if err == nil || removed != 0 {
			t.Fatalf("failed query must stop deletion: count=%d err=%v", removed, err)
		}
		_, err = files.Open(ctx, failed.ID)
		must(err)
		_, err = files.Open(ctx, bound.ID)
		must(err)
		// Final authorization failure after bytes were saved removes the upload.
		before, err := os.ReadDir(root)
		must(err)
		_, err = uploadF12AttachmentBeforeWrite(ctx, files, "3", supply.F12AttachmentUploadCommand{SubjectType: "SUPPLIER", SubjectID: f.Supplier, Purpose: "CONTRACT", FileName: "qa.pdf", Content: content}, func(context.Context) error { return supply.ErrForbidden })
		if err == nil {
			t.Fatal("authorization failure accepted")
		}
		after, err := os.ReadDir(root)
		must(err)
		if len(before) != len(after) {
			t.Fatal("failed authorization left a file")
		}
	})
	t.Run("QualificationOldOrphanOnlyAndDatabaseFailure", func(t *testing.T) {
		root := t.TempDir()
		files, err := supplyfiles.NewQualificationFileStore(root)
		must(err)
		save := func() *supply.SupplierQualificationFile {
			v, err := files.Save(ctx, "qa.pdf", content)
			must(err)
			return v
		}
		bound, orphan, recent := save(), save(), save()
		age(root, bound.ID)
		age(root, orphan.ID)
		var record string
		must(db.QueryRow(`INSERT INTO supplier_qualification(supplier_id,name,file_ref) VALUES($1,$2,$3) RETURNING id::text`, f.Supplier, f.Run, bound.ID).Scan(&record))
		defer db.Exec(`DELETE FROM supplier_qualification WHERE id=$1 AND supplier_id=$2`, record, f.Supplier)
		cleaner, err := NewSupplierQualificationAttachmentCleaner(db, "public", files)
		must(err)
		removed, err := cleaner.CleanupUnreferenced(ctx)
		must(err)
		if removed != 1 {
			t.Fatalf("removed=%d", removed)
		}
		_, err = files.Open(ctx, orphan.ID)
		if err == nil {
			t.Fatal("old orphan retained")
		}
		for _, id := range []string{bound.ID, recent.ID} {
			_, err = files.Open(ctx, id)
			must(err)
		}
		failed := save()
		age(root, failed.ID)
		conn, err := infradb.New(cfg.Database)
		must(err)
		defer conn.Close()
		broken, err := conn.DB.DB()
		must(err)
		must(broken.Close())
		failing, err := NewSupplierQualificationAttachmentCleaner(broken, "public", files)
		must(err)
		removed, err = failing.CleanupUnreferenced(ctx)
		if err == nil || removed != 0 {
			t.Fatalf("failed DB lookup deleted files: count=%d err=%v", removed, err)
		}
		_, err = files.Open(ctx, failed.ID)
		must(err)
		_, err = files.Open(ctx, bound.ID)
		must(err)
	})
}
