package supplypg

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
)

func TestFilterOptionQueriesBindLiteralKeywordAndRegions(t *testing.T) {
	keyword := `%' OR 1=1 --`
	queries, err := buildFilterOptionQueries([]string{"CN", "OVERSEAS"}, keyword, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(queries.company, keyword) || strings.Contains(queries.supplier, keyword) || len(queries.companyArgs) != 4 || len(queries.supplierArgs) != 4 || queries.companyArgs[0] != keyword || queries.companyArgs[3] != 51 || !strings.Contains(queries.supplier, "r.region=s.region") {
		t.Fatalf("unsafe/wrong query %+v", queries)
	}
	global, err := buildFilterOptionQueries([]string{"CN"}, keyword, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(global.company, "resource") || len(global.companyArgs) != 2 || len(global.supplierArgs) != 3 {
		t.Fatalf("catalog company incorrectly tied to resource region: %+v", global)
	}
	for _, regions := range [][]string{{"ALL"}, {"CN", "CN"}, {"CN", "OVERSEAS", "CN"}} {
		if _, err := buildFilterOptionQueries(regions, "", 50, false); err == nil {
			t.Fatalf("invalid region selection %v", regions)
		}
	}
	for _, limit := range []int{0, -1, 51} {
		if _, err := buildFilterOptionQueries(nil, "", limit, false); err == nil {
			t.Fatalf("invalid option limit %d", limit)
		}
	}
}
func TestFilterOptionsWithoutDatabaseReturnDependencyError(t *testing.T) {
	for _, store := range []*FilterOptionsStore{nil, NewFilterOptionsStore(nil)} {
		if _, err := store.ModelFilterOptions(context.Background(), supply.ModelOptionsQuery{Limit: 50}); !errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("missing model DB %v", err)
		}
		if _, err := store.ResourceFilterOptions(context.Background(), supply.ResourceOptionsQuery{Limit: 50}); !errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("missing resource DB %v", err)
		}
	}
}
