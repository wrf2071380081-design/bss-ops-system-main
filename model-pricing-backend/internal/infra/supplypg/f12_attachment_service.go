package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"

	"model_bss/internal/domain/supply"
)

var f12AttachmentIdentity = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var f12AttachmentCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

const f12AttachmentOrphanGracePeriod = 24 * time.Hour

type f12AttachmentBlobLister interface {
	ListBefore(context.Context, time.Time) ([]supply.F12AttachmentMetadata, error)
}

// F12AttachmentService authorizes uploads against the real F12 subject and
// only downloads files already referenced by a contract or credit row.
type F12AttachmentService struct {
	db     *sql.DB
	schema string
	write  *F12RecordStore
	blobs  supply.F12AttachmentBlobStore
}

var _ supply.F12AttachmentService = (*F12AttachmentService)(nil)

func NewF12AttachmentService(db *sql.DB, schema string, write *F12RecordStore, blobs supply.F12AttachmentBlobStore) (*F12AttachmentService, error) {
	if db == nil || write == nil || blobs == nil || !modelSchemaName.MatchString(schema) {
		return nil, supply.ErrDependencyUnavailable
	}
	return &F12AttachmentService{db: db, schema: schema, write: write, blobs: blobs}, nil
}

func (s *F12AttachmentService) Upload(ctx context.Context, meta supply.WriteMeta, input supply.F12AttachmentUploadCommand) (*supply.F12AttachmentMetadata, error) {
	if s == nil || s.db == nil || s.write == nil || s.blobs == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if !f12AttachmentIdentity.MatchString(meta.ActorID) || strings.TrimSpace(meta.RequestID) == "" || strings.TrimSpace(meta.Reason) == "" {
		return nil, &supply.FieldError{Path: "request", Reason: "authenticated actor, request ID and reason are required"}
	}
	if !f12AttachmentIdentity.MatchString(input.SubjectID) || (input.SubjectType != "SUPPLIER" && input.SubjectType != "CUSTOMER") || (input.Purpose != "CONTRACT" && input.Purpose != "CREDIT") || len(input.Content) == 0 || len(input.Content) > supply.F12AttachmentMaxFileBytes {
		return nil, &supply.FieldError{Path: "file", Reason: "invalid subject, purpose or attachment size"}
	}
	if (strings.TrimSpace(input.SourceFlowID) == "") != (strings.TrimSpace(input.SourceModuleKey) == "") {
		return nil, &supply.FieldError{Path: "source_flow_id", Reason: "flow and module must be provided together"}
	}
	if strings.TrimSpace(input.SourceFlowID) == "" && input.ExpectedFlowRevision != 0 || strings.TrimSpace(input.SourceFlowID) != "" && (input.ExpectedFlowRevision == 0 || input.ExpectedFlowRevision > math.MaxInt32) {
		return nil, &supply.FieldError{Path: "expected_flow_revision", Reason: "flow writes require the current expected revision"}
	}
	ready, err := s.writeSchemaReady(ctx)
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	if !ready {
		return nil, supply.ErrDependencyUnavailable
	}
	preauthorize := func(ctx context.Context) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return mapF12StorageError(err)
		}
		defer tx.Rollback()
		access := &f12RecordTransaction{store: s.write, tx: tx}
		subject, err := access.SubjectForUpdate(ctx, input.SubjectType, input.SubjectID)
		if err != nil {
			return err
		}
		if subject == nil || subject.Type != input.SubjectType || subject.ID != input.SubjectID {
			return supply.ErrReadContract
		}
		if _, err := access.AuthorizeF12Write(ctx, meta.ActorID, input.SubjectType, input.SubjectID, strings.TrimSpace(input.SourceFlowID), strings.TrimSpace(input.SourceModuleKey), input.ExpectedFlowRevision); err != nil {
			return err
		}
		return mapF12StorageError(tx.Commit())
	}
	authorize := func(ctx context.Context) error {
		return s.write.Within(ctx, func(tx supply.F12RecordTransaction) error {
			if tx == nil {
				return supply.ErrDependencyUnavailable
			}
			subject, err := tx.SubjectForUpdate(ctx, input.SubjectType, input.SubjectID)
			if err != nil {
				return err
			}
			if subject == nil || subject.Type != input.SubjectType || subject.ID != input.SubjectID {
				return supply.ErrReadContract
			}
			_, err = tx.AuthorizeF12Write(ctx, meta.ActorID, input.SubjectType, input.SubjectID, strings.TrimSpace(input.SourceFlowID), strings.TrimSpace(input.SourceModuleKey), input.ExpectedFlowRevision)
			return err
		})
	}
	return uploadF12AttachmentWithPreflight(ctx, s.blobs, meta.ActorID, input, preauthorize, authorize)
}

func uploadF12AttachmentWithPreflight(ctx context.Context, blobs supply.F12AttachmentBlobStore, actorID string, input supply.F12AttachmentUploadCommand, preauthorize, authorize func(context.Context) error) (*supply.F12AttachmentMetadata, error) {
	if preauthorize == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := preauthorize(ctx); err != nil {
		return nil, err
	}
	return uploadF12AttachmentBeforeWrite(ctx, blobs, actorID, input, authorize)
}

// uploadF12AttachmentBeforeWrite saves the bytes before entering the shared
// unit of work that locks and authorizes the subject. The public idempotency
// middleware may already have opened its request transaction; this only keeps
// local file I/O outside the F12 subject-row lock. The returned file remains
// unreferenced until a contract or credit version binds it; a failed write
// removes it best-effort and the startup sweeper handles process crashes.
func uploadF12AttachmentBeforeWrite(ctx context.Context, blobs supply.F12AttachmentBlobStore, actorID string, input supply.F12AttachmentUploadCommand, authorize func(context.Context) error) (*supply.F12AttachmentMetadata, error) {
	if blobs == nil || authorize == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	saved, err := blobs.Save(ctx, input.SubjectType, input.SubjectID, input.Purpose, actorID, input.FileName, input.Content)
	if err != nil {
		return nil, err
	}
	if saved == nil || saved.ID == "" || saved.SubjectType != input.SubjectType || saved.SubjectID != input.SubjectID || saved.Purpose != input.Purpose || saved.UploadedBy != actorID || saved.Size != len(input.Content) {
		deleteF12AttachmentBestEffort(ctx, blobs, saved)
		return nil, supply.ErrReadContract
	}
	if err := authorize(ctx); err != nil {
		deleteF12AttachmentBestEffort(ctx, blobs, saved)
		return nil, err
	}
	metadata := saved.F12AttachmentMetadata
	return &metadata, nil
}

func deleteF12AttachmentBestEffort(ctx context.Context, blobs supply.F12AttachmentBlobStore, saved *supply.F12AttachmentContent) {
	if blobs == nil || saved == nil || strings.TrimSpace(saved.ID) == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = blobs.Delete(cleanupCtx, saved.ID)
}

// CleanupUnreferenced removes old upload files that no contract or credit row
// references. The grace period avoids racing in-flight uploads; the runtime
// calls this before accepting requests.
func (s *F12AttachmentService) CleanupUnreferenced(ctx context.Context) (int, error) {
	if s == nil || s.db == nil || s.blobs == nil {
		return 0, supply.ErrDependencyUnavailable
	}
	ready, err := s.writeSchemaReady(ctx)
	if err != nil {
		return 0, mapF12StorageError(err)
	}
	if !ready {
		return 0, nil
	}
	lister, ok := s.blobs.(f12AttachmentBlobLister)
	if !ok {
		return 0, supply.ErrDependencyUnavailable
	}
	files, err := lister.ListBefore(ctx, time.Now().Add(-f12AttachmentOrphanGracePeriod))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, file := range files {
		var referenced bool
		err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM `+s.table("contract")+` WHERE file_id::text=$1
			UNION ALL
			SELECT 1 FROM `+s.table("credit_line")+` WHERE file_id::text=$1
		)`, file.ID).Scan(&referenced)
		if err != nil {
			return removed, mapF12StorageError(err)
		}
		if referenced {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = s.blobs.Delete(cleanupCtx, file.ID)
		cancel()
		if err != nil {
			return removed, supply.ErrDependencyUnavailable
		}
		removed++
	}
	return removed, nil
}

func (s *F12AttachmentService) Download(ctx context.Context, actorID, subjectType, subjectID, fileID, purpose string) (*supply.F12AttachmentContent, error) {
	if s == nil || s.db == nil || s.write == nil || s.blobs == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if !f12AttachmentIdentity.MatchString(actorID) || !f12AttachmentIdentity.MatchString(subjectID) || (subjectType != "SUPPLIER" && subjectType != "CUSTOMER") || (purpose != "CONTRACT" && purpose != "CREDIT") || strings.TrimSpace(fileID) == "" {
		return nil, supply.ErrNotFound
	}
	ready, err := s.writeSchemaReady(ctx)
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	if !ready {
		return nil, supply.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	defer tx.Rollback()
	var references []struct{ flowID, moduleKey string }
	table := "contract"
	if purpose == "CREDIT" {
		table = "credit_line"
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT COALESCE(source_flow_id::text,''),COALESCE(source_module_key,'') FROM `+s.table(table)+` WHERE file_id::text=$1 AND subject_type=$2 AND subject_id=$3`, fileID, subjectType, subjectID)
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	for rows.Next() {
		var reference struct{ flowID, moduleKey string }
		if err := rows.Scan(&reference.flowID, &reference.moduleKey); err != nil {
			_ = rows.Close()
			return nil, mapF12StorageError(err)
		}
		references = append(references, reference)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, mapF12StorageError(err)
	}
	if err := rows.Close(); err != nil {
		return nil, mapF12StorageError(err)
	}
	if len(references) == 0 {
		return nil, supply.ErrNotFound
	}
	access := &f12RecordTransaction{store: s.write, tx: tx}
	authorized := false
	for _, reference := range references {
		err := access.AuthorizeF12Read(ctx, actorID, subjectType, subjectID, reference.flowID, reference.moduleKey)
		if err == nil {
			authorized = true
			break
		}
		if errors.Is(err, supply.ErrForbidden) {
			continue
		}
		return nil, err
	}
	if !authorized {
		return nil, supply.ErrForbidden
	}
	if err := tx.Commit(); err != nil {
		return nil, mapF12StorageError(err)
	}
	file, err := s.blobs.Open(ctx, fileID)
	if err != nil {
		if errors.Is(err, supply.ErrNotFound) {
			return nil, supply.ErrNotFound
		}
		return nil, supply.ErrDependencyUnavailable
	}
	if file.SubjectType != subjectType || file.SubjectID != subjectID || file.Purpose != purpose || file.ID != fileID || file.Size != len(file.Content) {
		return nil, supply.ErrForbidden
	}
	return file, nil
}

func (s *F12AttachmentService) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

func (s *F12AttachmentService) writeSchemaReady(ctx context.Context) (bool, error) {
	return f12RecordSchemaReady(ctx, s.db, s.schema)
}

// F12AttachmentBlobVerifier is passed into the version writer so attachment
// metadata is checked while the subject's database row is locked.
type F12AttachmentBlobVerifier struct{ Files supply.F12AttachmentBlobStore }

func (v F12AttachmentBlobVerifier) VerifyF12Attachment(ctx context.Context, tx *sql.Tx, _ supply.F12AttachmentChain, uploadedBy, subjectType, subjectID, fileID, purpose string) error {
	if tx == nil || v.Files == nil {
		return supply.ErrDependencyUnavailable
	}
	return v.Files.Verify(ctx, fileID, subjectType, subjectID, purpose, uploadedBy)
}
