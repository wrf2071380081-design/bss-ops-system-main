package supplypg

import (
	"context"
	"errors"
	"testing"

	"model_bss/internal/domain/supply"
)

type f12CleanupBlobStore struct{ deleted string }

func (s *f12CleanupBlobStore) Save(context.Context, string, string, string, string, string, []byte) (*supply.F12AttachmentContent, error) {
	return nil, nil
}

func (s *f12CleanupBlobStore) Open(context.Context, string) (*supply.F12AttachmentContent, error) {
	return nil, supply.ErrNotFound
}

func (s *f12CleanupBlobStore) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.deleted = ref
	return nil
}

func (*f12CleanupBlobStore) Verify(context.Context, string, string, string, string, string) error {
	return nil
}

var _ supply.F12AttachmentBlobStore = (*f12CleanupBlobStore)(nil)

type f12UploadOrderBlobStore struct {
	saveCalls int
	deleted  string
}

func (s *f12UploadOrderBlobStore) Save(_ context.Context, subjectType, subjectID, purpose, uploadedBy, name string, content []byte) (*supply.F12AttachmentContent, error) {
	s.saveCalls++
	return &supply.F12AttachmentContent{F12AttachmentMetadata: supply.F12AttachmentMetadata{
		ID: "f12:v1:0123456789abcdef0123456789abcdef", SubjectType: subjectType, SubjectID: subjectID,
		Purpose: purpose, UploadedBy: uploadedBy, Name: name, Size: len(content),
	}}, nil
}

func (*f12UploadOrderBlobStore) Open(context.Context, string) (*supply.F12AttachmentContent, error) {
	return nil, supply.ErrNotFound
}

func (s *f12UploadOrderBlobStore) Delete(_ context.Context, ref string) error {
	s.deleted = ref
	return nil
}

func (*f12UploadOrderBlobStore) Verify(context.Context, string, string, string, string, string) error {
	return nil
}

var _ supply.F12AttachmentBlobStore = (*f12UploadOrderBlobStore)(nil)

func TestF12AttachmentBytesAreSavedBeforeF12SubjectLock(t *testing.T) {
	input := supply.F12AttachmentUploadCommand{
		SubjectType: "SUPPLIER", SubjectID: "12", Purpose: "CONTRACT", FileName: "contract.pdf", Content: []byte("pdf-data"),
	}
	blobs := &f12UploadOrderBlobStore{}
	metadata, err := uploadF12AttachmentBeforeWrite(context.Background(), blobs, "34", input, func(context.Context) error {
		if blobs.saveCalls != 1 {
			t.Fatalf("authorization started after %d blob saves, want exactly one", blobs.saveCalls)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil || metadata.SubjectID != input.SubjectID || metadata.UploadedBy != "34" || blobs.deleted != "" {
		t.Fatalf("unexpected upload result: metadata=%+v deleted=%q", metadata, blobs.deleted)
	}
}

func TestF12AttachmentPreauthorizesBeforeSavingAndRechecksAfterSaving(t *testing.T) {
	input := supply.F12AttachmentUploadCommand{
		SubjectType: "SUPPLIER", SubjectID: "12", Purpose: "CONTRACT", FileName: "contract.pdf", Content: []byte("pdf-data"),
	}
	blobs := &f12UploadOrderBlobStore{}
	preauthorized, reauthorized := false, false
	metadata, err := uploadF12AttachmentWithPreflight(context.Background(), blobs, "34", input,
		func(context.Context) error {
			if blobs.saveCalls != 0 {
				t.Fatalf("preflight ran after %d blob saves, want none", blobs.saveCalls)
			}
			preauthorized = true
			return nil
		},
		func(context.Context) error {
			if !preauthorized || blobs.saveCalls != 1 {
				t.Fatalf("final authorization ran without preflight or after %d blob saves", blobs.saveCalls)
			}
			reauthorized = true
			return nil
		})
	if err != nil || metadata == nil || !preauthorized || !reauthorized {
		t.Fatalf("attachment flow did not complete both authorization checks: metadata=%+v preflight=%t final=%t err=%v", metadata, preauthorized, reauthorized, err)
	}
}

func TestF12AttachmentPreflightFailureDoesNotSaveBytes(t *testing.T) {
	input := supply.F12AttachmentUploadCommand{
		SubjectType: "SUPPLIER", SubjectID: "12", Purpose: "CONTRACT", FileName: "contract.pdf", Content: []byte("pdf-data"),
	}
	blobs := &f12UploadOrderBlobStore{}
	want := errors.New("not the supplier owner")
	metadata, err := uploadF12AttachmentWithPreflight(context.Background(), blobs, "34", input,
		func(context.Context) error { return want },
		func(context.Context) error { t.Fatal("final authorization should not run after failed preflight"); return nil })
	if !errors.Is(err, want) || metadata != nil || blobs.saveCalls != 0 || blobs.deleted != "" {
		t.Fatalf("preflight failure must not write attachment bytes: metadata=%+v deleted=%q saves=%d err=%v", metadata, blobs.deleted, blobs.saveCalls, err)
	}
}

func TestF12AttachmentIsRemovedWhenSubjectWriteFails(t *testing.T) {
	input := supply.F12AttachmentUploadCommand{
		SubjectType: "SUPPLIER", SubjectID: "12", Purpose: "CONTRACT", FileName: "contract.pdf", Content: []byte("pdf-data"),
	}
	blobs := &f12UploadOrderBlobStore{}
	metadata, err := uploadF12AttachmentBeforeWrite(context.Background(), blobs, "34", input, func(context.Context) error {
		return errors.New("authorization failed")
	})
	if err == nil || metadata != nil {
		t.Fatalf("expected authorization error without metadata, got metadata=%+v err=%v", metadata, err)
	}
	if blobs.deleted != "f12:v1:0123456789abcdef0123456789abcdef" {
		t.Fatalf("failed upload cleanup ref = %q", blobs.deleted)
	}
}

func TestDeleteF12AttachmentBestEffortUsesLiveCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blobs := &f12CleanupBlobStore{}
	saved := &supply.F12AttachmentContent{F12AttachmentMetadata: supply.F12AttachmentMetadata{ID: "f12:v1:opaque"}}

	deleteF12AttachmentBestEffort(ctx, blobs, saved)
	if blobs.deleted != saved.ID {
		t.Fatalf("deleted attachment = %q, want %q", blobs.deleted, saved.ID)
	}
}
