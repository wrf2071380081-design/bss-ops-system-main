package supplypg

import (
	"context"
	"strings"
	"testing"
)

func TestF1SchemaReportsMissingColumnsInsteadOfInventingDefaults(t *testing.T) {
	report := AssessF1Columns(nil)
	if len(report.MissingColumns) == 0 || report.ColumnsCompatible {
		t.Fatalf("empty schema accepted %+v", report)
	}
	joined := strings.Join(report.MissingColumns, ",")
	for _, field := range []string{"standard_model.name", "standard_model.status", "standard_model_version.snapshot", "operation_log.request_id"} {
		if !strings.Contains(joined, field) {
			t.Fatalf("missing %s from %s", field, joined)
		}
	}
}
func TestF1SchemaChecksActualTypesAndPrecision(t *testing.T) {
	columns := F1ExpectedColumns()
	for i := range columns {
		if columns[i].Table == "standard_model" && columns[i].Name == "id" {
			columns[i].Type = "timestamp with time zone"
		}
	}
	report := AssessF1Columns(columns)
	if report.ColumnsCompatible || len(report.IncompatibleColumns) != 1 {
		t.Fatalf("type mismatch hidden %+v", report)
	}
}

func TestF1SchemaAcceptsNative34WithoutControlColumns(t *testing.T) {
	columns := []F1Column{}
	for _, column := range F1ExpectedColumns() {
		if column.Table == "standard_model" && (column.Name == "maintainer_id" || column.Name == "revision" || column.Name == "version_no" || column.Name == "verify_status" || column.Name == "checked_by") {
			continue
		}
		if column.Table == "standard_model_version" && column.Name == "version_no" {
			column.Type = "integer"
		}
		columns = append(columns, column)
	}
	report := AssessF1Columns(columns)
	if !report.ColumnsCompatible || report.RepositoryReady {
		t.Fatalf("native34 wrongly requires extra columns %+v", report)
	}
}
func TestF1CompatibleColumnsDoNotClaimConstraintOrDataReadiness(t *testing.T) {
	report := AssessF1Columns(F1ExpectedColumns())
	if !report.ColumnsCompatible || report.RepositoryReady || len(report.RequiredChecks) == 0 {
		t.Fatalf("column presence overclaims readiness %+v", report)
	}
}
func TestF1InspectorRequiresLiveConnection(t *testing.T) {
	if _, err := InspectF1Schema(context.Background(), nil, "public"); err == nil {
		t.Fatal("nil DB accepted")
	}
}
