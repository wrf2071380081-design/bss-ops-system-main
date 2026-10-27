package supply

import "testing"

func TestResourceMultiFiltersAndMetricRanges(t *testing.T) {
	q, err := normalizeResourceQuery(ResourceListQuery{SupplierIDs: []string{"21", "22"}, Regions: []string{"CN"}, ConnectionMax: "0", RPSMin: "9007199254740993"})
	if err != nil {
		t.Fatal(err)
	}
	row := &ResourceSnapshot{SupplierID: "21", Region: "CN", Test: ResourceTestView{Status: "COMPLETED", Metrics: ResourceReportMetrics{}}}
	zero, big := "0", "9007199254740993"
	row.Test.Metrics.ConnectionMS = &zero
	row.Test.Metrics.RPS = &big
	if !resourceMatches(row, q) {
		t.Fatal("zero and precise large metric excluded")
	}
	row.Test.Status = "STALE"
	if resourceMatches(row, q) {
		t.Fatal("stale metric included")
	}
	row.Test.Status = "COMPLETED"
	row.Test.Metrics.ConnectionMS = nil
	if resourceMatches(row, q) {
		t.Fatal("missing metric included")
	}
	for _, bad := range []ResourceListQuery{{Region: "CN", Regions: []string{"OVERSEAS"}}, {SupplierIDs: []string{"21", "21"}}, {States: []string{"INVALID"}}, {CoefficientMin: "0.9", CoefficientMax: "0.5"}, {RPSMin: "NaN"}, {TPSMax: "-1"}} {
		if _, err := normalizeResourceQuery(bad); err == nil {
			t.Fatalf("invalid filter accepted: %+v", bad)
		}
	}
}
