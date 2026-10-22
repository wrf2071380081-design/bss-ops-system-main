package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

type F1Column struct{ Table, Name, Type string }
type F1SchemaReport struct {
	ColumnsCompatible   bool     `json:"columns_compatible"`
	RepositoryReady     bool     `json:"repository_ready"`
	MissingColumns      []string `json:"missing_columns"`
	IncompatibleColumns []string `json:"incompatible_columns"`
	IndexDefinitions    []string `json:"index_definitions"`
	RequiredChecks      []string `json:"required_checks"`
}

// Native 000033/34 columns. Business control metadata is persisted in the
// version snapshot; column presence alone does not validate that protocol.
func F1ExpectedColumns() []F1Column {
	result := []F1Column{}
	add := func(table, kind string, names ...string) {
		for _, name := range names {
			result = append(result, F1Column{table, name, kind})
		}
	}
	add("standard_model", "bigint", "id", "created_by", "updated_by")
	add("standard_model", "character varying", "company", "name", "official_id", "model_type", "status")
	add("standard_model", "ARRAY", "input_modalities", "output_modalities")
	add("standard_model", "jsonb", "capacity", "abilities", "price_items", "api_access", "lifecycle", "sources")
	add("standard_model", "date", "checked_at")
	add("standard_model", "timestamp with time zone", "created_at", "updated_at")
	add("standard_model_version", "bigint", "id", "standard_model_id")
	add("standard_model_version", "integer", "version_no")
	add("standard_model_version", "jsonb", "snapshot")
	add("operation_log", "bigint", "id", "entity_id", "operator_id")
	add("operation_log", "character varying", "entity_type", "action", "reason", "request_id")
	add("operation_log", "jsonb", "before", "after")
	add("operation_log", "timestamp with time zone", "created_at")
	return result
}
func AssessF1Columns(columns []F1Column) *F1SchemaReport {
	report := &F1SchemaReport{MissingColumns: []string{}, IncompatibleColumns: []string{}, IndexDefinitions: []string{}, RequiredChecks: []string{"schema34 identity uniqueness and explicit identity conflicts", "versioned snapshot metadata agrees with current projection", "legacy rows need reviewed snapshot initialization, not invented ownership", "varchar lengths and trace/reason limits", "locked snapshot CAS, shared transaction and commit-failure response"}}
	actual := map[string]string{}
	for _, column := range columns {
		actual[column.Table+"."+column.Name] = column.Type
	}
	for _, expected := range F1ExpectedColumns() {
		key := expected.Table + "." + expected.Name
		kind, ok := actual[key]
		if !ok {
			report.MissingColumns = append(report.MissingColumns, key)
		} else if kind != expected.Type && !(expected.Table == "standard_model_version" && expected.Name == "version_no" && kind == "bigint") {
			report.IncompatibleColumns = append(report.IncompatibleColumns, fmt.Sprintf("%s: expected %s, actual %s", key, expected.Type, kind))
		}
	}
	sort.Strings(report.MissingColumns)
	sort.Strings(report.IncompatibleColumns)
	report.ColumnsCompatible = len(report.MissingColumns) == 0 && len(report.IncompatibleColumns) == 0
	// Column checks never certify constraints, business data or transaction behavior.
	report.RepositoryReady = false
	return report
}

// InspectF1Schema reads only information_schema and pg_indexes with a bound
// schema parameter. It does not alter tables, connect from credentials, or enable routes.
func InspectF1Schema(ctx context.Context, db *sql.DB, schema string) (*F1SchemaReport, error) {
	if db == nil || schema == "" {
		return nil, errors.New("F1 schema inspection requires a live DB and explicit schema")
	}
	rows, err := db.QueryContext(ctx, `SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema=$1 AND table_name IN ('standard_model','standard_model_version','operation_log') ORDER BY table_name,ordinal_position`, schema)
	if err != nil {
		return nil, err
	}
	columns := []F1Column{}
	for rows.Next() {
		var column F1Column
		if err := rows.Scan(&column.Table, &column.Name, &column.Type); err != nil {
			rows.Close()
			return nil, err
		}
		columns = append(columns, column)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	report := AssessF1Columns(columns)
	indexes, err := db.QueryContext(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname=$1 AND tablename IN ('standard_model','standard_model_version','operation_log') ORDER BY tablename,indexname`, schema)
	if err != nil {
		return nil, err
	}
	defer indexes.Close()
	for indexes.Next() {
		var definition string
		if err := indexes.Scan(&definition); err != nil {
			return nil, err
		}
		report.IndexDefinitions = append(report.IndexDefinitions, definition)
	}
	if err := indexes.Err(); err != nil {
		return nil, err
	}
	return report, nil
}
