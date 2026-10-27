package supplypg

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"model_bss/internal/domain/supply"
)

func TestMapF12StorageErrorFailsClosedForMissingSchema(t *testing.T) {
	for _, code := range []string{"42703", "42P01", "42804", "42846", "42883"} {
		t.Run(code, func(t *testing.T) {
			err := mapF12StorageError(&pgconn.PgError{Code: code})
			if !errors.Is(err, supply.ErrDependencyUnavailable) {
				t.Fatalf("SQLSTATE %s must report unavailable schema, got %v", code, err)
			}
		})
	}
}

func TestMapF12StorageErrorPreservesConflictAndOwnershipSemantics(t *testing.T) {
	for _, test := range []struct {
		code string
		want error
	}{
		{code: "23505", want: supply.ErrRevisionConflict},
		{code: "40001", want: supply.ErrRevisionConflict},
		{code: "23503", want: supply.ErrForbidden},
	} {
		t.Run(test.code, func(t *testing.T) {
			err := mapF12StorageError(&pgconn.PgError{Code: test.code})
			if !errors.Is(err, test.want) {
				t.Fatalf("SQLSTATE %s mapped to %v, want %v", test.code, err, test.want)
			}
		})
	}
	if err := mapF12StorageError(sql.ErrNoRows); !errors.Is(err, supply.ErrNotFound) {
		t.Fatalf("missing row mapped to %v, want not found", err)
	}
}

func TestF12WriteSchemaCapabilityRejectsMissingOrIncompatibleColumns(t *testing.T) {
	columns := make(map[string]f12ColumnType, len(f12WriteColumns))
	for _, requirement := range f12WriteColumns {
		switch requirement.kind {
		case "text":
			columns[requirement.table+"."+requirement.column] = f12ColumnType{dataType: "text", udtName: "text", nullable: true}
		case "revision":
			columns[requirement.table+"."+requirement.column] = f12ColumnType{dataType: "integer", udtName: "int4", nullable: true}
		case "bigint":
			columns[requirement.table+"."+requirement.column] = f12ColumnType{dataType: "bigint", udtName: "int8", nullable: true}
		}
	}
	if !f12WriteSchemaCompatible(columns) {
		t.Fatal("compatible F12 additive columns must be accepted")
	}

	delete(columns, "contract.file_id")
	if f12WriteSchemaCompatible(columns) {
		t.Fatal("missing attachment ID column must be rejected")
	}

	columns["contract.file_id"] = f12ColumnType{dataType: "text", udtName: "text", nullable: true}
	columns["credit_line.source_flow_revision"] = f12ColumnType{dataType: "text", udtName: "text", nullable: true}
	if f12WriteSchemaCompatible(columns) {
		t.Fatal("text flow revision must be rejected as an incompatible schema")
	}
	columns["credit_line.source_flow_revision"] = f12ColumnType{dataType: "integer", udtName: "int4", nullable: false}
	if f12WriteSchemaCompatible(columns) {
		t.Fatal("non-null flow revision must be rejected because ordinary maintenance writes NULL")
	}
}

func TestF12VersionIndexCapabilityRequiresBothExactUniqueKeys(t *testing.T) {
	indexes := map[string]struct{}{
		"contract.contract_key,version": {},
		"credit_line.currency,direction,subject_id,subject_type,version": {},
	}
	if !f12VersionUniqueIndexesCompatible(indexes) {
		t.Fatal("both unique business-version keys must enable F12 writes")
	}

	delete(indexes, "contract.contract_key,version")
	if f12VersionUniqueIndexesCompatible(indexes) {
		t.Fatal("missing contract version uniqueness must disable F12 writes")
	}

	indexes["contract.contract_key,version_no"] = struct{}{}
	if f12VersionUniqueIndexesCompatible(indexes) {
		t.Fatal("a unique key over the wrong contract version column must not enable F12 writes")
	}

	delete(indexes, "contract.contract_key,version_no")
	indexes["contract.contract_key,version"] = struct{}{}
	delete(indexes, "credit_line.currency,direction,subject_id,subject_type,version")
	if f12VersionUniqueIndexesCompatible(indexes) {
		t.Fatal("missing credit version uniqueness must disable F12 writes")
	}
}
