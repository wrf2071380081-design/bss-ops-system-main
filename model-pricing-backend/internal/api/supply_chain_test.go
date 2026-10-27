package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSupplierWorkspaceListQueryUsesV36BusinessFacts(t *testing.T) {
	query := supplierWorkspaceListQuery(false)
	for _, fragment := range []string{
		"CASE WHEN s.established THEN '已有' ELSE '潜在' END",
		"ELSE '未登记'",
		"public.onboarding_node",
		"public.contract c",
		"public.credit_line c",
		"c.direction='SUPPLIER_TO_US'",
		"max(latest.version)",
		"p.kind='PRICE_CHANGE'",
		"p.status IN ('PENDING','EXECUTING','BLOCKED')",
		"'AMBIGUOUS_VERSION'",
		"'INVALID_LEGACY_AMOUNT'",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("supplier workspace query missing native fact %q", fragment)
		}
	}
	for _, stale := range []string{
		"supplier_formal_config",
		"COALESCE(s.credit_amount::text",
		"CASE WHEN EXISTS (SELECT 1 FROM public.resource r WHERE r.supplier_id=s.id) THEN '已有'",
		"WHEN s.established THEN '已归档'",
	} {
		if strings.Contains(query, stale) {
			t.Fatalf("supplier workspace query still relies on stale projection %q", stale)
		}
	}
	if strings.Contains(query, "supplier_alias") {
		t.Fatal("v36 fallback query must remain usable before the shared alias migration is delivered")
	}
}

func TestSupplierWorkspaceListQueryIncludesFormerNamesOnlyWhenStructureExists(t *testing.T) {
	query := supplierWorkspaceListQuery(true)
	if !strings.Contains(query, "public.supplier_alias") || !strings.Contains(query, "json_agg(sa.alias_name ORDER BY sa.id)") {
		t.Fatal("supplier alias projection must be included when its shared structure is available")
	}
}

func TestSupplierWorkspacePageQueryFiltersAndSortsOnServer(t *testing.T) {
	countSQL, pageSQL, countArgs, pageArgs, err := supplierWorkspacePageQueries(true, SupplierWorkspaceQuery{
		Keyword: "Acme", Region: "CN", ContractStatus: "未登记", CooperationStage: "已有",
		HandlingStatus: "无未完成事项", SortBy: "name", SortOrder: "desc", Page: 2, Size: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"public.supplier_alias", "p.region=$2", "p.contract_status=$3", "p.cooperation_stage=$4", "p.handling_status=$5"} {
		if !strings.Contains(countSQL, fragment) || !strings.Contains(pageSQL, fragment) {
			t.Fatalf("page query must apply filter %q to count and data queries", fragment)
		}
	}
	if !strings.Contains(pageSQL, "ORDER BY lower(p.name) DESC,p.id ASC LIMIT $6 OFFSET $7") {
		t.Fatalf("page query must use a whitelisted stable sort and bound pagination: %s", pageSQL)
	}
	want := []any{"Acme", "CN", "未登记", "已有", "无未完成事项"}
	if !reflect.DeepEqual(countArgs, want) {
		t.Fatalf("count args = %#v, want %#v", countArgs, want)
	}
	wantPage := append(append([]any{}, want...), 10, 10)
	if !reflect.DeepEqual(pageArgs, wantPage) {
		t.Fatalf("page args = %#v, want %#v", pageArgs, wantPage)
	}
}

func TestSupplierWorkspacePageQueryRejectsCurrencyBlindCreditSort(t *testing.T) {
	_, _, _, _, err := supplierWorkspacePageQueries(false, SupplierWorkspaceQuery{SortBy: "credit_amount", SortOrder: "asc", Page: 1, Size: 20})
	if err == nil {
		t.Fatal("credit amount sorting must remain unavailable until mixed-currency comparison has a defined business rule")
	}
}

func TestParseSupplierWorkspaceQueryDefaultsAndRejectsUnknownSort(t *testing.T) {
	query, err := parseSupplierWorkspaceQuery(url.Values{"cooperation_stage": {"已有"}, "handling_status": {"有未完成事项"}})
	if err != nil {
		t.Fatal(err)
	}
	if query.Page != 1 || query.Size != 20 || query.SortBy != "id" || query.SortOrder != "asc" || query.CooperationStage != "已有" || query.HandlingStatus != "有未完成事项" {
		t.Fatalf("parsed supplier workspace query = %+v", query)
	}
	if _, err := parseSupplierWorkspaceQuery(url.Values{"sort_by": {"credit_amount"}}); err == nil {
		t.Fatal("credit sort must be rejected until mixed-currency sorting has a business rule")
	}
	query, err = parseSupplierWorkspaceQuery(url.Values{"contract_status": {"未登记"}})
	if err != nil || query.ContractStatus != "未登记" {
		t.Fatalf("contract status filter must support unrecorded contracts, query=%+v err=%v", query, err)
	}
}

func TestSupplierWorkspaceListExecutesReadOnlyAgainstV36(t *testing.T) {
	dsn := os.Getenv("SUPPLY_WORKSPACE_READONLY_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_WORKSPACE_READONLY_DSN to run the read-only v36 projection check")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SET default_transaction_read_only=on`); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	var version int64
	var dirty bool
	if err := db.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 36 || dirty {
		t.Fatalf("read-only projection target must be clean v36, got version=%d dirty=%t", version, dirty)
	}
	var readOnly string
	if err := db.QueryRowContext(ctx, `SELECT current_setting('default_transaction_read_only')`).Scan(&readOnly); err != nil || readOnly != "on" {
		t.Fatalf("connection must remain read-only before querying supplier data: setting=%q err=%v", readOnly, err)
	}
	if _, err := NewSupplyChainRuntime(db, nil).ListSuppliers(ctx); err != nil {
		t.Fatalf("v36 supplier workspace projection query failed: %v", err)
	}
}

func TestSupplierWorkspaceProjectionWithIsolatedV36Data(t *testing.T) {
	dsn := os.Getenv("SUPPLY_WORKSPACE_ISOLATED_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_WORKSPACE_ISOLATED_DSN to run the non-empty isolated v36 projection check")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	var databaseName string
	var version int64
	var dirty bool
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "model_bss_a_supplier_workspace_") {
		t.Fatalf("non-empty projection test refuses non-isolated database %q", databaseName)
	}
	if err := db.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 36 || dirty {
		t.Fatalf("isolated projection target must be clean v36, got version=%d dirty=%t", version, dirty)
	}

	key := fmt.Sprintf("QA-S03-%d", time.Now().UnixNano())
	var modelID int64
	supplierIDs := make(map[string]int64)
	var resourceID int64
	cleanup := func() {
		if resourceID > 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM public.resource WHERE id=$1`, resourceID)
		}
		if modelID > 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM public.standard_model WHERE id=$1`, modelID)
		}
		for _, id := range supplierIDs {
			_, _ = db.ExecContext(ctx, `DELETE FROM public.submission WHERE supplier_id=$1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM public.onboarding_flow WHERE kind='SUPPLIER' AND owner_id=$1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM public.contract WHERE subject_type='SUPPLIER' AND subject_id=$1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM public.credit_line WHERE subject_type='SUPPLIER' AND subject_id=$1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM public.supplier WHERE id=$1`, id)
		}
	}
	defer cleanup()

	if err := db.QueryRowContext(ctx, `INSERT INTO public.standard_model(company,model_type,name,official_id) VALUES('QA-S03',$1,$2,$2) RETURNING id`, "TEXT", key+"-Model").Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	createSupplier := func(suffix string, established bool) int64 {
		t.Helper()
		var id int64
		name := key + "-" + suffix
		if err := db.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region,established,settlement_currency) VALUES($1,'CN',$2,'CNY') RETURNING id`, name, established).Scan(&id); err != nil {
			t.Fatal(err)
		}
		supplierIDs[suffix] = id
		return id
	}
	potentialID := createSupplier("Potential", false)
	establishedWithoutContractID := createSupplier("EstablishedWithoutContract", true)
	existingID := createSupplier("Existing", true)
	ambiguousID := createSupplier("Ambiguous", true)

	if _, err := db.ExecContext(ctx, `INSERT INTO public.resource(standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted) VALUES($1,$2,$3,$3,'CN',0.5,'ADMITTED',true) RETURNING id`, modelID, existingID, key+"-Resource"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM public.resource WHERE supplier_id=$1 AND standard_model_id=$2`, existingID, modelID).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.contract(subject_type,subject_id,status,version) VALUES('SUPPLIER',$1,'ARCHIVED',1),('SUPPLIER',$2,'PROCESSING',1)`, existingID, ambiguousID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.credit_line(subject_type,subject_id,direction,amount,currency,version) VALUES
('SUPPLIER',$1,'SUPPLIER_TO_US',80,'CNY',1),('SUPPLIER',$1,'SUPPLIER_TO_US',125,'CNY',2),
('SUPPLIER',$2,'SUPPLIER_TO_US',20,'CNY',1),('SUPPLIER',$2,'SUPPLIER_TO_US',25,'CNY',1)`, existingID, ambiguousID); err != nil {
		t.Fatal(err)
	}
	var flowID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO public.onboarding_flow(kind,owner_id,status,current_node) VALUES('SUPPLIER',$1,'PROCESSING','CONFIG') RETURNING id`, existingID).Scan(&flowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.onboarding_node(flow_id,node_key,status) VALUES($1,'CONFIG','IN_PROGRESS'),($1,'CONTRACT','DONE')`, flowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.submission(supplier_id,kind,region,status) VALUES($1,'MODEL_NEW','CN','PENDING_APPROVAL')`, ambiguousID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.plan(resource_id,kind,due_at,status) VALUES($1,'PRICE_CHANGE',now(),'BLOCKED')`, resourceID); err != nil {
		t.Fatal(err)
	}

	rows, err := NewSupplyChainRuntime(db, nil).ListSuppliers(ctx)
	if err != nil {
		t.Fatalf("isolated v36 supplier list query failed: %v", err)
	}
	byName := make(map[string]SupplierWorkspaceRow, 4)
	for _, row := range rows {
		if strings.HasPrefix(row.Name, key+"-") {
			byName[row.Name] = row
		}
	}
	potential := byName[key+"-Potential"]
	if potential.ID != potentialID || potential.CooperationStage != "潜在" || potential.HandlingStatus != "无未完成事项" || potential.ContractStatus != "未登记" || potential.CreditStatus != "NOT_RECORDED" {
		t.Fatalf("potential empty supplier projection mismatch: %+v", potential)
	}
	establishedWithoutContract := byName[key+"-EstablishedWithoutContract"]
	if establishedWithoutContract.ID != establishedWithoutContractID || establishedWithoutContract.CooperationStage != "已有" || establishedWithoutContract.ContractStatus != "未登记" {
		t.Fatalf("established supplier without contract facts must remain unregistered: %+v", establishedWithoutContract)
	}
	existing := byName[key+"-Existing"]
	if existing.ID != existingID || existing.ResourceCount != 1 || existing.CreditAmount != "125.00" || existing.CreditStatus != "AVAILABLE" || existing.CooperationStage != "已有" || existing.HandlingStatus != "有未完成事项" || existing.ContractStatus != "已归档" {
		t.Fatalf("existing supplier with current work projection mismatch: %+v", existing)
	}
	ambiguous := byName[key+"-Ambiguous"]
	if ambiguous.ID != ambiguousID || ambiguous.CreditAmount != "" || ambiguous.CreditStatus != "AMBIGUOUS_VERSION" || ambiguous.HandlingStatus != "有未完成事项" || ambiguous.ContractStatus != "办理中" {
		t.Fatalf("ambiguous credit and processing supplier projection mismatch: %+v", ambiguous)
	}

	filteredPage, err := NewSupplyChainRuntime(db, nil).ListSuppliersPage(ctx, SupplierWorkspaceQuery{
		CooperationStage: "已有", HandlingStatus: "有未完成事项", SortBy: "name", SortOrder: "asc", Page: 1, Size: 1,
	})
	if err != nil {
		t.Fatalf("filtered supplier page query failed: %v", err)
	}
	if filteredPage.Total != 2 || len(filteredPage.List) != 1 || filteredPage.Page != 1 || filteredPage.Size != 1 || filteredPage.List[0].ID != ambiguousID {
		t.Fatalf("filtered supplier first page mismatch: %+v", filteredPage)
	}
	filteredPage, err = NewSupplyChainRuntime(db, nil).ListSuppliersPage(ctx, SupplierWorkspaceQuery{
		CooperationStage: "已有", HandlingStatus: "有未完成事项", SortBy: "name", SortOrder: "asc", Page: 2, Size: 1,
	})
	if err != nil {
		t.Fatalf("second supplier page query failed: %v", err)
	}
	if filteredPage.Total != 2 || len(filteredPage.List) != 1 || filteredPage.List[0].ID != existingID {
		t.Fatalf("filtered supplier second page mismatch: %+v", filteredPage)
	}
}
