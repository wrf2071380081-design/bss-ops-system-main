package supplypg

import (
	"database/sql"
	"testing"
)

func count(value int64) sql.NullInt64 { return sql.NullInt64{Int64: value, Valid: true} }
func TestReportCacheMapsMigration34NullableBigintsWithoutFloat(t *testing.T) {
	for _, test := range []struct {
		hit, input         int64
		wantHit, wantInput string
	}{{0, 2000, "0", "2000"}, {1200, 2000, "1200", "2000"}, {9007199254740993, 9223372036854775807, "9007199254740993", "9223372036854775807"}} {
		row := ReportCacheRow{CacheHitTokens: count(test.hit), CacheInputTokens: count(test.input), Scene: sql.NullString{String: "固定前缀", Valid: true}}
		result := row.DomainCache()
		if result.Status != "PROVIDED" || result.HitTokens != test.wantHit || result.InputTokens != test.wantInput || result.Scene != "固定前缀" {
			t.Fatalf("lost counters %+v", result)
		}
	}
}
func TestReportCacheMissingAndInvalidHistoricalRowsDisplayNotProvided(t *testing.T) {
	for _, row := range []ReportCacheRow{{}, {CacheHitTokens: count(0)}, {CacheInputTokens: count(2000)}, {CacheHitTokens: count(-1), CacheInputTokens: count(2000)}, {CacheHitTokens: count(2), CacheInputTokens: count(1)}, {CacheHitTokens: count(0), CacheInputTokens: count(0)}} {
		result := row.DomainCache()
		if result.Status != "NOT_PROVIDED" || result.HitTokens != "" || result.InputTokens != "" {
			t.Fatalf("historical row fabricated metric %+v", result)
		}
	}
}
func TestReportCacheProjectionDoesNotRewriteSelectedReport(t *testing.T) {
	row := ReportCacheRow{CacheHitTokens: count(1200), CacheInputTokens: count(2000)}
	before := row
	first := row.DomainCache()
	first.HitTokens = "0"
	second := row.DomainCache()
	if row != before || second.HitTokens != "1200" || second.Scene != "" {
		t.Fatal("projection rewrote report or reused state")
	}
}
