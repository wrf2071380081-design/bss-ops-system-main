package supplyfiles

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewQualificationFileStoreRestrictsExistingRootPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permission bits are not enforced on Windows")
	}
	root := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQualificationFileStore(root); err != nil {
		t.Fatalf("initialize existing qualification directory: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("existing qualification directory permissions = %#o, want 0700", got)
	}
}

func TestQualificationFileStoreAcceptsTwoMiBAndReturnsOpaqueReference(t *testing.T) {
	store, err := NewQualificationFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), QualificationFileMaxBytes-9)...)
	file, err := store.Save(context.Background(), "营业执照.pdf", content)
	if err != nil {
		t.Fatalf("save exact 2 MiB PDF: %v", err)
	}
	if !strings.HasPrefix(file.ID, "squal:v1:") || strings.ContainsAny(file.ID, "/\\") || file.Name != "营业执照.pdf" || file.ContentType != "application/pdf" || file.Size != QualificationFileMaxBytes {
		t.Fatalf("unsafe or incorrect file metadata: %+v", file)
	}
	opened, err := store.Open(context.Background(), file.ID)
	if err != nil || opened.Name != file.Name || opened.ContentType != file.ContentType || !bytes.Equal(opened.Content, content) {
		t.Fatalf("open saved file=%+v err=%v", opened, err)
	}
}

func TestQualificationFileStoreRejectsOversizeAndMismatchedType(t *testing.T) {
	store, err := NewQualificationFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oversize := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), QualificationFileMaxBytes-8)...)
	if _, err := store.Save(context.Background(), "large.pdf", oversize); err == nil {
		t.Fatal("expected file larger than 2 MiB to be rejected")
	}
	if _, err := store.Save(context.Background(), "payload.pdf", []byte("not a PDF")); err == nil {
		t.Fatal("expected content that does not match the PDF extension to be rejected")
	}
	if _, err := store.Save(context.Background(), "payload.exe", []byte("%PDF-1.7\n")); err == nil {
		t.Fatal("expected a disallowed extension to be rejected")
	}
}

func TestQualificationFileStoreRejectsTamperedReferenceAndDeletesObject(t *testing.T) {
	store, err := NewQualificationFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.Save(context.Background(), "license.pdf", []byte("%PDF-1.7\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../../secret", "squal:v1:../secret", "squal:v2:0123456789abcdef0123456789abcdef"} {
		if _, err := store.Open(context.Background(), bad); err == nil {
			t.Fatalf("tampered file reference accepted: %q", bad)
		}
	}
	if err := store.Delete(context.Background(), file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), file.ID); err == nil {
		t.Fatal("deleted attachment remained available")
	}
}

func TestQualificationFileStoreListsOnlyOldOpaqueReferencesIncludingBlobOnlyOrphans(t *testing.T) {
	store, err := NewQualificationFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	oldFile, err := store.Save(ctx, "old.pdf", []byte("%PDF-1.7\nold"))
	if err != nil {
		t.Fatal(err)
	}
	newFile, err := store.Save(ctx, "new.pdf", []byte("%PDF-1.7\nnew"))
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	oldAt := cutoff.Add(-time.Hour)
	oldKey := strings.TrimPrefix(oldFile.ID, "squal:v1:")
	for _, suffix := range []string{".blob", ".json"} {
		path := filepath.Join(store.root, oldKey+suffix)
		if err := os.Chtimes(path, oldAt, oldAt); err != nil {
			t.Fatal(err)
		}
	}
	orphanKey := strings.Repeat("b", 32)
	orphanPath := filepath.Join(store.root, orphanKey+".blob")
	if err := os.WriteFile(orphanPath, []byte("%PDF-1.7\norphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(orphanPath, oldAt, oldAt); err != nil {
		t.Fatal(err)
	}

	refs, err := store.ListBefore(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{oldFile.ID: false, "squal:v1:" + orphanKey: false}
	for _, ref := range refs {
		if _, ok := want[ref]; !ok {
			t.Fatalf("unexpected cleanup candidate %q", ref)
		}
		want[ref] = true
	}
	for ref, found := range want {
		if !found {
			t.Errorf("old opaque reference %q was not listed", ref)
		}
	}
	for _, ref := range refs {
		if ref == newFile.ID {
			t.Fatal("new file was listed as an orphan candidate")
		}
	}
}

func TestQualificationFileStoreRejectsOversizedStoredBlob(t *testing.T) {
	root := t.TempDir()
	store, err := NewQualificationFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	key := "0123456789abcdef0123456789abcdef"
	blobPath := filepath.Join(root, key+".blob")
	content := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), QualificationFileMaxBytes-8)...)
	if err := os.WriteFile(blobPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, key+".json"), []byte(`{"name":"license.pdf","content_type":"application/pdf"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), "squal:v1:"+key); err == nil {
		t.Fatal("oversized stored qualification blob was accepted")
	}
}

func TestReadPrivateAttachmentFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "private.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7\nvalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.blob")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	if _, err := readPrivateAttachmentFile(link, QualificationFileMaxBytes); err == nil {
		t.Fatal("symlinked private attachment was accepted")
	}
}
