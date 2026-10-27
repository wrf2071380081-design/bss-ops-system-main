package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"model_bss/internal/domain/supply"
)

// F12FlowWriteAuthorizer validates first-onboarding writes against a trusted
// B/C flow fact using the same SQL transaction as the F12 record. For CUSTOMER,
// the F12 subject is customer_profile.id while onboarding_flow.owner_id is
// defined against customer.id in the legacy shared schema. Implementations must
// resolve and validate that relation under the transaction; numeric ID equality
// alone is not an identity mapping.
type F12FlowWriteAuthorizer interface {
	// Flow write authorization returns the authoritative flow revision read under
	// the same transaction after subject/module/assignee/status checks. A positive
	// expectedRevision is a client CAS token and must match; zero is reserved for
	// no-flow compatibility only. It is never the authoritative revision.
	AuthorizeF12FlowWrite(context.Context, *sql.Tx, string, string, string, string, string, uint64) (uint64, error)
	// Flow reads verify the authenticated actor against the current authorized
	// handler/record-access fact. This does not permit a state-changing write.
	AuthorizeF12FlowRead(context.Context, *sql.Tx, string, string, string, string, string) error
	// Ordinary maintenance is allowed only when the provider confirms there is no
	// unfinished first-onboarding business module that must use flow assignment.
	AuthorizeF12OrdinaryWrite(context.Context, *sql.Tx, string, string, string) error
}

// F12AttachmentVerifier checks an uploaded D07 file's subject and purpose
// metadata inside the same transaction. Implementations must not download or
// transfer file contents while the subject row is locked.
type F12AttachmentVerifier interface {
	VerifyF12Attachment(context.Context, *sql.Tx, supply.F12AttachmentChain, string, string, string, string, string) error
}

// F12RecordStore is an adapter for the proposed F12 record columns. It never
// runs DDL; databases without the approved columns return a dependency error.
type F12RecordStore struct {
	schema          string
	resolve         ModelTxResolver
	flowAuthorizer  F12FlowWriteAuthorizer
	attachmentCheck F12AttachmentVerifier
}

var _ supply.F12RecordUnitOfWork = (*F12RecordStore)(nil)

var f12RecordSavepointSequence atomic.Uint64

func NewF12RecordStore(schema string, resolve ModelTxResolver, flowAuthorizer F12FlowWriteAuthorizer, attachmentCheck F12AttachmentVerifier) (*F12RecordStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	if resolve == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return &F12RecordStore{schema: schema, resolve: resolve, flowAuthorizer: flowAuthorizer, attachmentCheck: attachmentCheck}, nil
}

func (s *F12RecordStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

// Within isolates each F12 domain write in a savepoint inside the caller's
// idempotency transaction. A failed operation can roll back its partial record
// and log writes while leaving the outer transaction available to persist the
// FAILED idempotency result.
func (s *F12RecordStore) Within(ctx context.Context, run func(supply.F12RecordTransaction) error) (result error) {
	if s == nil || s.resolve == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.resolve(ctx)
	if err != nil {
		return mapF12StorageError(err)
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	ready, err := f12RecordSchemaReady(ctx, tx, s.schema)
	if err != nil {
		return err
	}
	if !ready {
		return supply.ErrDependencyUnavailable
	}
	savepoint := fmt.Sprintf("f12_record_%d", f12RecordSavepointSequence.Add(1))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return mapF12StorageError(err)
	}
	savepointOpen := true
	defer func() {
		if !savepointOpen {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := tx.ExecContext(cleanupCtx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
			_ = tx.Rollback()
			if result == nil {
				result = mapF12StorageError(err)
			}
			return
		}
		if _, err := tx.ExecContext(cleanupCtx, "RELEASE SAVEPOINT "+savepoint); err != nil {
			_ = tx.Rollback()
			if result == nil {
				result = mapF12StorageError(err)
			}
			return
		}
		savepointOpen = false
	}()
	if err := run(&f12RecordTransaction{store: s, tx: tx}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return mapF12StorageError(err)
	}
	savepointOpen = false
	return nil
}

type f12RecordTransaction struct {
	store *F12RecordStore
	tx    *sql.Tx
}

func (t *f12RecordTransaction) NextID(ctx context.Context, kind supply.F12RecordIDKind) (string, error) {
	if t == nil || t.store == nil || t.tx == nil {
		return "", supply.ErrDependencyUnavailable
	}
	var table string
	switch kind {
	case supply.F12ContractRecordID:
		table = "contract"
	case supply.F12CreditRecordID:
		table = "credit_line"
	default:
		return "", supply.ErrReadContract
	}
	var id string
	err := t.tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id'))::text`, t.store.table(table)).Scan(&id)
	if err != nil {
		return "", mapF12StorageError(err)
	}
	if !modelIdentity(id) {
		return "", supply.ErrReadContract
	}
	return id, nil
}

func f12SubjectTable(subjectType string) (string, error) {
	switch subjectType {
	case "SUPPLIER":
		return "supplier", nil
	case "CUSTOMER":
		return "customer_profile", nil
	default:
		return "", supply.ErrReadContract
	}
}

func (t *f12RecordTransaction) ContractSubject(ctx context.Context, contractKey string) (*supply.F12RecordSubject, error) {
	if t == nil || t.store == nil || t.tx == nil || !modelIdentity(contractKey) {
		return nil, supply.ErrReadContract
	}
	var subject supply.F12RecordSubject
	err := t.tx.QueryRowContext(ctx, `SELECT subject_type,subject_id::text FROM `+t.store.table("contract")+` WHERE contract_key=$1 ORDER BY version,id LIMIT 1`, contractKey).Scan(&subject.Type, &subject.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	if (subject.Type != "SUPPLIER" && subject.Type != "CUSTOMER") || !modelIdentity(subject.ID) {
		return nil, supply.ErrReadContract
	}
	return &subject, nil
}

func (t *f12RecordTransaction) SubjectForUpdate(ctx context.Context, subjectType, subjectID string) (*supply.F12RecordSubject, error) {
	if t == nil || t.store == nil || t.tx == nil || !modelIdentity(subjectID) {
		return nil, supply.ErrReadContract
	}
	name, err := f12SubjectTable(subjectType)
	if err != nil {
		return nil, err
	}
	var id string
	err = t.tx.QueryRowContext(ctx, `SELECT id::text FROM `+t.store.table(name)+` WHERE id=$1 FOR UPDATE`, subjectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	return &supply.F12RecordSubject{Type: subjectType, ID: id}, nil
}

func (t *f12RecordTransaction) AuthorizeF12Write(ctx context.Context, actorID, subjectType, subjectID, flowID, moduleKey string, expectedFlowRevision uint64) (uint64, error) {
	if t == nil || t.store == nil || t.tx == nil || !modelIdentity(actorID) {
		return 0, supply.ErrDependencyUnavailable
	}
	var active bool
	if err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+t.store.table("internal_staff")+` WHERE id=$1 AND status='ACTIVE')`, actorID).Scan(&active); err != nil {
		return 0, mapF12StorageError(err)
	}
	if !active {
		return 0, supply.ErrForbidden
	}
	if t.store.flowAuthorizer == nil {
		return 0, supply.ErrDependencyUnavailable
	}
	if flowID != "" || moduleKey != "" {
		if flowID == "" || moduleKey == "" || expectedFlowRevision == 0 || expectedFlowRevision > math.MaxInt32 || t.store.flowAuthorizer == nil {
			return 0, supply.ErrDependencyUnavailable
		}
		revision, err := t.store.flowAuthorizer.AuthorizeF12FlowWrite(ctx, t.tx, actorID, subjectType, subjectID, flowID, moduleKey, expectedFlowRevision)
		if err != nil {
			return 0, err
		}
		if revision == 0 || revision > math.MaxInt32 {
			return 0, supply.ErrReadContract
		}
		return revision, nil
	}
	if err := t.store.flowAuthorizer.AuthorizeF12OrdinaryWrite(ctx, t.tx, actorID, subjectType, subjectID); err != nil {
		return 0, err
	}
	if expectedFlowRevision != 0 {
		return 0, supply.ErrReadContract
	}

	if err := t.authorizeSubjectOwner(ctx, actorID, subjectType, subjectID); err != nil {
		return 0, err
	}
	return 0, nil
}

func (t *f12RecordTransaction) AuthorizeF12Read(ctx context.Context, actorID, subjectType, subjectID, flowID, moduleKey string) error {
	if t == nil || t.store == nil || t.tx == nil || !modelIdentity(actorID) || !modelIdentity(subjectID) {
		return supply.ErrDependencyUnavailable
	}
	var active bool
	if err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+t.store.table("internal_staff")+` WHERE id=$1 AND status='ACTIVE')`, actorID).Scan(&active); err != nil {
		return mapF12StorageError(err)
	}
	if !active {
		return supply.ErrForbidden
	}
	if flowID != "" || moduleKey != "" {
		if flowID == "" || moduleKey == "" || t.store.flowAuthorizer == nil {
			return supply.ErrDependencyUnavailable
		}
		return t.store.flowAuthorizer.AuthorizeF12FlowRead(ctx, t.tx, actorID, subjectType, subjectID, flowID, moduleKey)
	}
	return t.authorizeSubjectOwner(ctx, actorID, subjectType, subjectID)
}

func (t *f12RecordTransaction) authorizeSubjectOwner(ctx context.Context, actorID, subjectType, subjectID string) error {
	var ownerID sql.NullString
	switch subjectType {
	case "SUPPLIER":
		currentBuyer, err := t.currentSupplierBuyer(ctx, t.tx, subjectID)
		if err != nil {
			return mapF12StorageError(err)
		}
		ownerID = sql.NullString{String: currentBuyer, Valid: currentBuyer != ""}
	case "CUSTOMER":
		err := t.tx.QueryRowContext(ctx, `SELECT owner_sales_operator_id::text FROM `+t.store.table("customer_profile")+` WHERE id=$1`, subjectID).Scan(&ownerID)
		if errors.Is(err, sql.ErrNoRows) {
			return supply.ErrNotFound
		}
		if err != nil {
			return mapF12StorageError(err)
		}
	default:
		return supply.ErrReadContract
	}
	if !ownerID.Valid || ownerID.String != actorID {
		return supply.ErrForbidden
	}
	return nil
}

func (t *f12RecordTransaction) currentSupplierBuyer(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	return f12CurrentSupplierBuyer(ctx, tx, t.store.table("assignment_log"), id)
}

func f12CurrentSupplierBuyer(ctx context.Context, tx *sql.Tx, assignmentTable, id string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT old_staff_id::text,new_staff_id::text,changed_by::text FROM `+assignmentTable+` WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id' ORDER BY id`, id)
	if err != nil {
		return "", mapF12StorageError(err)
	}
	defer rows.Close()
	var current string
	for rows.Next() {
		var old, next, changedBy sql.NullString
		if err := rows.Scan(&old, &next, &changedBy); err != nil {
			return "", mapF12StorageError(err)
		}
		if (current == "" && old.Valid) || (current != "" && (!old.Valid || old.String != current)) || !next.Valid || !changedBy.Valid || !modelIdentity(next.String) || !modelIdentity(changedBy.String) {
			return "", supply.ErrReadContract
		}
		current = next.String
	}
	if err := rows.Err(); err != nil {
		return "", mapF12StorageError(err)
	}
	if current == "" {
		return "", supply.ErrReadContract
	}
	return current, nil
}

func (t *f12RecordTransaction) VerifyF12Attachment(ctx context.Context, uploadedBy, subjectType, subjectID, fileID, purpose string, chain supply.F12AttachmentChain) error {
	if t == nil || t.store == nil || t.tx == nil || t.store.attachmentCheck == nil {
		return supply.ErrDependencyUnavailable
	}
	if !modelIdentity(subjectID) || strings.TrimSpace(fileID) == "" || (subjectType != "SUPPLIER" && subjectType != "CUSTOMER") || !validF12AttachmentChain(subjectType, purpose, chain) {
		return supply.ErrReadContract
	}
	var referencedElsewhere bool
	var err error
	if purpose == "CONTRACT" {
		err = t.tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM `+t.store.table("contract")+` WHERE file_id::text=$1 AND ($2='' OR contract_key::text<>$2)
			UNION ALL
			SELECT 1 FROM `+t.store.table("credit_line")+` WHERE file_id::text=$1
		)`, fileID, chain.ContractKey).Scan(&referencedElsewhere)
	} else {
		err = t.tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM `+t.store.table("contract")+` WHERE file_id::text=$1
			UNION ALL
			SELECT 1 FROM `+t.store.table("credit_line")+` WHERE file_id::text=$1 AND
				(subject_type<>$2 OR subject_id::text<>$3 OR direction<>$4 OR currency<>$5)
		)`, fileID, subjectType, subjectID, chain.Direction, chain.Currency).Scan(&referencedElsewhere)
	}
	if err != nil {
		return mapF12StorageError(err)
	}
	if referencedElsewhere {
		return supply.ErrForbidden
	}
	return t.store.attachmentCheck.VerifyF12Attachment(ctx, t.tx, chain, uploadedBy, subjectType, subjectID, fileID, purpose)
}

func validF12AttachmentChain(subjectType, purpose string, chain supply.F12AttachmentChain) bool {
	switch purpose {
	case "CONTRACT":
		return chain.Kind == "CONTRACT" && (chain.ContractKey == "" || modelIdentity(chain.ContractKey)) && chain.Direction == "" && chain.Currency == ""
	case "CREDIT":
		expectedDirection := "SUPPLIER_TO_US"
		if subjectType == "CUSTOMER" {
			expectedDirection = "US_TO_CUSTOMER"
		}
		return chain.Kind == "CREDIT" && chain.ContractKey == "" && chain.Direction == expectedDirection && f12AttachmentCurrency.MatchString(chain.Currency)
	default:
		return false
	}
}

func (t *f12RecordTransaction) ContractVersionsForUpdate(ctx context.Context, contractKey string) ([]*supply.F12ContractVersion, error) {
	if t == nil || t.store == nil || t.tx == nil || !modelIdentity(contractKey) {
		return nil, supply.ErrReadContract
	}
	rows, err := t.tx.QueryContext(ctx, `SELECT id::text,contract_key::text,subject_type,subject_id::text,status,COALESCE(document_name,''),COALESCE(document_no,''),COALESCE(file_id::text,''),COALESCE(source_flow_id::text,''),COALESCE(source_module_key,''),COALESCE(source_flow_revision,0)::bigint,COALESCE(reason,''),COALESCE(created_by::text,''),version,created_at FROM `+t.store.table("contract")+` WHERE contract_key=$1 ORDER BY version,id FOR UPDATE`, contractKey)
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	defer rows.Close()
	result := make([]*supply.F12ContractVersion, 0)
	for rows.Next() {
		var row supply.F12ContractVersion
		var version, sourceFlowRevision int64
		if err := rows.Scan(&row.ID, &row.ContractKey, &row.SubjectType, &row.SubjectID, &row.Status, &row.DocumentName, &row.DocumentNo, &row.FileID, &row.SourceFlowID, &row.SourceModuleKey, &sourceFlowRevision, &row.Reason, &row.CreatedBy, &version, &row.CreatedAt); err != nil {
			return nil, mapF12StorageError(err)
		}
		if version < 1 || sourceFlowRevision < 0 {
			return nil, supply.ErrReadContract
		}
		row.Version = uint64(version)
		row.SourceFlowRevision = uint64(sourceFlowRevision)
		row.CreatedAt = row.CreatedAt.UTC()
		result = append(result, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapF12StorageError(err)
	}
	return result, nil
}

func (t *f12RecordTransaction) CreditVersionsForUpdate(ctx context.Context, subjectType, subjectID, direction, currency string) ([]*supply.F12CreditVersion, error) {
	if t == nil || t.store == nil || t.tx == nil || !modelIdentity(subjectID) {
		return nil, supply.ErrReadContract
	}
	rows, err := t.tx.QueryContext(ctx, `SELECT id::text,subject_type,subject_id::text,direction,amount::text,currency,COALESCE(document_name,''),COALESCE(document_no,''),COALESCE(file_id::text,''),COALESCE(source_flow_id::text,''),COALESCE(source_module_key,''),COALESCE(source_flow_revision,0)::bigint,COALESCE(reason,''),COALESCE(created_by::text,''),version,created_at FROM `+t.store.table("credit_line")+` WHERE subject_type=$1 AND subject_id=$2 AND direction=$3 AND currency=$4 ORDER BY version,id FOR UPDATE`, subjectType, subjectID, direction, currency)
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	defer rows.Close()
	result := make([]*supply.F12CreditVersion, 0)
	for rows.Next() {
		var row supply.F12CreditVersion
		var version, sourceFlowRevision int64
		if err := rows.Scan(&row.ID, &row.SubjectType, &row.SubjectID, &row.Direction, &row.Amount, &row.Currency, &row.DocumentName, &row.DocumentNo, &row.FileID, &row.SourceFlowID, &row.SourceModuleKey, &sourceFlowRevision, &row.Reason, &row.CreatedBy, &version, &row.CreatedAt); err != nil {
			return nil, mapF12StorageError(err)
		}
		if version < 1 || sourceFlowRevision < 0 {
			return nil, supply.ErrReadContract
		}
		row.Version = uint64(version)
		row.SourceFlowRevision = uint64(sourceFlowRevision)
		row.CreatedAt = row.CreatedAt.UTC()
		result = append(result, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapF12StorageError(err)
	}
	return result, nil
}

func (t *f12RecordTransaction) AppendF12ContractVersion(ctx context.Context, row *supply.F12ContractVersion) error {
	if t == nil || t.store == nil || t.tx == nil || row == nil || !modelIdentity(row.ID) || !modelIdentity(row.ContractKey) || !modelIdentity(row.SubjectID) || !modelIdentity(row.CreatedBy) || row.Version == 0 || row.Version > math.MaxInt32 || row.CreatedAt.IsZero() || !validF12SourceStorage(row.SourceFlowID, row.SourceModuleKey, row.SourceFlowRevision) {
		return supply.ErrReadContract
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("contract")+` (id,contract_key,subject_type,subject_id,status,version,document_name,document_no,file_id,source_flow_id,source_module_key,source_flow_revision,reason,created_by,created_at,updated_at) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,0),$13,$14,$15,$15)`, row.ID, row.ContractKey, row.SubjectType, row.SubjectID, row.Status, row.Version, nullableF12Text(row.DocumentName), nullableF12Text(row.DocumentNo), nullableF12Text(row.FileID), nullableF12Text(row.SourceFlowID), nullableF12Text(row.SourceModuleKey), int64(row.SourceFlowRevision), nullableF12Text(row.Reason), row.CreatedBy, row.CreatedAt.UTC())
	return mapF12StorageError(err)
}

func (t *f12RecordTransaction) AppendF12CreditVersion(ctx context.Context, row *supply.F12CreditVersion) error {
	if t == nil || t.store == nil || t.tx == nil || row == nil || !modelIdentity(row.ID) || !modelIdentity(row.SubjectID) || !modelIdentity(row.CreatedBy) || row.Version == 0 || row.Version > math.MaxInt32 || row.CreatedAt.IsZero() || !validF12SourceStorage(row.SourceFlowID, row.SourceModuleKey, row.SourceFlowRevision) {
		return supply.ErrReadContract
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("credit_line")+` (id,subject_type,subject_id,direction,amount,currency,version,document_name,document_no,file_id,source_flow_id,source_module_key,source_flow_revision,reason,created_by,created_at) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,0),$14,$15,$16)`, row.ID, row.SubjectType, row.SubjectID, row.Direction, row.Amount, row.Currency, row.Version, nullableF12Text(row.DocumentName), nullableF12Text(row.DocumentNo), nullableF12Text(row.FileID), nullableF12Text(row.SourceFlowID), nullableF12Text(row.SourceModuleKey), int64(row.SourceFlowRevision), nullableF12Text(row.Reason), row.CreatedBy, row.CreatedAt.UTC())
	return mapF12StorageError(err)
}

func validF12SourceStorage(flowID, moduleKey string, revision uint64) bool {
	if flowID == "" && moduleKey == "" {
		return revision == 0
	}
	return modelIdentity(flowID) && strings.TrimSpace(moduleKey) != "" && revision > 0 && revision <= math.MaxInt32
}

func (t *f12RecordTransaction) AppendF12Operation(ctx context.Context, op supply.F12RecordOperation) error {
	if t == nil || t.store == nil || t.tx == nil || (op.EntityType != "contract" && op.EntityType != "credit_line") || !modelIdentity(op.EntityID) || !modelIdentity(op.ActorID) || strings.TrimSpace(op.Action) == "" || len(op.Action) > 32 || strings.TrimSpace(op.Reason) == "" || strings.TrimSpace(op.RequestID) == "" || op.OccurredAt.IsZero() {
		return supply.ErrReadContract
	}
	var before []byte
	var after []byte
	var err error
	if op.BeforeContract != nil {
		before, err = json.Marshal(op.BeforeContract)
		if err != nil {
			return supply.ErrReadContract
		}
	}
	if op.AfterContract != nil {
		after, err = json.Marshal(op.AfterContract)
		if err != nil {
			return supply.ErrReadContract
		}
	}
	if op.BeforeCredit != nil {
		before, err = json.Marshal(op.BeforeCredit)
		if err != nil {
			return supply.ErrReadContract
		}
	}
	if op.AfterCredit != nil {
		after, err = json.Marshal(op.AfterCredit)
		if err != nil {
			return supply.ErrReadContract
		}
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("operation_log")+` (entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,(SELECT name FROM `+t.store.table("internal_staff")+` WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`, op.EntityType, op.EntityID, op.Action, op.ActorID, nullableF12JSON(before), nullableF12JSON(after), op.Reason, op.RequestID, op.OccurredAt.UTC())
	return mapF12StorageError(err)
}

func nullableF12Text(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableF12JSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func mapF12StorageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "40001":
			return supply.ErrRevisionConflict
		case "23503":
			return supply.ErrForbidden
		case "42703", "42P01", "42804", "42846", "42883":
			return fmt.Errorf("%w: F12 schema/provider is not ready", supply.ErrDependencyUnavailable)
		}
	}
	return err
}
