package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFilterOptionsPostgres34(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated PostgreSQL options test not requested")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("explicit dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong test database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	prefix := fmt.Sprintf("AOPT-%d-", time.Now().UnixNano())
	models, suppliers, resources := []string{}, []string{}, []string{}
	addModel := func(suffix string) string {
		t.Helper()
		var id string
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.standard_model(company,name,official_id,model_type) VALUES ($1,$1,$1,'TEXT') RETURNING id::text`, prefix+suffix).Scan(&id); err != nil {
			t.Fatal(err)
		}
		models = append(models, id)
		return id
	}
	addSupplier := func(suffix, region string) string {
		t.Helper()
		var id string
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region,status) VALUES ($1,$2,'FROZEN') RETURNING id::text`, prefix+suffix, region).Scan(&id); err != nil {
			t.Fatal(err)
		}
		suppliers = append(suppliers, id)
		return id
	}
	addResource := func(model, supplier, region string) {
		t.Helper()
		var id string
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.resource(standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient) VALUES ($1,$2,'fixture','fixture',$3,0.5) RETURNING id::text`, model, supplier, region).Scan(&id); err != nil {
			t.Fatal(err)
		}
		resources = append(resources, id)
	}
	m1, m2 := addModel("BASE-A"), addModel("BASE-B")
	addModel("BASE-C-unrelated")
	s1, s2 := addSupplier("BASE-A", "CN"), addSupplier("BASE-B", "OVERSEAS")
	addSupplier("BASE-C-unrelated", "CN")
	addResource(m1, s1, "CN")
	addResource(m2, s1, "CN")
	addResource(m1, s2, "OVERSEAS")
	mLiteral := addModel("LITERAL_100%")
	sLiteral := addSupplier("LITERAL_100%", "CN")
	addResource(mLiteral, sLiteral, "CN")
	for i := 1; i <= 55; i++ {
		suffix := fmt.Sprintf("MANY-%02d", i)
		m := addModel(suffix)
		s := addSupplier(suffix, "CN")
		addResource(m, s, "CN")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		for _, part := range []struct {
			table string
			ids   []string
		}{{"resource", resources}, {"supplier", suppliers}, {"standard_model", models}} {
			for _, id := range part.ids {
				if _, err := db.ExecContext(cleanup, "DELETE FROM public."+part.table+" WHERE id=$1::bigint", id); err != nil {
					t.Error(err)
				}
			}
		}
		var count int
		if err := db.QueryRowContext(cleanup, `SELECT count(*) FROM public.standard_model WHERE company LIKE $1`, prefix+"%").Scan(&count); err != nil || count != 0 {
			t.Errorf("fixture cleanup count=%d err=%v", count, err)
		}
	}()
	store := NewFilterOptionsStore(db)
	t.Run("model companies global including unrelated while suppliers follow resource region", func(t *testing.T) {
		q := supply.ModelOptionsQuery{Regions: []string{"CN"}, Keyword: prefix + "BASE-", Limit: 50}
		opts, err := store.ModelFilterOptions(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(opts.Companies) != 3 || opts.Companies[2] != prefix+"BASE-C-unrelated" || len(opts.Suppliers) != 1 || opts.Suppliers[0].ID != s1 || opts.HasMore {
			t.Fatalf("model options %+v", opts)
		}
		q.Regions = []string{"CN", "OVERSEAS"}
		opts, err = store.ModelFilterOptions(ctx, q)
		if err != nil || len(opts.Suppliers) != 2 {
			t.Fatalf("multi region %+v %v", opts, err)
		}
		q.Regions = []string{"CN"}
		q.Limit = 1
		opts, err = store.ModelFilterOptions(ctx, q)
		if err != nil || len(opts.Companies) != 1 || len(opts.Suppliers) != 1 || !opts.HasMore {
			t.Fatalf("only companies truncated %+v %v", opts, err)
		}
	})
	t.Run("plaza companies use resources and deduplicate suppliers", func(t *testing.T) {
		opts, err := store.ResourceFilterOptions(ctx, supply.ResourceOptionsQuery{Region: "CN", Keyword: prefix + "BASE-", Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(opts.Companies) != 2 || len(opts.Suppliers) != 1 || opts.Suppliers[0].ID != s1 || opts.HasMore {
			t.Fatalf("plaza CN %+v", opts)
		}
		opts, err = store.ResourceFilterOptions(ctx, supply.ResourceOptionsQuery{Region: "OVERSEAS", Keyword: prefix + "BASE-", Limit: 50})
		if err != nil || len(opts.Companies) != 1 || opts.Companies[0] != prefix+"BASE-A" || len(opts.Suppliers) != 1 || opts.Suppliers[0].ID != s2 {
			t.Fatalf("plaza overseas %+v %v", opts, err)
		}
		opts, err = store.ResourceFilterOptions(ctx, supply.ResourceOptionsQuery{Region: "OVERSEAS", Keyword: prefix + "BASE-", Limit: 1})
		if err != nil || opts.HasMore {
			t.Fatalf("exactly one is not truncated %+v %v", opts, err)
		}
	})
	t.Run("limit plus one detects truncation and search happens before limit", func(t *testing.T) {
		q := supply.ResourceOptionsQuery{Region: "CN", Keyword: prefix + "MANY-", Limit: 50}
		opts, err := store.ResourceFilterOptions(ctx, q)
		if err != nil || len(opts.Companies) != 50 || len(opts.Suppliers) != 50 || !opts.HasMore || opts.Suppliers[0].Name != prefix+"MANY-01" {
			t.Fatalf("resource truncation %+v %v", opts, err)
		}
		catalog, err := store.ModelFilterOptions(ctx, supply.ModelOptionsQuery{Keyword: q.Keyword, Limit: 50})
		if err != nil || len(catalog.Companies) != 50 || len(catalog.Suppliers) != 50 || !catalog.HasMore {
			t.Fatalf("model truncation %+v %v", catalog, err)
		}
		q.Keyword = prefix + "MANY-55"
		opts, err = store.ResourceFilterOptions(ctx, q)
		if err != nil || len(opts.Companies) != 1 || len(opts.Suppliers) != 1 || opts.HasMore {
			t.Fatalf("search after first page %+v %v", opts, err)
		}
	})
	t.Run("keyword is literal rather than SQL wildcard", func(t *testing.T) {
		opts, err := store.ResourceFilterOptions(ctx, supply.ResourceOptionsQuery{Keyword: prefix + "LITERAL_100%", Limit: 50})
		if err != nil || len(opts.Companies) != 1 || len(opts.Suppliers) != 1 {
			t.Fatalf("literal %+v %v", opts, err)
		}
		opts, err = store.ResourceFilterOptions(ctx, supply.ResourceOptionsQuery{Keyword: prefix + "LITERAL%", Limit: 50})
		if err != nil || len(opts.Companies) != 0 || len(opts.Suppliers) != 0 || opts.HasMore {
			t.Fatalf("unexpected wildcard match %+v %v", opts, err)
		}
	})
}
