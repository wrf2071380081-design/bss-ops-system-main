package supplypg

import (
	"context"
	"database/sql"
	"strings"
)

// SupplierModelStore 是供应商门户「模型管理」的只读投影（2026-10-28）。
//
// 为什么不用 A 线的 ResourceReadStore：那套读模型要求 `resource.buyer_id` 非空、
// 且要和 facts（resource 投影事实表）逐字段对得上——供应商侧只要「我有哪些模型、
// 什么状态、测试过没过」，采购人没分配、facts 没落库都不该让整页 500。
// 所以这里走一条**窄查询**：resource + standard_model + 最近一次 test_report。
type SupplierModelStore struct {
	db     *sql.DB
	schema string
}

// SupplierModelRecord 是列表行（字段与前端 SupplierModelResourceDTO 对齐）。
type SupplierModelRecord struct {
	ID              string
	StandardModelID string
	ModelName       string
	OfficialID      string
	SupplierModelID string
	Region          string
	Coefficient     string
	State           string
	EverAdmitted    bool

	// 最近一次测试报告（没有就是零值，HasReport=false）。
	HasReport        bool
	ReportID         string
	ReportVerdict    string
	ConnectionPassed bool
	StressPassed     bool
	ConnectionMS     *string
	RPS              *string
	TPS              *string
}

// NewSupplierModelStore 构造存储。
func NewSupplierModelStore(db *sql.DB, schema string) (*SupplierModelStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("supplier model store")
	}
	return &SupplierModelStore{db: db, schema: schema}, nil
}

func (s *SupplierModelStore) table(name string) string { return s.schema + "." + name }

// List 按主体分页列出资源；supplier_id 必须由调用方从登录态取得。
//
// 报告取「该资源最近一次」：测试重跑过的话旧报告不参与展示，
// 供应商看到的永远是最后一次结论。
func (s *SupplierModelStore) List(ctx context.Context, supplierID int64, page, size int) ([]SupplierModelRecord, int64, error) {
	if supplierID <= 0 {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("resource")+` WHERE supplier_id = $1`, supplierID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id::text, r.standard_model_id::text, m.name, COALESCE(m.official_id,''), r.supplier_model_name,
       r.region, r.coefficient::text, r.state, r.ever_admitted,
       tr.id::text, COALESCE(tr.verdict,''), COALESCE(tr.connection_passed,false), COALESCE(tr.stress_passed,false),
       tr.connection_ms::text, tr.rps::text, tr.tps::text
FROM `+s.table("resource")+` r
JOIN `+s.table("standard_model")+` m ON m.id = r.standard_model_id
LEFT JOIN LATERAL (
  SELECT id, verdict, connection_passed, stress_passed, connection_ms, rps, tps
  FROM `+s.table("test_report")+`
  WHERE resource_id = r.id
  ORDER BY tested_at DESC, id DESC
  LIMIT 1
) tr ON TRUE
WHERE r.supplier_id = $1
ORDER BY r.id
LIMIT $2 OFFSET $3`, supplierID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	list := make([]SupplierModelRecord, 0, size)
	for rows.Next() {
		var item SupplierModelRecord
		var reportID, verdict, connectionMS, rps, tps sql.NullString
		if err := rows.Scan(&item.ID, &item.StandardModelID, &item.ModelName, &item.OfficialID, &item.SupplierModelID,
			&item.Region, &item.Coefficient, &item.State, &item.EverAdmitted,
			&reportID, &verdict, &item.ConnectionPassed, &item.StressPassed,
			&connectionMS, &rps, &tps); err != nil {
			return nil, 0, err
		}
		item.HasReport = reportID.Valid && strings.TrimSpace(reportID.String) != ""
		item.ReportID = reportID.String
		item.ReportVerdict = strings.TrimSpace(verdict.String)
		item.ConnectionMS = metricText(connectionMS)
		item.RPS = metricText(rps)
		item.TPS = metricText(tps)
		list = append(list, item)
	}
	return list, total, rows.Err()
}

// metricText 把数据库数值转成前端原样展示的字符串（保留两位，去掉尾随 0 交给前端）。
func metricText(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value.String)
	return &trimmed
}
