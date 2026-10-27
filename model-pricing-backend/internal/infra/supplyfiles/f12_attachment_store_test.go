package supplyfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"model_bss/internal/domain/supply"
)

func TestF12AttachmentFileStoreVerifyChecksUploaderWhenRequired(t *testing.T) {
	store, err := NewF12AttachmentFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.Save(context.Background(), "SUPPLIER", "10", "CONTRACT", "7", "contract.pdf", []byte("%PDF-1.7\ncontract"))
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}

	if err := store.Verify(context.Background(), file.ID, "SUPPLIER", "10", "CONTRACT", "7"); err != nil {
		t.Fatalf("verify current uploader: %v", err)
	}
	if err := store.Verify(context.Background(), file.ID, "SUPPLIER", "10", "CONTRACT", "8"); !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("verify different uploader error = %v, want forbidden", err)
	}
	if err := store.Verify(context.Background(), file.ID, "SUPPLIER", "10", "CONTRACT", ""); err != nil {
		t.Fatalf("verify same-chain existing attachment: %v", err)
	}
}

func TestF12AttachmentFileStoreListsOnlyFilesOlderThanCutoff(t *testing.T) {
	store, err := NewF12AttachmentFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.Save(context.Background(), "CUSTOMER", "11", "CREDIT", "7", "credit.pdf", []byte("%PDF-1.7\ncredit"))
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}

	files, err := store.ListBefore(context.Background(), time.Now().Add(time.Minute))
	if err != nil || len(files) != 1 || files[0].ID != file.ID {
		t.Fatalf("ListBefore(future cutoff) = %+v, %v; want the saved file", files, err)
	}
	files, err = store.ListBefore(context.Background(), time.Now().Add(-time.Hour))
	if err != nil || len(files) != 0 {
		t.Fatalf("ListBefore(past cutoff) = %+v, %v; want no files", files, err)
	}
}

func TestF12AttachmentFileStoreListsOldBlobWithoutManifestForCleanup(t *testing.T) {
	root := t.TempDir()
	store, err := NewF12AttachmentFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	key := "0123456789abcdef0123456789abcdef"
	blobPath := filepath.Join(root, key+".blob")
	if err := os.WriteFile(blobPath, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(blobPath, old, old); err != nil {
		t.Fatal(err)
	}

	files, err := store.ListBefore(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil || len(files) != 1 || files[0].ID != "f12:v1:"+key {
		t.Fatalf("ListBefore(orphan blob) = %+v, %v; want its opaque ID", files, err)
	}
	if err := store.Delete(context.Background(), files[0].ID); err != nil {
		t.Fatalf("delete orphan blob: %v", err)
	}
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("orphan blob still exists after cleanup: err=%v", err)
	}
}
