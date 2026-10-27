package supplypg

import (
	"context"
	"database/sql"

	"model_bss/internal/domain/supply"
)

type f12SchemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type f12ColumnType struct {
	dataType string
	udtName  string
	nullable bool
}

type f12ColumnRequirement struct {
	table  string
	column string
	kind   string
}

var f12WriteColumns = []f12ColumnRequirement{
	{table: "contract", column: "contract_key", kind: "text"},
	{table: "contract", column: "document_name", kind: "text"},
	{table: "contract", column: "document_no", kind: "text"},
	{table: "contract", column: "file_id", kind: "text"},
	{table: "contract", column: "source_flow_id", kind: "text"},
	{table: "contract", column: "source_module_key", kind: "text"},
	{table: "contract", column: "source_flow_revision", kind: "revision"},
	{table: "contract", column: "reason", kind: "text"},
	{table: "contract", column: "created_by", kind: "bigint"},
	{table: "credit_line", column: "document_name", kind: "text"},
	{table: "credit_line", column: "document_no", kind: "text"},
	{table: "credit_line", column: "file_id", kind: "text"},
	{table: "credit_line", column: "source_flow_id", kind: "text"},
	{table: "credit_line", column: "source_module_key", kind: "text"},
	{table: "credit_line", column: "source_flow_revision", kind: "revision"},
	{table: "credit_line", column: "reason", kind: "text"},
	{table: "credit_line", column: "created_by", kind: "bigint"},
}

var f12VersionUniqueIndexKeys = []string{
	"contract.contract_key,version",
	"credit_line.currency,direction,subject_id,subject_type,version",
}

func f12RecordSchemaReady(ctx context.Context, queryer f12SchemaQueryer, schema string) (bool, error) {
	if queryer == nil || !modelSchemaName.MatchString(schema) {
		return false, supply.ErrDependencyUnavailable
	}
	rows, err := queryer.QueryContext(ctx, `SELECT table_name,column_name,data_type,udt_name,is_nullable
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name IN ('contract','credit_line')`, schema)
	if err != nil {
		return false, mapF12StorageError(err)
	}
	defer rows.Close()
	columns := make(map[string]f12ColumnType, len(f12WriteColumns))
	for rows.Next() {
		var table, column string
		var nullable string
		var columnType f12ColumnType
		if err := rows.Scan(&table, &column, &columnType.dataType, &columnType.udtName, &nullable); err != nil {
			return false, mapF12StorageError(err)
		}
		columnType.nullable = nullable == "YES"
		columns[table+"."+column] = columnType
	}
	if err := rows.Err(); err != nil {
		return false, mapF12StorageError(err)
	}
	if !f12WriteSchemaCompatible(columns) {
		return false, nil
	}
	indexes, err := f12VersionUniqueIndexes(ctx, queryer, schema)
	if err != nil {
		return false, err
	}
	return f12VersionUniqueIndexesCompatible(indexes), nil
}

func f12VersionUniqueIndexes(ctx context.Context, queryer f12SchemaQueryer, schema string) (map[string]struct{}, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT table_rel.relname,
		string_agg(attribute.attname::text, ',' ORDER BY attribute.attname::text)
		FROM pg_index AS index_info
		JOIN pg_class AS table_rel ON table_rel.oid=index_info.indrelid
		JOIN pg_namespace AS table_schema ON table_schema.oid=table_rel.relnamespace
		CROSS JOIN LATERAL unnest(index_info.indkey) WITH ORDINALITY AS key_column(attnum,ordinality)
		JOIN pg_attribute AS attribute ON attribute.attrelid=table_rel.oid AND attribute.attnum=key_column.attnum
		WHERE table_schema.nspname=$1
		  AND table_rel.relname IN ('contract','credit_line')
		  AND index_info.indisunique AND index_info.indisready AND index_info.indisvalid
		  AND (
		    (table_rel.relname='contract' AND (index_info.indpred IS NULL OR
		      lower(replace(replace(replace(replace(pg_get_expr(index_info.indpred,index_info.indrelid),' ',''),'"',''),'(',''),')',''))='contract_keyisnotnull'))
		    OR (table_rel.relname='credit_line' AND index_info.indpred IS NULL)
		  )
		  AND key_column.ordinality<=index_info.indnkeyatts
		GROUP BY table_rel.relname,index_info.indexrelid,index_info.indnkeyatts
		HAVING count(attribute.attname)=index_info.indnkeyatts`, schema)
	if err != nil {
		return nil, mapF12StorageError(err)
	}
	defer rows.Close()
	indexes := make(map[string]struct{}, len(f12VersionUniqueIndexKeys))
	for rows.Next() {
		var table, columns string
		if err := rows.Scan(&table, &columns); err != nil {
			return nil, mapF12StorageError(err)
		}
		indexes[table+"."+columns] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, mapF12StorageError(err)
	}
	return indexes, nil
}

func f12VersionUniqueIndexesCompatible(indexes map[string]struct{}) bool {
	for _, required := range f12VersionUniqueIndexKeys {
		if _, ok := indexes[required]; !ok {
			return false
		}
	}
	return true
}

func f12WriteSchemaCompatible(columns map[string]f12ColumnType) bool {
	for _, requirement := range f12WriteColumns {
		column, ok := columns[requirement.table+"."+requirement.column]
		if !ok || !column.nullable || !f12ColumnTypeCompatible(requirement.kind, column) {
			return false
		}
	}
	return true
}

func f12ColumnTypeCompatible(kind string, column f12ColumnType) bool {
	switch kind {
	case "text":
		return (column.dataType == "text" && column.udtName == "text") ||
			(column.dataType == "character varying" && column.udtName == "varchar")
	case "revision":
		return (column.dataType == "integer" && column.udtName == "int4") ||
			(column.dataType == "bigint" && column.udtName == "int8")
	case "bigint":
		return column.dataType == "bigint" && column.udtName == "int8"
	default:
		return false
	}
}
