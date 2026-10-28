package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"model_bss/internal/domain/supply"
)

type F12ReadStore struct {
	schema string
	read   ModelReadRunner
}

var _ supply.F12ReadStore = (*F12ReadStore)(nil)

func NewF12ReadStore(schema string, read ModelReadRunner) (*F12ReadStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &F12ReadStore{schema: schema, read: read}, nil
}

func (s *F12ReadStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

func (s *F12ReadStore) subjectTable(subjectType string) (string, error) {
	switch subjectType {
	case "SUPPLIER":
		return s.table("supplier"), nil
	case "CUSTOMER":
		// The current internal customer workspace uses customer_profile.id.
		// The parallel legacy customer table from 000033 is not its identity source.
		return s.table("customer_profile"), nil
	default:
		return "", supply.ErrReadContract
	}
}

func f12Offset(page, size int) (int, error) {
	if page < 1 || size < 1 || size > 100 || page > int(^uint(0)>>1)/size {
		return 0, supply.ErrReadContract
	}
	return (page - 1) * size, nil
}

func (s *F12ReadStore) withSubject(ctx context.Context, tx *sql.Tx, subjectType, subjectID string) error {
	table, err := s.subjectTable(subjectType)
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM `+table+` WHERE id=$1`, subjectID).Scan(&id)
	if err == sql.ErrNoRows {
		return supply.ErrNotFound
	}
	if err != nil {
		return err
	}
	if id != subjectID {
		return supply.ErrReadContract
	}
	return nil
}

func (s *F12ReadStore) columns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2`, s.schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func f12TextProjection(columns map[string]bool, alias, name string) string {
	if !columns[name] {
		return `''::text`
	}
	return `COALESCE(` + alias + `.` + name + `::text,'')`
}

func f12RevisionProjection(columns map[string]bool, alias string) string {
	if !columns["source_flow_revision"] {
		return `0::bigint`
	}
	return `COALESCE(` + alias + `.source_flow_revision,0)::bigint`
}

func (s *F12ReadStore) ListF12Contracts(ctx context.Context, q supply.F12ContractQuery) ([]*supply.F12ContractRecord, int64, error) {
	if (q.ContractKey != "" && !modelIdentity(q.ContractKey)) || (q.LatestOnly && q.ContractKey == "") {
		return nil, 0, supply.ErrReadContract
	}
	if s == nil || s.read == nil {
		return nil, 0, supply.ErrDependencyUnavailable
	}
	offset, err := f12Offset(q.Page, q.Size)
	if err != nil || (q.SubjectType != "SUPPLIER" && q.SubjectType != "CUSTOMER") || !modelIdentity(q.SubjectID) || (q.Status != "" && q.Status != "PROCESSING" && q.Status != "ARCHIVED") {
		return nil, 0, supply.ErrReadContract
	}
	var records []*supply.F12ContractRecord
	var total int64
	err = (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, func(tx *sql.Tx) error {
		if err := s.withSubject(ctx, tx, q.SubjectType, q.SubjectID); err != nil {
			return err
		}
		columns, err := s.columns(ctx, tx, "contract")
		if err != nil {
			return err
		}
		if q.ContractKey != "" && !columns["contract_key"] {
			return supply.ErrDependencyUnavailable
		}
		where, args := f12ContractWhere(q)
		if q.LatestOnly {
			where += ` AND c.version=(SELECT max(latest.version) FROM ` + s.table("contract") + ` latest WHERE latest.subject_type=c.subject_type AND latest.subject_id=c.subject_id AND latest.contract_key=c.contract_key)`
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("contract")+` c WHERE `+where, args...).Scan(&total); err != nil {
			return err
		}
		queryArgs := append(append([]any{}, args...), q.Size, offset)
		contractKey := f12TextProjection(columns, "c", "contract_key")
		attachment := `(c.file_ref IS NOT NULL AND btrim(c.file_ref) <> '')`
		if columns["file_id"] {
			attachment = `(` + attachment + ` OR (c.file_id IS NOT NULL AND btrim(c.file_id::text) <> ''))`
		}
		isLatest := `false`
		if columns["contract_key"] {
			isLatest = `(c.contract_key IS NOT NULL AND c.version=(SELECT max(latest.version) FROM ` + s.table("contract") + ` latest WHERE latest.subject_type=c.subject_type AND latest.subject_id=c.subject_id AND latest.contract_key=c.contract_key))`
		}
		rows, err := tx.QueryContext(ctx, `SELECT c.id::text,`+contractKey+`,c.subject_type,c.subject_id::text,c.status,`+f12TextProjection(columns, "c", "document_name")+`,`+f12TextProjection(columns, "c", "document_no")+`,`+f12TextProjection(columns, "c", "file_id")+`,`+f12TextProjection(columns, "c", "source_flow_id")+`,`+f12TextProjection(columns, "c", "source_module_key")+`,`+f12RevisionProjection(columns, "c")+`,`+f12TextProjection(columns, "c", "reason")+`,`+f12TextProjection(columns, "c", "created_by")+`,`+isLatest+`,c.version,
`+attachment+`,c.signed_at,c.created_at,c.updated_at
FROM `+s.table("contract")+` c WHERE `+where+fmt.Sprintf(` ORDER BY c.created_at DESC,c.id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), queryArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		records = make([]*supply.F12ContractRecord, 0, q.Size)
		for rows.Next() {
			var row supply.F12ContractRecord
			var version, sourceFlowRevision int64
			var isLatest bool
			var signed sql.NullTime
			if err := rows.Scan(&row.ID, &row.ContractKey, &row.SubjectType, &row.SubjectID, &row.Status, &row.DocumentName, &row.DocumentNo, &row.FileID, &row.SourceFlowID, &row.SourceModuleKey, &sourceFlowRevision, &row.Reason, &row.CreatedBy, &isLatest, &version, &row.HasAttachment, &signed, &row.CreatedAt, &row.UpdatedAt); err != nil {
				return err
			}
			if version < 1 || sourceFlowRevision < 0 {
				return supply.ErrReadContract
			}
			row.Version = uint64(version)
			row.SourceFlowRevision = uint64(sourceFlowRevision)
			row.IsLatest = isLatest
			if signed.Valid {
				t := signed.Time.UTC()
				row.SignedAt = &t
			}
			records = append(records, &row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func f12ContractWhere(q supply.F12ContractQuery) (string, []any) {
	where := `c.subject_type=$1 AND c.subject_id=$2`
	args := []any{q.SubjectType, q.SubjectID}
	if q.Status != "" {
		args = append(args, q.Status)
		where += fmt.Sprintf(" AND c.status=$%d", len(args))
	}
	if q.ContractKey != "" {
		args = append(args, q.ContractKey)
		where += fmt.Sprintf(" AND c.contract_key=$%d", len(args))
	}
	return where, args
}

func f12CreditWhere(q supply.F12CreditQuery) (string, []any) {
	where := `c.subject_type=$1 AND c.subject_id=$2`
	args := []any{q.SubjectType, q.SubjectID}
	if q.Direction != "" {
		args = append(args, q.Direction)
		where += fmt.Sprintf(" AND c.direction=$%d", len(args))
	}
	if q.Currency != "" {
		args = append(args, q.Currency)
		where += fmt.Sprintf(" AND c.currency=$%d", len(args))
	}
	if q.AsOf != nil {
		args = append(args, q.AsOf.UTC())
		where += fmt.Sprintf(" AND c.created_at <= $%d", len(args))
	}
	return where, args
}

func f12CreditProjection(columns map[string]bool) string {
	attachment := `false`
	if columns["file_id"] {
		attachment = `(c.file_id IS NOT NULL AND btrim(c.file_id::text) <> '')`
	}
	return `,` + f12TextProjection(columns, "c", "document_name") + `,` + f12TextProjection(columns, "c", "document_no") + `,` + f12TextProjection(columns, "c", "file_id") + `,` + f12TextProjection(columns, "c", "source_flow_id") + `,` + f12TextProjection(columns, "c", "source_module_key") + `,` + f12RevisionProjection(columns, "c") + `,` + attachment + `,` + f12TextProjection(columns, "c", "reason") + `,` + f12TextProjection(columns, "c", "created_by")
}

func f12LatestCreditQuery(table, where string, args []any, metadata string) (string, []any) {
	query := `SELECT c.id::text,c.subject_type,c.subject_id::text,c.direction,c.amount::text,c.currency,c.version,c.effective_at,c.created_at` + metadata + `
FROM ` + table + ` c JOIN (
 SELECT direction,currency,MAX(version) AS version FROM ` + table + ` c WHERE ` + where + ` GROUP BY direction,currency
) current ON current.direction=c.direction AND current.currency=c.currency AND current.version=c.version
WHERE ` + where + ` ORDER BY c.direction,c.currency,c.id`
	// The same predicate and placeholders appear in both SELECT scopes, so one
	// binding list serves both occurrences.
	return query, append([]any(nil), args...)
}

func scanF12Credit(scanner interface{ Scan(...any) error }) (*supply.F12CreditRecord, error) {
	var row supply.F12CreditRecord
	var version, sourceFlowRevision int64
	var effective sql.NullTime
	if err := scanner.Scan(&row.ID, &row.SubjectType, &row.SubjectID, &row.Direction, &row.Amount, &row.Currency, &version, &effective, &row.CreatedAt, &row.DocumentName, &row.DocumentNo, &row.FileID, &row.SourceFlowID, &row.SourceModuleKey, &sourceFlowRevision, &row.HasAttachment, &row.Reason, &row.CreatedBy); err != nil {
		return nil, err
	}
	if version < 1 || sourceFlowRevision < 0 {
		return nil, supply.ErrReadContract
	}
	row.Version = uint64(version)
	row.SourceFlowRevision = uint64(sourceFlowRevision)
	if effective.Valid {
		t := effective.Time.UTC()
		row.EffectiveAt = &t
	}
	return &row, nil
}

func (s *F12ReadStore) ListF12Credits(ctx context.Context, q supply.F12CreditQuery) ([]*supply.F12CreditRecord, int64, []*supply.F12CreditRecord, error) {
	if s == nil || s.read == nil {
		return nil, 0, nil, supply.ErrDependencyUnavailable
	}
	offset, err := f12Offset(q.Page, q.Size)
	expectedDirection := "SUPPLIER_TO_US"
	if q.SubjectType == "CUSTOMER" {
		expectedDirection = "US_TO_CUSTOMER"
	}
	if err != nil || (q.SubjectType != "SUPPLIER" && q.SubjectType != "CUSTOMER") || !modelIdentity(q.SubjectID) || (q.Direction != "" && q.Direction != expectedDirection) || (q.Currency != "" && len(q.Currency) != 3) {
		return nil, 0, nil, supply.ErrReadContract
	}
	var records, latest []*supply.F12CreditRecord
	var total int64
	err = (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, func(tx *sql.Tx) error {
		if err := s.withSubject(ctx, tx, q.SubjectType, q.SubjectID); err != nil {
			return err
		}
		columns, err := s.columns(ctx, tx, "credit_line")
		if err != nil {
			return err
		}
		where, args := f12CreditWhere(q)
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("credit_line")+` c WHERE `+where, args...).Scan(&total); err != nil {
			return err
		}
		queryArgs := append(append([]any{}, args...), q.Size, offset)
		metadata := f12CreditProjection(columns)
		query := `SELECT c.id::text,c.subject_type,c.subject_id::text,c.direction,c.amount::text,c.currency,c.version,c.effective_at,c.created_at` + metadata + `
FROM ` + s.table("credit_line") + ` c WHERE ` + where + fmt.Sprintf(` ORDER BY c.created_at DESC,c.id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
		rows, err := tx.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return err
		}
		records = make([]*supply.F12CreditRecord, 0, q.Size)
		for rows.Next() {
			row, scanErr := scanF12Credit(rows)
			if scanErr != nil {
				rows.Close()
				return scanErr
			}
			records = append(records, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		latestQuery, latestArgs := f12LatestCreditQuery(s.table("credit_line"), where, args, metadata)
		latestRows, err := tx.QueryContext(ctx, latestQuery, latestArgs...)
		if err != nil {
			return err
		}
		defer latestRows.Close()
		latest = make([]*supply.F12CreditRecord, 0)
		for latestRows.Next() {
			row, scanErr := scanF12Credit(latestRows)
			if scanErr != nil {
				return scanErr
			}
			latest = append(latest, row)
		}
		return latestRows.Err()
	})
	if err != nil {
		return nil, 0, nil, err
	}
	return records, total, latest, nil
}

// ListF12ReadinessEvidence loads only current contract/credit records whose
// source is the requested B/C flow module. Old schemas without flow revision
// metadata fail closed instead of treating legacy records as first-flow proof.
func (s *F12ReadStore) ListF12ReadinessEvidence(ctx context.Context, request supply.F12ReadinessRequest) ([]*supply.F12ContractRecord, []*supply.F12CreditRecord, error) {
	if s == nil || s.read == nil {
		return nil, nil, supply.ErrDependencyUnavailable
	}
	if (request.SubjectType != "SUPPLIER" && request.SubjectType != "CUSTOMER") || !modelIdentity(request.SubjectID) || !modelIdentity(request.FlowID) || strings.TrimSpace(request.ModuleKey) == "" {
		return nil, nil, supply.ErrReadContract
	}
	var contractRows []*supply.F12ContractRecord
	var creditRows []*supply.F12CreditRecord
	err := (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, func(tx *sql.Tx) error {
		if err := s.withSubject(ctx, tx, request.SubjectType, request.SubjectID); err != nil {
			return err
		}
		for table, required := range map[string][]string{
			"contract":    {"contract_key", "document_name", "document_no", "file_id", "source_flow_id", "source_module_key", "source_flow_revision"},
			"credit_line": {"document_name", "document_no", "file_id", "source_flow_id", "source_module_key", "source_flow_revision"},
		} {
			columns, err := s.columns(ctx, tx, table)
			if err != nil {
				return err
			}
			for _, column := range required {
				if !columns[column] {
					return supply.ErrDependencyUnavailable
				}
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT c.id::text,c.contract_key::text,c.subject_type,c.subject_id::text,c.status,COALESCE(c.document_name,''),COALESCE(c.document_no,''),COALESCE(c.file_id::text,''),COALESCE(c.source_flow_id::text,''),COALESCE(c.source_module_key,''),COALESCE(c.source_flow_revision,0)::bigint,COALESCE(c.reason,''),COALESCE(c.created_by::text,''),c.version,(c.file_id IS NOT NULL AND btrim(c.file_id::text)<>''),c.created_at,c.updated_at,
(c.version=(SELECT max(latest.version) FROM `+s.table("contract")+` latest WHERE latest.contract_key=c.contract_key))
FROM `+s.table("contract")+` c WHERE c.subject_type=$1 AND c.subject_id=$2 AND c.source_flow_id::text=$3 AND c.source_module_key=$4 AND c.contract_key IS NOT NULL ORDER BY c.contract_key,c.version`, request.SubjectType, request.SubjectID, request.FlowID, request.ModuleKey)
		if err != nil {
			return err
		}
		contractRows = make([]*supply.F12ContractRecord, 0)
		for rows.Next() {
			var row supply.F12ContractRecord
			var version, flowRevision int64
			var latest bool
			if err := rows.Scan(&row.ID, &row.ContractKey, &row.SubjectType, &row.SubjectID, &row.Status, &row.DocumentName, &row.DocumentNo, &row.FileID, &row.SourceFlowID, &row.SourceModuleKey, &flowRevision, &row.Reason, &row.CreatedBy, &version, &row.HasAttachment, &row.CreatedAt, &row.UpdatedAt, &latest); err != nil {
				_ = rows.Close()
				return err
			}
			if version < 1 || flowRevision < 1 {
				_ = rows.Close()
				return supply.ErrReadContract
			}
			row.Version, row.SourceFlowRevision, row.IsLatest = uint64(version), uint64(flowRevision), latest
			contractRows = append(contractRows, &row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT c.id::text,c.subject_type,c.subject_id::text,c.direction,c.amount::text,c.currency,c.version,c.effective_at,c.created_at,COALESCE(c.document_name,''),COALESCE(c.document_no,''),COALESCE(c.file_id::text,''),COALESCE(c.source_flow_id::text,''),COALESCE(c.source_module_key,''),COALESCE(c.source_flow_revision,0)::bigint,(c.file_id IS NOT NULL AND btrim(c.file_id::text)<>''),COALESCE(c.reason,''),COALESCE(c.created_by::text,'')
FROM `+s.table("credit_line")+` c WHERE c.subject_type=$1 AND c.subject_id=$2 AND c.source_flow_id::text=$3 AND c.source_module_key=$4
AND c.version=(SELECT max(latest.version) FROM `+s.table("credit_line")+` latest WHERE latest.subject_type=c.subject_type AND latest.subject_id=c.subject_id AND latest.direction=c.direction AND latest.currency=c.currency)
ORDER BY c.direction,c.currency`, request.SubjectType, request.SubjectID, request.FlowID, request.ModuleKey)
		if err != nil {
			return err
		}
		creditRows = make([]*supply.F12CreditRecord, 0)
		for rows.Next() {
			var row supply.F12CreditRecord
			var version, flowRevision int64
			var effective sql.NullTime
			if err := rows.Scan(&row.ID, &row.SubjectType, &row.SubjectID, &row.Direction, &row.Amount, &row.Currency, &version, &effective, &row.CreatedAt, &row.DocumentName, &row.DocumentNo, &row.FileID, &row.SourceFlowID, &row.SourceModuleKey, &flowRevision, &row.HasAttachment, &row.Reason, &row.CreatedBy); err != nil {
				_ = rows.Close()
				return err
			}
			if version < 1 || flowRevision < 1 {
				_ = rows.Close()
				return supply.ErrReadContract
			}
			row.Version, row.SourceFlowRevision = uint64(version), uint64(flowRevision)
			if effective.Valid {
				t := effective.Time.UTC()
				row.EffectiveAt = &t
			}
			creditRows = append(creditRows, &row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return nil, nil, mapF12StorageError(err)
	}
	return contractRows, creditRows, nil
}
