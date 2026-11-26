package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"model_bss/internal/domain/supply"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ResourceProjection contains only native, non-secret resource/supplier columns.
type ResourceProjection struct {
	ID, SupplierID, StandardModelID, Region, SupplierModelID, Coefficient, BuyerID, State, LatestReportID string
	EverAdmitted                                                                                          bool
	CreatedAt, UpdatedAt                                                                                  time.Time
	AdmittedAt                                                                                            *time.Time
	Supplier                                                                                              supply.ResourceSupplier
}

// ResourceReadFacts comes from persisted A/B facts in the same transaction.
// ProjectionUpdatedAt binds those facts to a native row; it is not a revision.
type ResourceReadFacts struct {
	ResourceID                                            string
	Projection                                            ResourceProjection
	ProjectionUpdatedAt                                   time.Time
	Revision                                              uint64
	OriginRowID                                           string
	OriginRowVersion, TargetVersion, LatestReportSequence uint64
	FormalConfigID                                        string
	FormalConfigRevision                                  uint64
	Config                                                *supply.FormalConfigView
	Report                                                *ReportEvidenceRef
}
type ResourceFactsLoader func(context.Context, *sql.Tx, ResourceProjection) (*ResourceReadFacts, error)
type ResourceReadStore struct {
	schema string
	read   ModelReadRunner
	facts  ResourceFactsLoader
}

var _ supply.ResourceReadStore = (*ResourceReadStore)(nil)

func NewResourceReadStore(schema string, read ModelReadRunner, facts ResourceFactsLoader) (*ResourceReadStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &ResourceReadStore{schema, read, facts}, nil
}
func (s *ResourceReadStore) within(ctx context.Context, run func(*sql.Tx) error) error {
	if s == nil || s.facts == nil {
		return supply.ErrDependencyUnavailable
	}
	return (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, run)
}
func (s *ResourceReadStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

func resourceReadQuery(q supply.ResourceListQuery, view string) (string, []any, error) {
	if q.View != view || !modelPageValid(q.Page, q.Size) || !q.NullsLast || q.TieBreakBy != "id" || (q.Order != "asc" && q.Order != "desc") {
		return "", nil, supply.ErrReadContract
	}
	allowed := []string{"id", "name", "company", "model_type", "supplier_count", "resource_count", "available_count", "coefficient_min", "coefficient_max"}
	if view == "flat" {
		allowed = []string{"id", "supplier_name", "region", "coefficient", "state", "updated_at", "admitted_at", "connection_ms", "rps", "tps", "cache_hit_rate"}
	}
	if !slices.Contains(allowed, q.SortBy) {
		return "", nil, supply.ErrReadContract
	}
	normalized, normalizeErr := supply.NormalizeResourceListQuery(q)
	if normalizeErr != nil {
		return "", nil, supply.ErrReadContract
	}
	q = normalized
	where := []string{"TRUE"}
	args := []any{}
	add := func(v string) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	for _, f := range []struct {
		value, column string
		limit         int
	}{
		{q.Keyword, "", 200}, {q.Company, "m.company", 128}, {q.ModelType, "m.model_type", 32},
		{q.SupplierID, "r.supplier_id", 19}, {q.StandardModelID, "r.standard_model_id", 19}, {q.Region, "r.region", 16}, {q.State, "r.state", 24},
	} {
		if !utf8.ValidString(f.value) || strings.ContainsRune(f.value, 0) || utf8.RuneCountInString(f.value) > f.limit {
			return "", nil, supply.ErrReadContract
		}
		if f.value == "" {
			continue
		}
		value := f.value
		switch f.column {
		case "r.supplier_id", "r.standard_model_id":
			if !modelIdentity(value) {
				return "", nil, supply.ErrReadContract
			}
		case "r.region":
			if value != "CN" && value != "OVERSEAS" {
				return "", nil, supply.ErrReadContract
			}
		case "r.state":
			if !slices.Contains([]string{"PENDING_TEST", "PENDING_ACCESS", "ADMITTED", "OFFLINE"}, value) {
				return "", nil, supply.ErrReadContract
			}
		case "m.model_type":
			if !slices.Contains([]string{"TEXT", "IMAGE", "AUDIO", "VIDEO"}, value) {
				return "", nil, supply.ErrReadContract
			}
		}
		if f.column == "r.state" && value == "PENDING_ACCESS" {
			where = append(where, "(r.state="+add("PENDING_ADMIT")+" OR r.state="+add("PENDING_ACCESS")+")")
		} else if f.column == "" {
			where = append(where, `strpos(lower(m.name||' '||m.official_id||' '||s.name||' '||r.supplier_model_name),lower(`+add(value)+`::text))>0`)
		} else {
			where = append(where, f.column+"="+add(value))
		}
	}
	for _, f := range []struct {
		column string
		values []string
	}{{"m.company", q.Companies}, {"m.model_type", q.ModelTypes}, {"r.supplier_id", q.SupplierIDs}, {"r.region", q.Regions}, {"r.state", q.States}} {
		if len(f.values) == 0 {
			continue
		}
		params := []string{}
		for _, v := range f.values {
			params = append(params, add(v))
			if f.column == "r.state" && v == "PENDING_ACCESS" {
				params = append(params, add("PENDING_ADMIT"))
			}
		}
		where = append(where, f.column+" IN ("+strings.Join(params, ",")+")")
	}
	return strings.Join(where, " AND "), args, nil
}

// resourceModelRef 是分组用的模型元信息：能从标准模型读到就用模型本身，
// 读不到时退回 resource 关联的 standard_model 列，保证"模型行"仍然能显示出来。
type resourceModelRef struct{ id, name, officialID, company, modelType string }

// resourceProjectionRow 是一条资源的 SQL 投影 + 模型列兜底。
type resourceProjectionRow struct {
	p     ResourceProjection
	model resourceModelRef
}

// resourceReadEntry 要么是一条可用的资源（record/view 非空），
// 要么是一条"读不出来但必须让用户看见"的问题行（issue 非空）。
type resourceReadEntry struct {
	record *supply.ResourceRecord
	view   *supply.ResourceSnapshot
	issue  *supply.ResourceIssue
	model  resourceModelRef
}

func issueEntry(row resourceProjectionRow, code, message string) resourceReadEntry {
	p := row.p
	return resourceReadEntry{model: row.model, issue: &supply.ResourceIssue{Code: code, Message: message, ResourceID: p.ID, SupplierID: p.SupplierID, SupplierName: p.Supplier.Name}}
}

func (s *ResourceReadStore) load(ctx context.Context, tx *sql.Tx, where string, args []any) ([]resourceReadEntry, error) {
	// B allocates physical resources to attach draft tests. They become formal
	// supply resources only when approval materializes the authoritative A
	// control snapshot. Keep these allocations out of both lists and details.
	where = "(" + where + ") AND (NOT EXISTS (SELECT 1 FROM " + s.table("submission_row") + " sr WHERE sr.resource_id=r.id) OR EXISTS (SELECT 1 FROM " + s.table("operation_log") + " ctl WHERE ctl.entity_type='resource' AND ctl.entity_id=r.id AND ctl.action='RESOURCE_REVISION'))"
	rows, err := tx.QueryContext(ctx, `SELECT r.id::text,r.supplier_id::text,r.standard_model_id::text,r.region,r.supplier_model_name,r.coefficient::text,r.buyer_id::text,r.state,r.ever_admitted,r.latest_test_report_id::text,r.created_at,r.updated_at,admission.admitted_at,s.id::text,s.name,s.region,s.status,m.name,m.official_id,m.company,m.model_type FROM `+s.table("resource")+` r LEFT JOIN LATERAL (SELECT min(l.created_at) AS admitted_at FROM `+s.table("operation_log")+` l WHERE l.entity_type='resource' AND l.entity_id=r.id AND l.action='RESOURCE_REVISION' AND l."after"->>'operation'='ADMIT') admission ON TRUE LEFT JOIN `+s.table("supplier")+` s ON s.id=r.supplier_id LEFT JOIN `+s.table("standard_model")+` m ON m.id=r.standard_model_id WHERE `+where+` ORDER BY r.id ASC`, args...)
	if err != nil {
		return nil, err
	}
	// 扫描阶段就把"这一行读不全"挑出来，而不是让整页失败：
	// 列结构/驱动错误仍然致命（那是环境问题），行内容不合法一律降级成问题行。
	projections := []resourceProjectionRow{}
	entries := []resourceReadEntry{}
	for rows.Next() {
		var id, supplierID, modelID, region, supplierModel, coefficient, state sql.NullString
		var buyer, report, supplier, name, supplierRegion, status sql.NullString
		var modelName, modelOfficial, modelCompany, modelType sql.NullString
		var everAdmitted sql.NullBool
		var createdAt, updatedAt, admittedAt sql.NullTime
		err = rows.Scan(&id, &supplierID, &modelID, &region, &supplierModel, &coefficient, &buyer, &state, &everAdmitted, &report, &createdAt, &updatedAt, &admittedAt, &supplier, &name, &supplierRegion, &status, &modelName, &modelOfficial, &modelCompany, &modelType)
		if err != nil {
			rows.Close()
			return nil, err
		}
		model := resourceModelRef{id: modelID.String, name: modelName.String, officialID: modelOfficial.String, company: modelCompany.String, modelType: modelType.String}
		row := resourceProjectionRow{model: model}
		row.p = ResourceProjection{ID: id.String, SupplierID: supplierID.String, StandardModelID: modelID.String, Region: region.String, SupplierModelID: supplierModel.String, Coefficient: coefficient.String, State: state.String, LatestReportID: report.String}
		switch {
		case !id.Valid || !supplierID.Valid || !modelID.Valid || !region.Valid || !supplierModel.Valid || !coefficient.Valid || !state.Valid || !everAdmitted.Valid || !createdAt.Valid || !updatedAt.Valid:
			entries = append(entries, issueEntry(row, "RESOURCE_COLUMN_MISSING", "资源关键字段为空，无法展示"))
			continue
		case !supplier.Valid || !name.Valid || !supplierRegion.Valid || !status.Valid:
			entries = append(entries, issueEntry(row, "RESOURCE_SUPPLIER_MISSING", "关联的供应商资料读取不到"))
			continue
		}
		row.p.EverAdmitted = everAdmitted.Bool
		row.p.CreatedAt = createdAt.Time
		row.p.UpdatedAt = updatedAt.Time
		row.p.BuyerID = buyer.String
		row.p.Supplier = supply.ResourceSupplier{ID: supplier.String, Name: name.String, Region: supplierRegion.String, Status: status.String}
		if row.p.State == "PENDING_ADMIT" {
			row.p.State = "PENDING_ACCESS"
		}
		if admittedAt.Valid {
			value := admittedAt.Time.UTC()
			row.p.AdmittedAt = &value
		}
		projections = append(projections, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Close rows before model/fact/report queries on this same connection.
	models := map[string]*supply.StandardModel{}
	modelErrors := map[string]bool{}
	modelStore := &ModelReadStore{schema: s.schema}
	for _, row := range projections {
		p := row.p
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 采集人未分配＝还没进入正式接入流程（典型是申报行点测试时自动建的临时资源）。
		// 不再整页 500，也不再静默隐藏：挂一条问题在模型行上，让运营看得见、补得动。
		if p.BuyerID == "" {
			entries = append(entries, issueEntry(row, "RESOURCE_BUYER_MISSING", "该资源尚未分配采购人，未纳入统计"))
			continue
		}
		f, err := s.facts(ctx, tx, p)
		// facts 报的若是基础设施问题（依赖未装配、连接失败、事务中断），必须继续向上抛：
		// 把它伪装成"这条数据脏"会让真正的环境故障被一条黄色提示盖住。
		if err != nil {
			if _, dataProblem := supply.ResourceIssueOf(err); !dataProblem {
				return nil, err
			}
		}
		if err != nil || f == nil || f.ResourceID != p.ID || !f.ProjectionUpdatedAt.Equal(p.UpdatedAt) || !resourceProjectionEqual(f.Projection, p) || f.Revision == 0 || f.LatestReportSequence > math.MaxInt64 || (f.FormalConfigID == "") != (f.FormalConfigRevision == 0) {
			// 区分「还没登记」和「登记过但对不上」：前者是正常中间态（申报测试 / 手工登记），
			// 后者要人介入核对，前端给的颜色也不一样。
			code, message := "RESOURCE_CONTROL_SNAPSHOT_INCONSISTENT", "资源与控制快照不一致，请联系运营核对"
			exists, snapshotErr := s.hasControlSnapshot(ctx, tx, p.ID)
			if snapshotErr != nil {
				return nil, snapshotErr
			}
			if !exists {
				code, message = "RESOURCE_NOT_FORMALIZED", "该资源尚未完成接入登记（常见于申报测试或手工登记），未纳入统计"
			}
			entries = append(entries, issueEntry(row, code, message))
			continue
		}
		r := &supply.Resource{ID: p.ID, SupplierID: p.SupplierID, StandardModelID: p.StandardModelID, Region: p.Region, SupplierModelID: p.SupplierModelID, Coefficient: p.Coefficient, BuyerID: p.BuyerID, State: p.State, EverAdmitted: p.EverAdmitted, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
			Revision: f.Revision, OriginRowID: f.OriginRowID, OriginRowVersion: f.OriginRowVersion, TargetVersion: f.TargetVersion, FormalConfigID: f.FormalConfigID, FormalConfigRevision: f.FormalConfigRevision, LatestReportID: p.LatestReportID, LatestReportSequence: f.LatestReportSequence}
		model, exists := models[p.StandardModelID]
		if !exists {
			if modelErrors[p.StandardModelID] {
				entries = append(entries, issueEntry(row, "MODEL_UNREADABLE", "标准模型资料读取不到，请到标准模型库核对"))
				continue
			}
			model, err = modelStore.current(ctx, tx, p.StandardModelID)
			if err != nil {
				// 数据类问题（快照不合契约、模型行不存在）降级；其它（SQL/连接）仍然致命。
				if issue, dataProblem := supply.ResourceIssueOf(err); dataProblem {
					modelErrors[p.StandardModelID] = true
					row.model = resourceModelRef{id: p.StandardModelID, name: row.model.name, officialID: row.model.officialID, company: row.model.company, modelType: row.model.modelType}
					entry := issueEntry(row, issue.Code, "标准模型资料读取不到："+issue.Message)
					entries = append(entries, entry)
					continue
				}
				return nil, err
			}
			models[p.StandardModelID] = model
		}
		// 分组/去重按模型本体为准，避免同一模型一部分行用兜底列、一部分用模型，拼出两个名字。
		row.model = resourceModelRef{id: model.ID, name: model.Data.Name, officialID: model.Data.OfficialID, company: model.Data.Company, modelType: model.Data.ModelType}
		record := &supply.ResourceRecord{Resource: r, Model: model, Supplier: p.Supplier, Config: f.Config, AdmittedAt: p.AdmittedAt}
		if p.LatestReportID == "" {
			if f.Report != nil || f.LatestReportSequence != 0 {
				entries = append(entries, issueEntry(row, "RESOURCE_EVIDENCE_POINTER_INCONSISTENT", "资源标记的测试证据与生效报告不一致"))
				continue
			}
			// 没有生效证据时，仍把「最近一次实际测试」带给前端展示；
			// 它不参与准入判定（resource.go 里依旧 block TESTS_REQUIRED）。
			display, err := s.latestDisplayReport(ctx, tx, p.ID)
			if err != nil {
				if issue, dataProblem := supply.ResourceIssueOf(err); dataProblem {
					entries = append(entries, issueEntry(row, issue.Code, issue.Message))
					continue
				}
				return nil, err
			}
			record.DisplayReport = display
		} else {
			ref := f.Report
			if ref == nil || ref.ReportID != p.LatestReportID || ref.ResourceID != p.ID || ref.SupplierID != p.SupplierID || ref.StandardModelID != p.StandardModelID || ref.Region != p.Region || ref.OriginRowID != f.OriginRowID || ref.Sequence != f.LatestReportSequence {
				entries = append(entries, issueEntry(row, "RESOURCE_EVIDENCE_POINTER_INCONSISTENT", "资源标记的测试证据与生效报告不一致"))
				continue
			}
			record.Report, err = ReadSelectedReport(ctx, tx, s.schema, *ref)
			if err != nil {
				if issue, dataProblem := supply.ResourceIssueOf(err); dataProblem {
					entries = append(entries, issueEntry(row, issue.Code, issue.Message))
					continue
				}
				return nil, err
			}
		}
		view, err := supply.BuildResourceSnapshot(record)
		if err != nil {
			if issue, dataProblem := supply.ResourceIssueOf(err); dataProblem {
				entries = append(entries, issueEntry(row, issue.Code, issue.Message))
				continue
			}
			return nil, err
		}
		entries = append(entries, resourceReadEntry{record: record, view: view, model: row.model})
	}
	return entries, nil
}

// hasControlSnapshot 判断这条资源有没有正式的 A 线控制快照（RESOURCE_REVISION 操作日志）。
// 只在单行已经读不出来时才调用，用来区分「还没登记」与「登记过但对不上」。
func (s *ResourceReadStore) hasControlSnapshot(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+s.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION')`, id).Scan(&exists)
	return exists, err
}

// latestDisplayReport 取该资源「最近一次实际跑过」的测试证据，**仅用于展示**。
//
// 为什么不能直接用 latest_test_report_id：那是「当前生效、可作为准入依据」的指针，
// 审批正式化资源或办理重开时会被清空（见 native_handling_approval.go 的注释），
// 于是「明明测过」的资源在列表里显示成「未测试」（2026-11-08 供应商接入页反馈）。
//
// 平台把连通与压测拆成两个 test_job / 两份 test_report，单取一行只有一半指标，
// 所以这里取最近两份并按字段合并：连通取 connection_ms，压测取 rps/tps。
func (s *ResourceReadStore) latestDisplayReport(ctx context.Context, tx *sql.Tx, resourceID string) (*supply.ResourceReportSummary, error) {
	if !modelIdentity(resourceID) {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,connection_ms::text,rps::text,tps::text,connection_passed,stress_passed,coalesce(cache_hit_tokens::text,''),coalesce(cache_input_tokens::text,''),coalesce(cache_hit_ratio::text,''),coalesce(cache_ratio_available,false)
FROM `+s.table("test_report")+` WHERE resource_id=$1 ORDER BY tested_at DESC NULLS LAST, id DESC LIMIT 2`, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var summary *supply.ResourceReportSummary
	// 连通与压测是两份报告，结论必须各取「带该项指标的那一份」，不能都取最新一份：
	// 否则压测报告的 connection_passed=false 会把已经通过的连通测试显示成未通过。
	var connectivityDecided, pressureDecided bool
	for rows.Next() {
		var id string
		var connectionMS, rps, tps, hitTokens, inputTokens, hitRatio sql.NullString
		var ratioAvailable sql.NullBool
		var connectionPassed, stressPassed sql.NullBool
		if err := rows.Scan(&id, &connectionMS, &rps, &tps, &connectionPassed, &stressPassed, &hitTokens, &inputTokens, &hitRatio, &ratioAvailable); err != nil {
			return nil, err
		}
		if summary == nil {
			// 最新那份报告决定展示用的 report_id。
			summary = &supply.ResourceReportSummary{ID: id, Valid: true}
		}
		if !connectivityDecided && connectionMS.Valid && connectionMS.String != "" {
			summary.ConnectivityPassed = connectionPassed.Valid && connectionPassed.Bool
			connectivityDecided = true
		}
		if !pressureDecided && rps.Valid && rps.String != "" {
			summary.PressurePassed = stressPassed.Valid && stressPassed.Bool
			pressureDecided = true
		}
		takeMetric := func(target **string, value sql.NullString) {
			if *target != nil || !value.Valid || value.String == "" {
				return
			}
			copied := value.String
			*target = &copied
		}
		takeMetric(&summary.Metrics.ConnectionMS, connectionMS)
		takeMetric(&summary.Metrics.RPS, rps)
		takeMetric(&summary.Metrics.TPS, tps)
		if summary.Cache == nil && hitTokens.Valid && inputTokens.Valid && hitTokens.String != "" && inputTokens.String != "" {
			summary.Cache = &supply.ReportCacheCounters{Status: "PROVIDED", HitTokens: hitTokens.String, InputTokens: inputTokens.String}
		}
		// 平台常常只报 cache_hit_ratio_pct（没有命中/输入 token 数）：
		// 之前只认 token 明细，导致报告里明明有 88.95%，列表仍显示"未提供"（2026-11-25）。
		if summary.Cache == nil && ratioAvailable.Valid && ratioAvailable.Bool && hitRatio.Valid && hitRatio.String != "" {
			if pct, ok := normalizePercent(hitRatio.String); ok {
				summary.Cache = &supply.ReportCacheCounters{Status: "PROVIDED", HitPercent: pct}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summary, nil
}

// Supplier state may change independently. Resource columns must agree with
// their persisted control snapshot, including changes that kept updated_at.
func resourceProjectionEqual(a, b ResourceProjection) bool {
	a.Supplier = supply.ResourceSupplier{}
	b.Supplier = supply.ResourceSupplier{}
	// AdmittedAt is a read-side projection of the immutable admission operation
	// log; it is not part of the resource's mutable control snapshot.
	a.AdmittedAt = nil
	b.AdmittedAt = nil
	a.CreatedAt = a.CreatedAt.UTC()
	b.CreatedAt = b.CreatedAt.UTC()
	a.UpdatedAt = a.UpdatedAt.UTC()
	b.UpdatedAt = b.UpdatedAt.UTC()
	x, y := resourceNumber(a.Coefficient), resourceNumber(b.Coefficient)
	if x == nil || y == nil {
		return false
	}
	a.Coefficient = x.RatString()
	b.Coefficient = y.RatString()
	if a.State == "PENDING_ADMIT" {
		a.State = "PENDING_ACCESS"
	}
	if b.State == "PENDING_ADMIT" {
		b.State = "PENDING_ACCESS"
	}
	return a == b
}

func (s *ResourceReadStore) GetResource(ctx context.Context, id string) (*supply.ResourceRecord, error) {
	if !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	var record *supply.ResourceRecord
	err := s.within(ctx, func(tx *sql.Tx) error {
		rows, err := s.load(ctx, tx, "r.id=$1", []any{id})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return supply.ErrNotFound
		}
		if len(rows) != 1 {
			return supply.ErrReadContract
		}
		// 行存在但数据有问题：返回可读的类型化错误，让详情页显示"哪里不对"，
		// 而不是像以前那样 500「系统错误」。
		if rows[0].record == nil {
			if issue := rows[0].issue; issue != nil {
				return &supply.ResourceDataError{Code: issue.Code, Message: issue.Message}
			}
			return supply.ErrReadContract
		}
		record = rows[0].record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

func resourceIDCompare(a, b string) int {
	x, _ := strconv.ParseUint(a, 10, 63)
	y, _ := strconv.ParseUint(b, 10, 63)
	if x < y {
		return -1
	}
	if x > y {
		return 1
	}
	return 0
}
func resourceNumber(value string) *big.Rat { n, _ := new(big.Rat).SetString(value); return n }
func resourceMetric(e resourceReadEntry, sortBy string) *big.Rat {
	switch sortBy {
	case "coefficient":
		return resourceNumber(e.view.Coefficient)
	case "connection_ms":
		if e.view.Test.Metrics.ConnectionMS != nil {
			return resourceNumber(*e.view.Test.Metrics.ConnectionMS)
		}
	case "rps":
		if e.view.Test.Metrics.RPS != nil {
			return resourceNumber(*e.view.Test.Metrics.RPS)
		}
	case "tps":
		if e.view.Test.Metrics.TPS != nil {
			return resourceNumber(*e.view.Test.Metrics.TPS)
		}
	case "cache_hit_rate":
		cache := e.view.Test.CacheStats
		if cache.Status == "PROVIDED" {
			hit, _ := new(big.Int).SetString(cache.HitTokens, 10)
			input, _ := new(big.Int).SetString(cache.InputTokens, 10)
			return new(big.Rat).SetFrac(hit, input)
		}
	}
	return nil
}
func resourceFlatCompare(a, b resourceReadEntry, q supply.ResourceListQuery) int {
	n := 0
	switch q.SortBy {
	case "id":
		n = resourceIDCompare(a.view.ID, b.view.ID)
	case "supplier_name":
		n = strings.Compare(a.view.SupplierName, b.view.SupplierName)
	case "region":
		n = strings.Compare(a.view.Region, b.view.Region)
	case "state":
		n = strings.Compare(a.view.State, b.view.State)
	case "updated_at":
		n = a.view.UpdatedAt.Compare(b.view.UpdatedAt)
	case "admitted_at":
		if a.view.AdmittedAt == nil && b.view.AdmittedAt != nil {
			return 1
		}
		if a.view.AdmittedAt != nil && b.view.AdmittedAt == nil {
			return -1
		}
		if a.view.AdmittedAt != nil {
			n = a.view.AdmittedAt.Compare(*b.view.AdmittedAt)
		}
	default:
		x, y := resourceMetric(a, q.SortBy), resourceMetric(b, q.SortBy)
		if x == nil && y != nil {
			return 1
		}
		if x != nil && y == nil {
			return -1
		}
		if x != nil {
			n = x.Cmp(y)
		}
	}
	if q.Order == "desc" {
		n = -n
	}
	if n == 0 {
		return resourceIDCompare(a.view.ID, b.view.ID)
	}
	return n
}
func resourcePageRange(length int, q supply.ResourceListQuery) (int, int) {
	start := (q.Page - 1) * q.Size
	if start >= length {
		return length, length
	}
	return start, min(start+q.Size, length)
}
func (s *ResourceReadStore) ListResourceRows(ctx context.Context, q supply.ResourceListQuery) ([]*supply.ResourceRecord, int64, error) {
	where, args, err := resourceReadQuery(q, "flat")
	if err != nil {
		return nil, 0, err
	}
	result := []*supply.ResourceRecord{}
	var total int64
	err = s.within(ctx, func(tx *sql.Tx) error {
		loaded, err := s.load(ctx, tx, where, args)
		if err != nil {
			return err
		}
		// flat 视图是「挑一条资源去报价/接入」用的，没有位置挂问题标签：
		// 数据有问题的行不参与（由模型广场在模型行上展示原因），total 也随之只计可用行。
		entries := make([]resourceReadEntry, 0, len(loaded))
		for _, e := range loaded {
			if e.view != nil {
				entries = append(entries, e)
			}
		}
		entries = filteredResourceEntries(entries, q)
		total = int64(len(entries))
		slices.SortFunc(entries, func(a, b resourceReadEntry) int { return resourceFlatCompare(a, b, q) })
		start, end := resourcePageRange(len(entries), q)
		for _, e := range entries[start:end] {
			result = append(result, e.record)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

type resourceReadGroup struct {
	id, name, officialID, company, modelType string
	suppliers                                map[string]bool
	entries                                  []resourceReadEntry
	issues                                   []supply.ResourceIssue
	available                                int
	min, max                                 *big.Rat
}

// coefficientGroupCompare 让没有货源的模型（min/max 为 nil）无论升降序都排在最后，
// 避免「—」占满首屏而把真正有几家货源的模型挤到后面。
func coefficientGroupCompare(a, b *big.Rat, order, aID, bID string) int {
	if (a == nil) != (b == nil) {
		if a == nil {
			return 1
		}
		return -1
	}
	n := 0
	if a != nil {
		n = a.Cmp(b)
	}
	if order == "desc" {
		n = -n
	}
	if n == 0 {
		return resourceIDCompare(aID, bID)
	}
	return n
}

func resourceGroupCompare(a, b *resourceReadGroup, q supply.ResourceListQuery) int {
	n := 0
	cmpCount := func(x, y int) int {
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	}
	switch q.SortBy {
	case "id":
		n = resourceIDCompare(a.id, b.id)
	case "name":
		n = strings.Compare(a.name, b.name)
	case "company":
		n = strings.Compare(a.company, b.company)
	case "model_type":
		n = strings.Compare(a.modelType, b.modelType)
	case "supplier_count":
		n = cmpCount(len(a.suppliers), len(b.suppliers))
	case "resource_count":
		n = cmpCount(len(a.entries), len(b.entries))
	case "available_count":
		n = cmpCount(a.available, b.available)
	case "coefficient_min":
		return coefficientGroupCompare(a.min, b.min, q.Order, a.id, b.id)
	case "coefficient_max":
		return coefficientGroupCompare(a.max, b.max, q.Order, a.id, b.id)
	}
	if q.Order == "desc" {
		n = -n
	}
	if n == 0 {
		return resourceIDCompare(a.id, b.id)
	}
	return n
}
func (s *ResourceReadStore) ListResourceGroups(ctx context.Context, q supply.ResourceListQuery) ([]supply.ResourceGroupRows, int64, error) {
	where, args, err := resourceReadQuery(q, "grouped")
	if err != nil {
		return nil, 0, err
	}
	result := []supply.ResourceGroupRows{}
	var total int64
	err = s.within(ctx, func(tx *sql.Tx) error {
		entries, err := s.load(ctx, tx, where, args)
		if err != nil {
			return err
		}
		entries = filteredResourceEntries(entries, q)
		byID := map[string]*resourceReadGroup{}
		groups := []*resourceReadGroup{}
		for _, e := range entries {
			// 模型 id 都取不到的行走不了分组（连行标题都没有），只能丢弃。
			if e.model.id == "" {
				continue
			}
			g := byID[e.model.id]
			if g == nil {
				g = &resourceReadGroup{id: e.model.id, name: e.model.name, officialID: e.model.officialID, company: e.model.company, modelType: e.model.modelType, suppliers: map[string]bool{}}
				byID[g.id] = g
				groups = append(groups, g)
			}
			// 兜底列可能没有名称：后到的可用资源行/模型行会把元信息补全。
			if g.name == "" && e.model.name != "" {
				g.name, g.officialID, g.company, g.modelType = e.model.name, e.model.officialID, e.model.company, e.model.modelType
			}
			if e.view == nil {
				// 读不出来的资源：只记问题，不计入供应商数/资源数/可报价数/系数区间。
				if e.issue != nil {
					g.issues = append(g.issues, *e.issue)
				}
				continue
			}
			v := e.view
			coefficient := resourceNumber(v.Coefficient)
			if g.min == nil || g.max == nil {
				g.min, g.max = coefficient, coefficient
			}
			g.entries = append(g.entries, e)
			g.suppliers[v.SupplierID] = true
			if v.QuoteReadiness.Eligible {
				g.available++
			}
			if coefficient.Cmp(g.min) < 0 {
				g.min = coefficient
			}
			if coefficient.Cmp(g.max) > 0 {
				g.max = coefficient
			}
		}
		total = int64(len(groups))
		// 模型广场同时是招商清单：没有任何货源的标准模型也要占一行。
		// 只有模型维度的筛选（关键词/公司/类型）时补位；一旦带上供应商、部署节点、
		// 状态、系数或指标筛选，用户问的是"哪些模型有这类资源"，补位会答非所问。
		if !hasResourceDimension(q) {
			models, err := s.unsuppliedModels(ctx, tx, q, byID)
			if err != nil {
				return err
			}
			groups = append(groups, models...)
			total = int64(len(groups))
		}
		slices.SortFunc(groups, func(a, b *resourceReadGroup) int { return resourceGroupCompare(a, b, q) })
		start, end := resourcePageRange(len(groups), q)
		for _, g := range groups[start:end] {
			if len(g.entries) == 0 {
				result = append(result, supply.ResourceGroupRows{
					Model:  &supply.ResourceGroup{ID: g.id, Name: g.name, OfficialID: g.officialID, Company: g.company, ModelType: g.modelType},
					Issues: g.issues,
				})
				continue
			}
			members := make([]*supply.ResourceRecord, 0, len(g.entries))
			for _, e := range g.entries {
				members = append(members, e.record)
			}
			result = append(result, supply.ResourceGroupRows{Records: members, Issues: g.issues})
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

// hasResourceDimension 判断查询是否落在资源维度上（供应商/节点/状态/系数/指标）。
func hasResourceDimension(q supply.ResourceListQuery) bool {
	return q.SupplierID != "" || len(q.SupplierIDs) > 0 || q.Region != "" || len(q.Regions) > 0 ||
		q.State != "" || len(q.States) > 0 ||
		q.CoefficientMin != "" || q.CoefficientMax != "" ||
		q.ConnectionMin != "" || q.ConnectionMax != "" || q.RPSMin != "" || q.RPSMax != "" || q.TPSMin != "" || q.TPSMax != ""
}

// unsuppliedModels 返回符合条件的、当前没有任何资源的在用标准模型（按 m.id 升序）。
// 要求至少有一个版本：还没建档的模型连详情都打不开，放进招商清单会点出一个 500。
func (s *ResourceReadStore) unsuppliedModels(ctx context.Context, tx *sql.Tx, q supply.ResourceListQuery, known map[string]*resourceReadGroup) ([]*resourceReadGroup, error) {
	args := []any{}
	// 资料不全的模型（名称/官方 ID/公司/类型为空）不进招商清单：
	// 补位行要求字段完整，混进来会让 buildEmptyResourceGroup 返回 ErrReadContract，
	// 进而整张模型广场 500（2026-11-18 线上问题）。这类行由资料维护流程修，不在这里兜。
	where := []string{"m.status='ACTIVE'", "m.name <> ''", "m.official_id <> ''", "m.company <> ''", "m.model_type <> ''"}
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if q.Keyword != "" {
		p := add(q.Keyword)
		where = append(where, `strpos(lower(m.name||' '||m.official_id),lower(`+p+`::text))>0`)
	}
	if q.StandardModelID != "" {
		where = append(where, "m.id="+add(q.StandardModelID))
	}
	if q.Company != "" {
		where = append(where, "m.company="+add(q.Company))
	}
	if len(q.Companies) > 0 {
		params := make([]string, 0, len(q.Companies))
		for _, value := range q.Companies {
			params = append(params, add(value))
		}
		where = append(where, "m.company IN ("+strings.Join(params, ",")+")")
	}
	if q.ModelType != "" {
		where = append(where, "m.model_type="+add(q.ModelType))
	}
	if len(q.ModelTypes) > 0 {
		params := make([]string, 0, len(q.ModelTypes))
		for _, value := range q.ModelTypes {
			params = append(params, add(value))
		}
		where = append(where, "m.model_type IN ("+strings.Join(params, ",")+")")
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id::text,m.name,m.official_id,m.company,m.model_type FROM `+s.table("standard_model")+` m WHERE `+strings.Join(where, " AND ")+` AND NOT EXISTS (SELECT 1 FROM `+s.table("resource")+` r WHERE r.standard_model_id=m.id) AND EXISTS (SELECT 1 FROM `+s.table("standard_model_version")+` v WHERE v.standard_model_id=m.id) ORDER BY m.id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []*resourceReadGroup{}
	for rows.Next() {
		var g resourceReadGroup
		if err = rows.Scan(&g.id, &g.name, &g.officialID, &g.company, &g.modelType); err != nil {
			return nil, err
		}
		if known[g.id] != nil { // 资源在 unloadable 过滤后为空时仍以有货源为准
			continue
		}
		g.suppliers = map[string]bool{}
		result = append(result, &g)
	}
	return result, rows.Err()
}

func filteredResourceEntries(entries []resourceReadEntry, q supply.ResourceListQuery) []resourceReadEntry {
	result := make([]resourceReadEntry, 0, len(entries))
	for _, e := range entries {
		if e.view == nil {
			// 问题行没有系数/指标可比：带数值区间筛选时无法判断是否命中，只能不展示；
			// 其余维度（关键词/公司/类型/供应商/节点/状态）已经由 SQL 过滤过。
			if hasResourceRangeFilter(q) {
				continue
			}
			result = append(result, e)
			continue
		}
		if supply.ResourceMatchesQuery(e.view, q) {
			result = append(result, e)
		}
	}
	return result
}

// hasResourceRangeFilter 判断查询是否带系数/连通/吞吐的数值区间筛选。
func hasResourceRangeFilter(q supply.ResourceListQuery) bool {
	return q.CoefficientMin != "" || q.CoefficientMax != "" ||
		q.ConnectionMin != "" || q.ConnectionMax != "" ||
		q.RPSMin != "" || q.RPSMax != "" || q.TPSMin != "" || q.TPSMax != ""
}

// ConfigGroupOf 取资源所属的准入配置组（旁路只读查询）。
//
// 刻意不走资源投影（facts/projection）那条链路：那条链路会逐字段比对控制快照，
// 多读一列就会让整条资源被判不一致（2026-11-19 事故）。这里只回答
// "这个资源挂在哪个配置组、那个组的 URL/Key 是什么"。
func (s *ResourceReadStore) ConfigGroupOf(ctx context.Context, resourceID string) (supply.ResourceConfigGroup, error) {
	out := supply.ResourceConfigGroup{}
	err := s.within(ctx, func(tx *sql.Tx) error {
		var gid sql.NullInt64
		var name, base, key sql.NullString
		err := tx.QueryRowContext(ctx, `
SELECT sr.test_config_group_id, g.name, g.base_url, g.api_key_enc
FROM `+s.table("submission_row")+` sr
LEFT JOIN `+s.table("supplier_test_config_group")+` g ON g.id = sr.test_config_group_id
WHERE sr.resource_id = $1
ORDER BY sr.id DESC LIMIT 1`, resourceID).Scan(&gid, &name, &base, &key)
		if errors.Is(err, sql.ErrNoRows) {
			return supply.ErrNotFound
		}
		if err != nil {
			return err
		}
		if gid.Valid && gid.Int64 > 0 {
			out.GroupID = fmt.Sprintf("%d", gid.Int64)
		}
		out.GroupName = name.String
		out.BaseURL = base.String
		out.HasKey = key.String != ""
		return nil
	})
	if err != nil {
		return supply.ResourceConfigGroup{}, err
	}
	return out, nil
}

// normalizePercent 把库里的 numeric(88.9500) 规整成域对象要求的 0-100、最多两位小数。
func normalizePercent(raw string) (string, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value < 0 || value > 100 {
		return "", false
	}
	text := strconv.FormatFloat(value, 'f', 2, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" {
		return "0", true
	}
	return text, true
}
