package supply

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

const SupplierQualificationMaxFileBytes = 2 * 1024 * 1024

var (
	ErrQualificationFileUnavailable   = errors.New("supplier qualification attachment is unavailable")
	ErrQualificationMetadataRequired = errors.New("supplier qualification metadata is required by the active schema")
)

type SupplierQualificationFile struct {
	ID          string
	Name        string
	ContentType string
	Size        int
	Content     []byte
}

type SupplierQualificationBlobStore interface {
	Save(context.Context, string, []byte) (*SupplierQualificationFile, error)
	Open(context.Context, string) (*SupplierQualificationFile, error)
	Delete(context.Context, string) error
}

type SupplierQualificationAttachmentStore interface {
	CreateQualification(context.Context, WriteMeta, string, string, string, time.Time) (*SupplierQualification, error)
	QualificationFileReference(context.Context, string, string, string) (string, string, error)
}

// SupplierQualificationUploadAuthorizer lets the service reject an upload
// before writing bytes to private storage. CreateQualification still repeats
// the authorization inside its write transaction to close the race.
type SupplierQualificationUploadAuthorizer interface {
	AuthorizeSupplierQualificationUpload(context.Context, WriteMeta, string) error
}

type SupplierQualificationMetadata struct {
	Type      string
	ValidFrom string
	ValidTo   string
}

// SupplierQualificationMetadataStore is available only when the approved
// qualification metadata columns are present. Review status remains read-only
// and must be written by its authoritative review workflow.
type SupplierQualificationMetadataStore interface {
	CreateQualificationWithMetadata(context.Context, WriteMeta, string, string, string, SupplierQualificationMetadata, time.Time) (*SupplierQualification, error)
	UpdateQualificationMetadata(context.Context, WriteMeta, string, string, uint64, string, SupplierQualificationMetadata, time.Time) (*SupplierQualification, error)
}

type SupplierQualificationService struct {
	store SupplierQualificationAttachmentStore
	blobs SupplierQualificationBlobStore
	now   func() time.Time
}

func NewSupplierQualificationService(store SupplierQualificationAttachmentStore, blobs SupplierQualificationBlobStore, clock func() time.Time) *SupplierQualificationService {
	if clock == nil {
		clock = time.Now
	}
	return &SupplierQualificationService{store: store, blobs: blobs, now: clock}
}

func (s *SupplierQualificationService) Upload(ctx context.Context, meta WriteMeta, supplierID, name, fileName string, content []byte) (*SupplierQualification, error) {
	return s.upload(ctx, meta, supplierID, name, fileName, content, nil)
}

func (s *SupplierQualificationService) UploadWithMetadata(ctx context.Context, meta WriteMeta, supplierID, name, fileName string, content []byte, details SupplierQualificationMetadata) (*SupplierQualification, error) {
	canonical, err := canonicalSupplierQualificationMetadata(details)
	if err != nil {
		return nil, err
	}
	return s.upload(ctx, meta, supplierID, name, fileName, content, &canonical)
}

func (s *SupplierQualificationService) upload(ctx context.Context, meta WriteMeta, supplierID, name, fileName string, content []byte, details *SupplierQualificationMetadata) (*SupplierQualification, error) {
	if s == nil || s.store == nil || s.blobs == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) {
		return nil, invalid("actor_id", "expected authenticated staff bigint string")
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return nil, err
	}
	if !validID(supplierID) {
		return nil, invalid("supplier_id", "expected positive bigint string")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 128 {
		return nil, invalid("name", "qualification name is required and must not exceed 128 characters")
	}
	if len(content) == 0 || len(content) > SupplierQualificationMaxFileBytes {
		return nil, invalid("file", "qualification file must be between 1 byte and 2 MiB")
	}
	authorizer, ok := s.store.(SupplierQualificationUploadAuthorizer)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	if err := authorizer.AuthorizeSupplierQualificationUpload(ctx, meta, supplierID); err != nil {
		return nil, err
	}
	file, err := s.blobs.Save(ctx, fileName, content)
	if err != nil {
		return nil, invalid("file", "qualification file must be a valid PDF, PNG or JPEG up to 2 MiB")
	}
	if !validQualificationFile(file) {
		_ = s.blobs.Delete(ctx, file.ID)
		return nil, ErrReadContract
	}
	createdAt := s.now().UTC().Truncate(time.Microsecond)
	var row *SupplierQualification
	if details == nil {
		row, err = s.store.CreateQualification(ctx, meta, supplierID, name, file.ID, createdAt)
	} else {
		metadataStore, ok := s.store.(SupplierQualificationMetadataStore)
		if !ok {
			_ = s.blobs.Delete(ctx, file.ID)
			return nil, ErrDependencyUnavailable
		}
		row, err = metadataStore.CreateQualificationWithMetadata(ctx, meta, supplierID, name, file.ID, *details, createdAt)
	}
	if err != nil {
		_ = s.blobs.Delete(ctx, file.ID)
		return nil, err
	}
	if row == nil || !validID(row.ID) || row.SupplierID != supplierID || row.Name != name || !row.HasAttachment || row.UploadedAt.IsZero() {
		_ = s.blobs.Delete(ctx, file.ID)
		return nil, ErrReadContract
	}
	row.CanDownload = true
	row.FileName = file.Name
	row.FileReference = file.ID
	return row, nil
}

func (s *SupplierQualificationService) UpdateMetadata(ctx context.Context, meta WriteMeta, supplierID, qualificationID string, expectedRevision uint64, name string, details SupplierQualificationMetadata) (*SupplierQualification, error) {
	if s == nil || s.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) {
		return nil, invalid("actor_id", "expected authenticated staff bigint string")
	}
	if !validID(supplierID) {
		return nil, invalid("supplier_id", "expected positive bigint string")
	}
	if !validID(qualificationID) {
		return nil, invalid("qualification_id", "expected positive bigint string")
	}
	if expectedRevision == 0 || expectedRevision >= math.MaxInt32 {
		return nil, invalid("expected_revision", "expected a positive revision below the storage limit")
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 128 {
		return nil, invalid("name", "qualification name is required and must not exceed 128 characters")
	}
	canonical, err := canonicalSupplierQualificationMetadata(details)
	if err != nil {
		return nil, err
	}
	metadataStore, ok := s.store.(SupplierQualificationMetadataStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	row, err := metadataStore.UpdateQualificationMetadata(ctx, meta, supplierID, qualificationID, expectedRevision, name, canonical, s.now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	if row == nil || row.ID != qualificationID || row.SupplierID != supplierID || row.Name == "" || row.Revision == nil || *row.Revision != expectedRevision+1 {
		return nil, ErrReadContract
	}
	if row.FileReference != "" {
		fileName, canDownload := s.DownloadableFileName(ctx, row.FileReference)
		row.CanDownload, row.FileName = canDownload, fileName
	}
	return row, nil
}

func canonicalSupplierQualificationMetadata(details SupplierQualificationMetadata) (SupplierQualificationMetadata, error) {
	details.Type = strings.TrimSpace(details.Type)
	details.ValidFrom = strings.TrimSpace(details.ValidFrom)
	details.ValidTo = strings.TrimSpace(details.ValidTo)
	if details.Type == "" || len([]rune(details.Type)) > 128 {
		return SupplierQualificationMetadata{}, invalid("qualification_type", "required text up to 128 characters; allowed values are owned by the review workflow")
	}
	from, err := parseSupplierQualificationDate(details.ValidFrom)
	if err != nil {
		return SupplierQualificationMetadata{}, invalid("valid_from", "expected YYYY-MM-DD or empty")
	}
	to, err := parseSupplierQualificationDate(details.ValidTo)
	if err != nil {
		return SupplierQualificationMetadata{}, invalid("valid_to", "expected YYYY-MM-DD or empty")
	}
	if from != nil && to != nil && from.After(*to) {
		return SupplierQualificationMetadata{}, invalid("valid_to", "must be on or after valid_from")
	}
	return details, nil
}

func parseSupplierQualificationDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return nil, errors.New("invalid calendar date")
	}
	return &parsed, nil
}

func (s *SupplierQualificationService) Download(ctx context.Context, actorID, supplierID, qualificationID string) (*SupplierQualificationFile, error) {
	if s == nil || s.store == nil || s.blobs == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(actorID) {
		return nil, invalid("actor_id", "expected authenticated staff bigint string")
	}
	if !validID(supplierID) || !validID(qualificationID) {
		return nil, invalid("qualification_id", "expected positive bigint string")
	}
	ref, name, err := s.store.QualificationFileReference(ctx, actorID, supplierID, qualificationID)
	if err != nil {
		return nil, err
	}
	if !validQualificationFileReference(ref) {
		return nil, ErrQualificationFileUnavailable
	}
	file, err := s.blobs.Open(ctx, ref)
	if err != nil {
		return nil, ErrQualificationFileUnavailable
	}
	if !validQualificationFile(file) || file.Size != len(file.Content) || name == "" {
		return nil, ErrReadContract
	}
	return file, nil
}

func (s *SupplierQualificationService) CanDownload(ctx context.Context, ref string) bool {
	_, ok := s.DownloadableFileName(ctx, ref)
	return ok
}

func (s *SupplierQualificationService) DownloadableFileName(ctx context.Context, ref string) (string, bool) {
	if s == nil || s.blobs == nil || !validQualificationFileReference(ref) {
		return "", false
	}
	file, err := s.blobs.Open(ctx, ref)
	if err != nil || !validQualificationFile(file) || file.Size != len(file.Content) {
		return "", false
	}
	return file.Name, true
}

func validQualificationFileReference(value string) bool {
	if len(value) != len("squal:v1:")+32 || !strings.HasPrefix(value, "squal:v1:") {
		return false
	}
	for _, char := range value[len("squal:v1:"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func validQualificationFile(file *SupplierQualificationFile) bool {
	if file == nil || !validQualificationFileReference(file.ID) || file.Name == "" || strings.ContainsAny(file.Name, `/\\`) || file.Size < 1 || file.Size > SupplierQualificationMaxFileBytes || len(file.Content) > SupplierQualificationMaxFileBytes {
		return false
	}
	if len(file.Content) > 0 && len(file.Content) != file.Size {
		return false
	}
	if len(file.Content) > 0 && len(file.Content) != file.Size {
		return false
	}
	switch file.ContentType {
	case "application/pdf", "image/png", "image/jpeg":
		return true
	default:
		return false
	}
}
