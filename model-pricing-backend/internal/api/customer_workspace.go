package api

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/auth"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// CustomerWorkspaceRuntime 提供「客户管理」页合并后需要的三段只读数据：
// 客户列表（含合同/合作/办理的派生列）、客户报价、特价审批记录。
//
// 为什么是独立只读运行时而不是扩写 domain/customer：
// 9a/9b 的正式 Service 只覆盖单客户操作，没有「跨客户 + 派生列」的查询口；
// 这里全部走只读 SQL，不写库、不碰状态机，避免与 C 侧正式链路产生第二套写入口。
//
// 数据口径（缺列一律留空/派生，不编造）：
//   - 国内/海外、结算币种、税率、合同状态：000037 已落库（原型列），直接读
//   - 客户来源：customer_profile 无来源列 → 返回空串，前端显示「—」
//   - 合作阶段：存在任意报价 → 已有；否则 潜在
//   - 服务状态：有 EFFECTIVE 报价 **或** 存在未归档且 ACTIVE 的客户资源绑定 → 服务中；
//     仅有未完成报价 → 接入中；否则 潜在客户（A1：2026-11-03 加绑定口径，见 ListCustomers 注释）
//   - 办理状态：存在 DRAFT/PENDING/APPROVED 报价 → 有未完成事项；否则 无未完成事项
type CustomerWorkspaceRuntime struct{ raw *sql.DB }

func NewCustomerWorkspaceRuntime(raw *sql.DB) *CustomerWorkspaceRuntime {
	return &CustomerWorkspaceRuntime{raw: raw}
}

// CustomerWorkspaceRow 客户列表行。
type CustomerWorkspaceRow struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	LevelCode          string `json:"level_code"`
	Source             string `json:"source"`
	OwnerName          string `json:"owner_name"`
	OwnerSalesOperator int64  `json:"owner_sales_operator_id"`
	Region             string `json:"region"`
	SettlementCurrency string `json:"settlement_currency"`
	TaxRate            string `json:"tax_rate"`
	ContractStatus     string `json:"contract_status"`
	ServiceStatus      string `json:"service_status"`
	CooperationStage   string `json:"cooperation_stage"`
	HandlingStatus     string `json:"handling_status"`
	Status             string `json:"status"`
	CreditLimit        string `json:"credit_limit"`
	CreditUsed         string `json:"credit_used"`
	ActiveQuotes       int    `json:"active_quotes"`
	PendingQuotes      int    `json:"pending_quotes"`
	// OpenedModelCount 已开通模型数：按 standard_model_id 去重，只算当前生效（ACTIVE）的绑定。
	// 与原型一致（proto-app.js 的「已开通模型数」= 未归档且 state='已开通' 的模型去重计数）；
	// 禁用/禁用待确认/恢复待确认/来源待处理/停供**都不计入**——「已开通」表示"现在能用"，
	// 不是"曾经有过关系"。这些状态在客户详情「接入模型」Tab 里仍然可见，只是不进这个计数。
	OpenedModelCount int64 `json:"opened_model_count"`
}

// CustomerQuoteRow 客户报价行。
type CustomerQuoteRow struct {
	ID               int64  `json:"id"`
	CustomerID       int64  `json:"customer_id"`
	CustomerName     string `json:"customer_name"`
	VersionNo        int    `json:"version_no"`
	QuoteType        string `json:"quote_type"`
	Status           string `json:"status"`
	PriceBookVersion int    `json:"price_book_version"`
	ValidUntil       string `json:"valid_until"`
	OwnerName        string `json:"owner_name"`
	ItemCount        int    `json:"item_count"`
	// OurCompany 我方签约主体：报价记录里回填「继续编辑」时要用它，不能丢。
	OurCompany string `json:"our_company"`
	CreatedAt  string `json:"created_at"`
}

// CustomerApprovalRow 特价审批记录行。
type CustomerApprovalRow struct {
	ID           int64  `json:"id"`
	QuoteID      int64  `json:"quote_id"`
	CustomerName string `json:"customer_name"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	CreatedBy    string `json:"created_by"`
	CreatedAt    string `json:"created_at"`
}

func scopeIsSelf(op *middleware.Operator) bool {
	if op == nil {
		return false
	}
	return op.DataScope == auth.ScopeSELF || op.DataScope == auth.ScopeDEPTSUB
}

func timeText(v sql.NullTime) string {
	if !v.Valid {
		return ""
	}
	return v.Time.UTC().Format("2006-01-02T15:04:05Z")
}

// ListCustomers 客户列表 + 派生列。
func (rt *CustomerWorkspaceRuntime) ListCustomers(ctx context.Context, op *middleware.Operator, keyword string, page, size int) ([]CustomerWorkspaceRow, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, "ls.legal_name ILIKE $"+strconv.Itoa(len(args)))
	}
	if scopeIsSelf(op) {
		args = append(args, op.StaffID)
		where = append(where, "cp.owner_sales_operator_id = $"+strconv.Itoa(len(args)))
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := rt.raw.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM customer_profile cp JOIN legal_subject ls ON ls.id = cp.subject_id WHERE `+cond,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []CustomerWorkspaceRow{}, 0, nil
	}

	args = append(args, size, (page-1)*size)
	rows, err := rt.raw.QueryContext(ctx, `
		SELECT cp.id, ls.legal_name, cp.level_code, st.name, cp.owner_sales_operator_id, cp.status,
		       cp.credit_limit::text, cp.credit_used::text,
		       cp.region, cp.settlement_currency, COALESCE(cp.tax_rate::text, ''),
		       -- 合同状态按 CONTRACT 节点推导（与供应商列表同一套口径），不再读 cp.contract_status：
		       -- 那个字段只在建客户时写入默认值、全库没有任何更新路径，导致列表永远"待记录"，
		       -- 与档案里已经登记的合同完全脱节。
		       COALESCE(cf.contract_state, '暂无'),
		       COALESCE(q.active_cnt, 0), COALESCE(q.pending_cnt, 0),
		       -- 已开通模型数：与原型一致；禁用/待处理/停供不计入（只算 archived=FALSE AND state='ACTIVE'）。
		       -- 用 count(DISTINCT standard_model_id) 而不是数绑定行：与原型 new Set(...).size 同语义。
		       -- 说明：当前的 state='ACTIVE' 过滤 + 部分唯一索引 uk_crl_customer_model 已保证
		       -- 「同客户同模型最多一条 ACTIVE」，所以 DISTINCT 目前是防御性写法（实测去掉它结果不变）；
		       -- 保留它是为了让口径与原型逐字对应，且万一将来放宽索引/加状态时不会静默变错。
		       (SELECT count(DISTINCT l.standard_model_id)
		          FROM customer_resource_link l
		         WHERE l.customer_profile_id = cp.id
		           AND l.archived = FALSE
		           AND l.state = 'ACTIVE')
		  FROM customer_profile cp
		  JOIN legal_subject ls ON ls.id = cp.subject_id
		  JOIN internal_staff st ON st.id = cp.owner_sales_operator_id
		  LEFT JOIN (
		        SELECT customer_id,
		               COUNT(*) FILTER (WHERE status = 'EFFECTIVE') AS active_cnt,
		               COUNT(*) FILTER (WHERE status IN ('DRAFT','PENDING','APPROVED')) AS pending_cnt
		          FROM customer_quote
		         GROUP BY customer_id
		       ) q ON q.customer_id = cp.id
		  -- 一次聚合出全部客户的合同状态，避免逐行子查询。
		  -- 「办理中」压过「已归档」：还有在办节点却显示已归档，会让人以为这单结束了。
		  LEFT JOIN (
		        SELECT f.owner_id,
		               CASE
		                 WHEN count(*) FILTER (WHERE n.status IN ('IN_PROGRESS','WAITING_PREDECESSOR')) > 0 THEN '办理中'
		                 WHEN count(*) FILTER (WHERE n.status = 'COMPLETED') > 0 THEN '已归档'
		                 ELSE '暂无'
		               END AS contract_state
		          FROM onboarding_flow f
		          JOIN onboarding_node n ON n.flow_id = f.id AND n.node_key = 'CONTRACT' AND n.enabled
		         WHERE f.kind = 'CUSTOMER'
		         GROUP BY f.owner_id
		       ) cf ON cf.owner_id = cp.id
		 WHERE `+cond+`
		 ORDER BY cp.created_at DESC, cp.id DESC
		 LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]CustomerWorkspaceRow, 0, size)
	for rows.Next() {
		var row CustomerWorkspaceRow
		if err := rows.Scan(&row.ID, &row.Name, &row.LevelCode, &row.OwnerName, &row.OwnerSalesOperator,
			&row.Status, &row.CreditLimit, &row.CreditUsed,
			&row.Region, &row.SettlementCurrency, &row.TaxRate, &row.ContractStatus,
			&row.ActiveQuotes, &row.PendingQuotes, &row.OpenedModelCount); err != nil {
			return nil, 0, err
		}
		row.Source = ""
		// 服务状态口径（A1，2026-11-03）：**在原口径上加 OR，不推翻**。
		// 原口径只有「有生效报价才服务中」，导致客户开通全流程走完、绑定已 ACTIVE
		// 的客户在列表里仍显示「潜在客户」——用户点完会以为流程没生效。
		// 这里复用同一行已经算好的 opened_model_count（archived=FALSE AND state='ACTIVE'
		// 的绑定数）作为第二个判据：既不新增 join / 子查询（避免行数膨胀与 N+1），
		// 也保证「有生效报价的老客户」不会掉回潜在客户（无回归）。
		//
		// 注意（刻意保留的副作用，不要「优化」成只改 ServiceStatus）：
		// 这个 case **同时决定合作阶段与服务状态**——有生效绑定即视为「已有合作」，
		// 有未完成报价视为「接入中」，两者都没有才是「潜在」。
		// 理由：客户已经有在跑的服务绑定，还标「潜在」自相矛盾；
		// 若只给 ServiceStatus 赋值，会出现「合作阶段=潜在 + 服务状态=服务中」这种更难看的分叉。
		switch {
		case row.ActiveQuotes > 0 || row.OpenedModelCount > 0:
			row.CooperationStage = "合作"
			row.ServiceStatus = "服务中"
		case row.PendingQuotes > 0:
			row.CooperationStage = "合作"
			row.ServiceStatus = "接入中"
		default:
			row.CooperationStage = "潜在"
			row.ServiceStatus = "潜在客户"
		}
		if row.PendingQuotes > 0 {
			row.HandlingStatus = "有未完成事项"
		} else {
			row.HandlingStatus = "无未完成事项"
		}
		list = append(list, row)
	}
	return list, total, rows.Err()
}

// ListQuotes 客户报价列表（可按客户/状态过滤）。
func (rt *CustomerWorkspaceRuntime) ListQuotes(ctx context.Context, op *middleware.Operator, customerID int64, status, keyword string, page, size int) ([]CustomerQuoteRow, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if customerID > 0 {
		args = append(args, customerID)
		where = append(where, "cq.customer_id = $"+strconv.Itoa(len(args)))
	}
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		where = append(where, "cq.status = $"+strconv.Itoa(len(args)))
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, "ls.legal_name ILIKE $"+strconv.Itoa(len(args)))
	}
	if scopeIsSelf(op) {
		args = append(args, op.StaffID)
		where = append(where, "cq.owner_sales_operator_id = $"+strconv.Itoa(len(args)))
	}
	cond := strings.Join(where, " AND ")
	from := ` FROM customer_quote cq
		  JOIN customer_profile cp ON cp.id = cq.customer_id
		  JOIN legal_subject ls ON ls.id = cp.subject_id
		  JOIN internal_staff st ON st.id = cq.owner_sales_operator_id
		 WHERE ` + cond

	var total int
	if err := rt.raw.QueryRowContext(ctx, "SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []CustomerQuoteRow{}, 0, nil
	}

	args = append(args, size, (page-1)*size)
	rows, err := rt.raw.QueryContext(ctx, `
		SELECT cq.id, cq.customer_id, ls.legal_name, cq.version_no, cq.quote_type, cq.status,
		       cq.price_book_version, cq.valid_until, st.name,
		       (SELECT COUNT(*) FROM customer_quote_item i WHERE i.customer_quote_id = cq.id),
		       cq.our_company, cq.created_at`+from+`
		 ORDER BY cq.created_at DESC, cq.id DESC
		 LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]CustomerQuoteRow, 0, size)
	for rows.Next() {
		var row CustomerQuoteRow
		var validUntil sql.NullTime
		var created sql.NullTime
		if err := rows.Scan(&row.ID, &row.CustomerID, &row.CustomerName, &row.VersionNo, &row.QuoteType,
			&row.Status, &row.PriceBookVersion, &validUntil, &row.OwnerName, &row.ItemCount, &row.OurCompany, &created); err != nil {
			return nil, 0, err
		}
		row.ValidUntil = timeText(validUntil)
		row.CreatedAt = timeText(created)
		list = append(list, row)
	}
	return list, total, rows.Err()
}

// ListApprovals 特价审批记录（change_request.change_type = SPECIAL_PRICE）。
func (rt *CustomerWorkspaceRuntime) ListApprovals(ctx context.Context, op *middleware.Operator, quoteID int64) ([]CustomerApprovalRow, error) {
	where := []string{"cr.change_type = 'SPECIAL_PRICE'"}
	args := []any{}
	if quoteID > 0 {
		args = append(args, quoteID)
		where = append(where, "(cr.payload->>'customer_quote_id')::bigint = $"+strconv.Itoa(len(args)))
	}
	if scopeIsSelf(op) {
		args = append(args, op.StaffID)
		where = append(where, "cq.owner_sales_operator_id = $"+strconv.Itoa(len(args)))
	}
	rows, err := rt.raw.QueryContext(ctx, `
		SELECT cr.id,
		       COALESCE((cr.payload->>'customer_quote_id')::bigint, 0),
		       COALESCE(ls.legal_name, ''),
		       cr.status,
		       COALESCE(cr.payload->>'reason', ''),
		       COALESCE(cr.created_by, ''),
		       cr.created_at
		  FROM change_request cr
		  LEFT JOIN customer_quote cq ON cq.id = (cr.payload->>'customer_quote_id')::bigint
		  LEFT JOIN customer_profile cp ON cp.id = cq.customer_id
		  LEFT JOIN legal_subject ls ON ls.id = cp.subject_id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY cr.created_at DESC, cr.id DESC
		 LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]CustomerApprovalRow, 0, 16)
	for rows.Next() {
		var row CustomerApprovalRow
		var created sql.NullTime
		if err := rows.Scan(&row.ID, &row.QuoteID, &row.CustomerName, &row.Status, &row.Reason, &row.CreatedBy, &created); err != nil {
			return nil, err
		}
		row.CreatedAt = timeText(created)
		list = append(list, row)
	}
	return list, rows.Err()
}

// ---- handlers ----

type CustomerWorkspaceHandler struct{ rt *CustomerWorkspaceRuntime }

func NewCustomerWorkspaceHandler(rt *CustomerWorkspaceRuntime) *CustomerWorkspaceHandler {
	return &CustomerWorkspaceHandler{rt: rt}
}

func (h *CustomerWorkspaceHandler) ListCustomers(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.rt.ListCustomers(c.Request.Context(), op, c.Query("keyword"), page, size)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}

func (h *CustomerWorkspaceHandler) ListQuotes(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	customerID, _ := strconv.ParseInt(c.Query("customer_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.rt.ListQuotes(c.Request.Context(), op, customerID, c.Query("status"), c.Query("keyword"), page, size)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}

func (h *CustomerWorkspaceHandler) ListApprovals(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	quoteID, _ := strconv.ParseInt(c.Query("quote_id"), 10, 64)
	list, err := h.rt.ListApprovals(c.Request.Context(), op, quoteID)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list})
}
