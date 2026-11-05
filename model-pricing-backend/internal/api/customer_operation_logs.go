// Package api 的 customer_operation_logs.go：客户操作记录（B3a 第 1 项）。
//
// 为什么需要这个接口：客户详情「操作记录」Tab 原本**没有可用数据源**——
//   - `/api/internal/audit-logs`（M12:V）读的是 `audit_log` 表（`repo/workbench_alert.go:274`），
//     而该表在本库并不存在（`information_schema` 只有 `operation_log`）；
//   - `/api/supplier/account/operation-logs` 只服务供应商自己（`supplier_account.go:369`）。
//
// 客户域真正的留痕写在 `operation_log`：B1 的建客户是 `entity_type='customer_profile'`，
// B2 的开通动作是 `entity_type='onboarding_flow'`。所以这里把两段合成一条时间线。
//
// 权限与数据域：`perm.M8:V` + **复用 B1 的 `customer.RegistryService.Profile`** 做存在性/数据域判定
// （它已经给出 404 / 403 语义），不在这里另写一套 SQL 过滤——避免"列表一套判定、详情另一套"的漂移。
package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/customer"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// customerOperationActionLabels 客户域动作码 → 中文文案。
//
// 前端不自己翻译：新增动作只要在这里补一行，所有端一致。
// 未知码原样返回（宁可露出英文码，也不要让前端出现空白格）。
var customerOperationActionLabels = map[string]string{
	"CREATE":            "新建客户",
	"START":             "发起开通流程",
	"COMPLETE_CONTRACT": "完成合同与授信",
	"COMPLETE_CONFIG":   "完成资源配置（开通来源）",
}

// CustomerOperationLogRow 客户操作记录行。
//
// before/after 原样返回 JSON 文本（`operation_log` 是 jsonb）：服务端不做结构化，
// 由前端决定是折叠展示还是键值展开——避免两边各写一套字段映射。
type CustomerOperationLogRow struct {
	ID           int64  `json:"id"`
	Action       string `json:"action"`
	ActionLabel  string `json:"action_label"`
	EntityType   string `json:"entity_type"`
	EntityID     int64  `json:"entity_id"`
	OperatorName string `json:"operator_name"`
	Reason       string `json:"reason"`
	Before       string `json:"before"`
	After        string `json:"after"`
	RequestID    string `json:"request_id"`
	CreatedAt    string `json:"created_at"`
}

// ListOperationLogs 读该客户的操作记录：建客户（customer_profile） + 开通流程（onboarding_flow）。
// OperationLogFilters 是原型的三个筛选：操作人 / 操作类型 / 时间区间（左闭右开）。
type OperationLogFilters struct {
	Operator string
	Action   string
	From     *time.Time
	To       *time.Time
}

func (rt *CustomerWorkspaceRuntime) ListOperationLogs(ctx context.Context, customerID int64, page, size int, filters OperationLogFilters) ([]CustomerOperationLogRow, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	// 两段留痕合并成一条时间线：
	//   - 客户档案自身的动作（B1 建客户）
	//   - 该客户的**所有**开通流程动作（B2 START / COMPLETE_CONTRACT / COMPLETE_CONFIG）
	// onboarding_flow 的 owner_id 是 customer_profile.id（迁移 000050 已纠偏注释），不是遗留 customer 表。
	where := `((l.entity_type = 'customer_profile' AND l.entity_id = $1)
	    OR (l.entity_type = 'onboarding_flow' AND l.entity_id IN (
	          SELECT f.id FROM onboarding_flow f WHERE f.kind = 'CUSTOMER' AND f.owner_id = $1)))`
	args := []any{customerID}
	if filters.Operator != "" {
		args = append(args, filters.Operator)
		where += ` AND coalesce(l.operator_name_snapshot,'')=$` + strconv.Itoa(len(args))
	}
	if filters.Action != "" {
		args = append(args, filters.Action)
		where += ` AND l.action=$` + strconv.Itoa(len(args))
	}
	if filters.From != nil {
		args = append(args, *filters.From)
		where += ` AND l.created_at >= $` + strconv.Itoa(len(args))
	}
	if filters.To != nil {
		args = append(args, *filters.To)
		where += ` AND l.created_at < $` + strconv.Itoa(len(args))
	}

	var total int
	if err := rt.raw.QueryRowContext(ctx, `SELECT count(*) FROM operation_log l WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []CustomerOperationLogRow{}, 0, nil
	}

	rows, err := rt.raw.QueryContext(ctx, `
SELECT l.id, l.entity_type, l.entity_id, l.action, COALESCE(l.operator_name_snapshot, ''),
       COALESCE(l.reason, ''), COALESCE(l."before"::text, ''), COALESCE(l."after"::text, ''),
       COALESCE(l.request_id, ''), to_char(l.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
  FROM operation_log l
 WHERE `+where+`
 ORDER BY l.created_at DESC, l.id DESC
 LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2), append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]CustomerOperationLogRow, 0, size)
	for rows.Next() {
		var row CustomerOperationLogRow
		if err := rows.Scan(&row.ID, &row.EntityType, &row.EntityID, &row.Action, &row.OperatorName,
			&row.Reason, &row.Before, &row.After, &row.RequestID, &row.CreatedAt); err != nil {
			return nil, 0, err
		}
		row.ActionLabel = customerOperationActionLabels[row.Action]
		if strings.TrimSpace(row.ActionLabel) == "" {
			row.ActionLabel = row.Action // 未知码原样返回
		}
		list = append(list, row)
	}
	return list, total, rows.Err()
}

// CustomerOperationLogHandler 客户操作记录处理器。
type CustomerOperationLogHandler struct {
	svc *customer.RegistryService
	rt  *CustomerWorkspaceRuntime
}

// NewCustomerOperationLogHandler 构造。
func NewCustomerOperationLogHandler(svc *customer.RegistryService, rt *CustomerWorkspaceRuntime) *CustomerOperationLogHandler {
	return &CustomerOperationLogHandler{svc: svc, rt: rt}
}

// ListCustomerOperationLogs 客户操作记录（M8:V）。
//
// @Summary 客户操作记录（M8:V）
// @Description 返回该客户在 operation_log 里的动作时间线：建客户（entity_type=customer_profile）与该客户的全部开通流程动作（entity_type=onboarding_flow），按时间倒序。before/after 为 JSON 文本，原样返回。数据域与客户档案一致：SELF/DEPT_SUB 只能看自己归属的客户（越权 403），客户不存在 404。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_profile.id"
// @Param page query int false "页码" default(1)
// @Param size query int false "每页（上限 100）" default(20)
// @Success 200 {object} api.APIResponse "code=0；data={list,total,page,size}"
// @Failure 400 {object} api.APIResponse "code=10001 客户 ID 非法"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:V 或客户超出数据域"
// @Failure 404 {object} api.APIResponse "code=10004 客户不存在"
// @Router /api/internal/customers/{id}/operation-logs [get]
func (h *CustomerOperationLogHandler) ListCustomerOperationLogs(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "customer id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	// 存在性 + 数据域：复用 B1 的档案服务，404/403 语义与客户详情完全一致。
	if _, err := h.svc.Profile(c.Request.Context(), id, scopeOf(op)); err != nil {
		response.Error(c, customerRegistryErrToAppErr(err))
		return
	}
	values, err := supplyQuery(c, "page", "size", "operator", "action", "from", "to")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, _ := strconv.Atoi(values.Get("page"))
	size, _ := strconv.Atoi(values.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	from, to, err := sourceOperationRange(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	list, total, err := h.rt.ListOperationLogs(c.Request.Context(), id, page, size, OperationLogFilters{
		Operator: values.Get("operator"), Action: values.Get("action"), From: from, To: to,
	})
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}
