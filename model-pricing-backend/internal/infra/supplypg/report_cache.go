// Package supplypg maps target PostgreSQL rows to A's domain without changing
// business invariants or using legacy profile/SKU tables.
package supplypg

import (
	"database/sql"
	"model_bss/internal/domain/supply"
	"strconv"
)

// ReportCacheRow corresponds exactly to test_report's 000034 nullable bigint
// columns and its 000033 scene column. The enclosing repository must select ONE
// immutable report by its actual ID; never merge counters from multiple rows.
// This projection is read-only, not a B report write validator or a repository.
type ReportCacheRow struct {
	CacheHitTokens   sql.NullInt64  `gorm:"column:cache_hit_tokens"`
	CacheInputTokens sql.NullInt64  `gorm:"column:cache_input_tokens"`
	Scene            sql.NullString `gorm:"column:scene"`
}

func (row ReportCacheRow) DomainCache() *supply.ReportCacheCounters {
	result := &supply.ReportCacheCounters{Status: "NOT_PROVIDED"}
	if row.Scene.Valid {
		result.Scene = row.Scene.String
	}
	// 000034 explicitly defines missing/invalid historical counters as unavailable.
	// Preserve the stored row; do not turn missing data into zero or auto-repair it.
	if !row.CacheHitTokens.Valid || !row.CacheInputTokens.Valid || row.CacheHitTokens.Int64 < 0 || row.CacheInputTokens.Int64 <= 0 || row.CacheHitTokens.Int64 > row.CacheInputTokens.Int64 {
		return result
	}
	result.Status = "PROVIDED"
	result.HitTokens = strconv.FormatInt(row.CacheHitTokens.Int64, 10)
	result.InputTokens = strconv.FormatInt(row.CacheInputTokens.Int64, 10)
	return result
}
