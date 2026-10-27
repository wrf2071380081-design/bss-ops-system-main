package supply

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type qualificationFileBlobFake struct {
	file       *SupplierQualificationFile
	opened     *SupplierQualificationFile
	deleteCall int
	saveCalls  int
}

func (f *qualificationFileBlobFake) Save(context.Context, string, []byte) (*SupplierQualificationFile, error) {
	f.saveCalls++
	return f.file, nil
}
func (f *qualificationFileBlobFake) Open(context.Context, string) (*SupplierQualificationFile, error) {
	return f.opened, nil
}
func (f *qualificationFileBlobFake) Delete(context.Context, string) error {
	f.deleteCall++
	return nil
}

type qualificationAttachmentStoreFake struct {
	created       *SupplierQualification
	createErr     error
	authorizeErr  error
	authorizeCall int
	createActor   string
	createID      string
	createName    string
	createFileRef string
	fileRef       string
}

type qualificationMetadataStoreFake struct {
	qualificationAttachmentStoreFake
	updateCalls int
}

func (f *qualificationMetadataStoreFake) UpdateQualificationMetadata(context.Context, WriteMeta, string, string, uint64, string, SupplierQualificationMetadata, time.Time) (*SupplierQualification, error) {
	f.updateCalls++
	return nil, nil
}

func (f *qualificationAttachmentStoreFake) CreateQualification(_ context.Context, meta WriteMeta, supplierID, name, fileRef string, at time.Time) (*SupplierQualification, error) {
	f.createActor, f.createID, f.createName, f.createFileRef = meta.ActorID, supplierID, name, fileRef
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.created, nil
}

func (f *qualificationAttachmentStoreFake) AuthorizeSupplierQualificationUpload(_ context.Context, _ WriteMeta, _ string) error {
	f.authorizeCall++
	return f.authorizeErr
}

func (f *qualificationAttachmentStoreFake) QualificationFileReference(context.Context, string, string, string) (string, string, error) {
	return f.fileRef, "营业执照", nil
}

func TestSupplierQualificationFilesUploadStoresOwnedOpaqueReference(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	file := &SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf", Size: 13}
	row := &SupplierQualification{ID: "81", SupplierID: "21", Name: "营业执照", HasAttachment: true, UploadedAt: at}
	blobs := &qualificationFileBlobFake{file: file}
	store := &qualificationAttachmentStoreFake{created: row}
	service := NewSupplierQualificationService(store, blobs, func() time.Time { return at })
	meta := WriteMeta{ActorID: "7", RequestID: "request-81", Reason: "补充资质"}
	got, err := service.Upload(ctx, meta, "21", "营业执照", "license.pdf", []byte("%PDF-1.7\nbody"))
	if err != nil || got != row || store.authorizeCall != 1 || store.createActor != "7" || store.createID != "21" || store.createName != "营业执照" || store.createFileRef != file.ID || !got.HasAttachment || !got.CanDownload {
		t.Fatalf("upload result=%+v store=%+v err=%v", got, store, err)
	}
}

func TestSupplierQualificationFilesRejectUnauthorizedUploadBeforeSaving(t *testing.T) {
	blobs := &qualificationFileBlobFake{file: &SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf", Size: 13}}
	store := &qualificationAttachmentStoreFake{authorizeErr: ErrForbidden}
	service := NewSupplierQualificationService(store, blobs, time.Now)
	_, err := service.Upload(context.Background(), WriteMeta{ActorID: "7", RequestID: "request-unauthorized", Reason: "补充资质"}, "21", "营业执照", "license.pdf", []byte("%PDF-1.7\nbody"))
	if !errors.Is(err, ErrForbidden) || store.authorizeCall != 1 || blobs.saveCalls != 0 {
		t.Fatalf("unauthorized upload error=%v authorize calls=%d blob save calls=%d", err, store.authorizeCall, blobs.saveCalls)
	}
}

func TestSupplierQualificationFilesDeleteBlobWhenBusinessWriteFails(t *testing.T) {
	ctx := context.Background()
	file := &SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf", Size: 13}
	blobs := &qualificationFileBlobFake{file: file}
	store := &qualificationAttachmentStoreFake{createErr: ErrForbidden}
	service := NewSupplierQualificationService(store, blobs, time.Now)
	_, err := service.Upload(ctx, WriteMeta{ActorID: "7", RequestID: "request-82", Reason: "补充资质"}, "21", "营业执照", "license.pdf", []byte("%PDF-1.7\nbody"))
	if !errors.Is(err, ErrForbidden) || blobs.deleteCall != 1 {
		t.Fatalf("write error=%v blob cleanup calls=%d", err, blobs.deleteCall)
	}
}

func TestSupplierQualificationFilesRejectInvalidMetadataBeforeSaving(t *testing.T) {
	blobs := &qualificationFileBlobFake{file: &SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	service := NewSupplierQualificationService(&qualificationAttachmentStoreFake{}, blobs, time.Now)
	_, err := service.Upload(context.Background(), WriteMeta{ActorID: "7", RequestID: "request-83", Reason: "补充资质"}, "021", "", "../license.pdf", []byte("%PDF-1.7\nbody"))
	if err == nil || blobs.saveCalls != 0 {
		t.Fatalf("invalid supplier/name/file metadata reached blob storage: err=%v", err)
	}
}

func TestSupplierQualificationFilesCannotDownloadBlobWithMismatchedSize(t *testing.T) {
	content := []byte("%PDF-1.7\nbody")
	file := &SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf", Size: len(content) + 1, Content: content}
	blobs := &qualificationFileBlobFake{opened: file}
	store := &qualificationAttachmentStoreFake{fileRef: file.ID}
	service := NewSupplierQualificationService(store, blobs, time.Now)
	if service.CanDownload(context.Background(), file.ID) {
		t.Fatal("blob whose stored size disagrees with downloaded bytes was advertised as downloadable")
	}
}

func TestSupplierQualificationFilesCannotAdvertiseMetadataOnlyBlobAsDownloadable(t *testing.T) {
	file := &SupplierQualificationFile{ID: "squal:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "license.pdf", ContentType: "application/pdf", Size: 13}
	blobs := &qualificationFileBlobFake{opened: file}
	store := &qualificationAttachmentStoreFake{fileRef: file.ID}
	service := NewSupplierQualificationService(store, blobs, time.Now)
	if service.CanDownload(context.Background(), file.ID) {
		t.Fatal("metadata-only blob was advertised as downloadable without bytes")
	}
	if _, err := service.Download(context.Background(), "7", "21", "81"); !errors.Is(err, ErrReadContract) {
		t.Fatalf("metadata-only blob download error=%v, want read contract error", err)
	}
}

func TestSupplierQualificationMetadataUpdateRejectsInvalidIdentifiersAndRevision(t *testing.T) {
	tests := []struct {
		name            string
		meta            WriteMeta
		supplierID      string
		qualificationID string
		revision        uint64
		wantField       string
	}{
		{name: "actor", meta: WriteMeta{ActorID: "0", RequestID: "request", Reason: "补充"}, supplierID: "21", qualificationID: "81", revision: 1, wantField: "actor_id"},
		{name: "supplier", meta: WriteMeta{ActorID: "7", RequestID: "request", Reason: "补充"}, supplierID: "021", qualificationID: "81", revision: 1, wantField: "supplier_id"},
		{name: "qualification", meta: WriteMeta{ActorID: "7", RequestID: "request", Reason: "补充"}, supplierID: "21", qualificationID: "0", revision: 1, wantField: "qualification_id"},
		{name: "zero revision", meta: WriteMeta{ActorID: "7", RequestID: "request", Reason: "补充"}, supplierID: "21", qualificationID: "81", revision: 0, wantField: "expected_revision"},
		{name: "revision exceeds storage limit", meta: WriteMeta{ActorID: "7", RequestID: "request", Reason: "补充"}, supplierID: "21", qualificationID: "81", revision: uint64(math.MaxInt32), wantField: "expected_revision"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &qualificationMetadataStoreFake{}
			service := NewSupplierQualificationService(store, nil, time.Now)
			_, err := service.UpdateMetadata(context.Background(), test.meta, test.supplierID, test.qualificationID, test.revision, "营业执照", SupplierQualificationMetadata{Type: "证照"})
			var fieldErr *FieldError
			if !errors.As(err, &fieldErr) || fieldErr.Path != test.wantField || store.updateCalls != 0 {
				t.Fatalf("update error=%v field=%+v store calls=%d, want field %q and no write", err, fieldErr, store.updateCalls, test.wantField)
			}
		})
	}
}
