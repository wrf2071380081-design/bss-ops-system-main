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
	CacheHitTokens   sql.NullInt64 `gorm:"column:cache_hit_tokens"`
	CacheInputTokens sql.NullInt64 `gorm:"column:cache_input_tokens"`
	// 平台常只报比率不报 token 明细（Q4），这两个字段用来兜底展示。
	CacheHitRatio   sql.NullString `gorm:"column:cache_hit_ratio"`
	CacheRatioReady sql.NullBool   `gorm:"column:cache_ratio_available"`
	Scene           sql.NullString `gorm:"column:scene"`
}

func (row ReportCacheRow) DomainCache() *supply.ReportCacheCounters {
	result := &supply.ReportCacheCounters{Status: "NOT_PROVIDED"}
	if row.Scene.Valid {
		result.Scene = row.Scene.String
	}
	// 000034 explicitly defines missing/invalid historical counters as unavailable.
	// Preserve the stored row; do not turn missing data into zero or auto-repair it.
	if !row.CacheHitTokens.Valid || !row.CacheInputTokens.Valid || row.CacheHitTokens.Int64 < 0 || row.CacheInputTokens.Int64 <= 0 || row.CacheHitTokens.Int64 > row.CacheInputTokens.Int64 {
		// 没有 token 明细，但平台上报了比率：直接用它，别显示"未提供"
		// （2026-11-25：报告里 88.95%，列表却空着）。
		if row.CacheRatioReady.Valid && row.CacheRatioReady.Bool && row.CacheHitRatio.Valid {
			if pct, ok := normalizePercent(row.CacheHitRatio.String); ok {
				result.Status = "PROVIDED"
				result.HitPercent = pct
			}
		}
		return result
	}
	result.Status = "PROVIDED"
	result.HitTokens = strconv.FormatInt(row.CacheHitTokens.Int64, 10)
	result.InputTokens = strconv.FormatInt(row.CacheInputTokens.Int64, 10)
	return result
}
