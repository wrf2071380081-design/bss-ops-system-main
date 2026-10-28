package supplypg

import (
	"context"
	"database/sql"
	"fmt"
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
	Company         string
	ModelType       string
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

// visibleClause 是原型 sPortalResourceRows() 的可见行规则：
//
//	本主体 + 未合并 + 状态在四态内 + （曾接入 或 已接入/已下架 或 有过「通过」的申报）
//
// 意义：供应商只该看到「已经进过平台流程」的模型。刚提交还没跑完的、纯草稿阶段的
// 资源不出现在模型管理里（它们在申报页看进度），否则列表会被半成品塞满。
const visibleResourceClause = `(r.ever_admitted OR r.state IN ('ADMITTED','OFFLINE') OR EXISTS (
  SELECT 1 FROM %s s JOIN %s sr ON sr.submission_id = s.id
  WHERE s.supplier_id = r.supplier_id AND s.status = 'APPROVED'
    AND (sr.resource_id = r.id OR sr.standard_model_id = r.standard_model_id)))`

// CountAll 返回该主体名下**全部**资源数（含未进入可见范围的）。
// 页面用它算「还有 N 个模型在流程中」，避免按可见行规则过滤后整页空白却没有任何解释。
func (s *SupplierModelStore) CountAll(ctx context.Context, supplierID int64) (int64, error) {
	if supplierID <= 0 {
		return 0, nil
	}
	var total int64
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+s.table("resource")+` WHERE supplier_id = $1`, supplierID).Scan(&total)
	return total, err
}

// SupplierModelFilterOptions 是筛选条候选值。
//
// 模型类型与接入状态都是**固定枚举**（原型这两组选项就是写死的），
// 只有模型公司来自该主体名下的实际数据——公司是开放集合，枚举不出来。
//
// 公司取的是该主体**全部**资源（不按可见行规则过滤）：否则页面一空，
// 下拉就没有候选值、变成点不动的灰控件（2026-10-28 反馈的问题）。
type SupplierModelFilterOptions struct {
	Companies  []string
	ModelTypes []string
	States     []string
}

// supplierModelTypes 是平台标准模型的四种类型，对应原型「模型类型」四项。
var supplierModelTypes = []string{"TEXT", "IMAGE", "VIDEO", "AUDIO"}

// supplierAccessStates 是供应商视角的接入状态三档，对应原型「接入状态」三项。
//
// 内部状态里 PENDING_TEST（待测试）和 PENDING_ACCESS（待接入）在供应商侧
// 都呈现为「待接入」——不向供应商暴露"测试中"这种内部阶段。
var supplierAccessStates = []string{"PENDING_ACCESS", "ADMITTED", "OFFLINE"}

// SupplierAccessStatus 把内部资源状态折叠成供应商展示用的三档。
// 与前端 `accessStatus()` 同一口径，改这里要同步前端。
func SupplierAccessStatus(state string) string {
	switch state {
	case "ADMITTED":
		return "ADMITTED"
	case "OFFLINE":
		return "OFFLINE"
	default:
		return "PENDING_ACCESS"
	}
}

// FilterOptions 按主体取筛选候选值。
func (s *SupplierModelStore) FilterOptions(ctx context.Context, supplierID int64) (*SupplierModelFilterOptions, error) {
	options := &SupplierModelFilterOptions{
		Companies:  []string{},
		ModelTypes: append([]string{}, supplierModelTypes...),
		States:     append([]string{}, supplierAccessStates...),
	}
	if supplierID <= 0 {
		return options, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT COALESCE(m.company,'')
FROM `+s.table("resource")+` r
JOIN `+s.table("standard_model")+` m ON m.id = r.standard_model_id
WHERE r.supplier_id = $1 AND m.company <> ''
ORDER BY 1`, supplierID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var company string
		if err := rows.Scan(&company); err != nil {
			return nil, err
		}
		options.Companies = append(options.Companies, company)
	}
	return options, rows.Err()
}

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
	visible := fmt.Sprintf(visibleResourceClause, s.table("submission"), s.table("submission_row"))
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+s.table("resource")+` r WHERE r.supplier_id = $1 AND `+visible,
		supplierID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id::text, r.standard_model_id::text, m.name, COALESCE(m.official_id,''), r.supplier_model_name,
       COALESCE(m.company,''), COALESCE(m.model_type,''),
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
WHERE r.supplier_id = $1 AND `+visible+`
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
			&item.Company, &item.ModelType,
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
