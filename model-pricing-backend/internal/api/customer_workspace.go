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
//   - 客户来源：customer_profile 无来源列 → 返回空串，前端显示「—」
//   - 合同状态：库内没有客户合同表（contract 是供应商侧）→ 恒为「待记录」
//   - 合作阶段：存在 EFFECTIVE 报价 → 已有；否则 待接入
//   - 办理状态：存在 DRAFT/PENDING/APPROVED 报价 → 有未完成事项；否则 无未完成事项
type CustomerWorkspaceRuntime struct{ raw *sql.DB }

func NewCustomerWorkspaceRuntime(raw *sql.DB) *CustomerWorkspaceRuntime {
	return &CustomerWorkspaceRuntime{raw: raw}
}

// CustomerWorkspaceRow 客户列表行。
type CustomerWorkspaceRow struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	LevelCode        string `json:"level_code"`
	Source           string `json:"source"`
	OwnerName        string `json:"owner_name"`
	ContractStatus   string `json:"contract_status"`
	CooperationStage string `json:"cooperation_stage"`
	HandlingStatus   string `json:"handling_status"`
	Status           string `json:"status"`
	CreditLimit      string `json:"credit_limit"`
	CreditUsed       string `json:"credit_used"`
	ActiveQuotes     int    `json:"active_quotes"`
	PendingQuotes    int    `json:"pending_quotes"`
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
	CreatedAt        string `json:"created_at"`
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
		SELECT cp.id, ls.legal_name, cp.level_code, st.name, cp.status,
		       cp.credit_limit::text, cp.credit_used::text,
		       COALESCE(q.active_cnt, 0), COALESCE(q.pending_cnt, 0)
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
		if err := rows.Scan(&row.ID, &row.Name, &row.LevelCode, &row.OwnerName, &row.Status,
			&row.CreditLimit, &row.CreditUsed, &row.ActiveQuotes, &row.PendingQuotes); err != nil {
			return nil, 0, err
		}
		row.Source = ""
		row.ContractStatus = "待记录"
		if row.ActiveQuotes > 0 {
			row.CooperationStage = "已有"
		} else {
			row.CooperationStage = "待接入"
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
		       cq.created_at`+from+`
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
			&row.Status, &row.PriceBookVersion, &validUntil, &row.OwnerName, &row.ItemCount, &created); err != nil {
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
