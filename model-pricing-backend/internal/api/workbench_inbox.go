package api

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/workbench"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// WorkbenchInboxHandler 工作台统一收件箱。
type WorkbenchInboxHandler struct {
	svc *workbench.InboxService
}

// NewWorkbenchInboxHandler 构造。
func NewWorkbenchInboxHandler(svc *workbench.InboxService) *WorkbenchInboxHandler {
	return &WorkbenchInboxHandler{svc: svc}
}

// Inbox 收件箱列表。
// GET /api/internal/workbench/inbox?state=TODO|READ|DONE|READ_DONE&assignee_id=&biz_type=&region=&keyword=&page=&size=
func (h *WorkbenchInboxHandler) Inbox(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	page, size := parsePageQuery(c)
	assigneeID, _ := strconv.ParseInt(c.Query("assignee_id"), 10, 64)
	res, err := h.svc.Inbox(c.Request.Context(), workbench.InboxQuery{
		State:      c.Query("state"),
		BizType:    c.Query("biz_type"),
		Region:     c.Query("region"),
		Keyword:    c.Query("keyword"),
		AssigneeID: assigneeID,
		Page:       page,
		Size:       size,
	}, op.OperatorID)
	if err != nil {
		if errors.Is(err, workbench.ErrInvalidState) {
			response.Error(c, apperr.ErrInvalidParams)
			return
		}
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// People 人员选择器（只读查看他人收件箱用）。
// GET /api/internal/workbench/people
func (h *WorkbenchInboxHandler) People(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	res, err := h.svc.People(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// Operations 操作记录。
// GET /api/internal/workbench/operations?operator_id=&action=&entity_type=&entity_id=&keyword=&date_from=&date_to=&page=&size=
func (h *WorkbenchInboxHandler) Operations(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	page, size := parsePageQuery(c)
	entityID, _ := strconv.ParseInt(c.Query("entity_id"), 10, 64)
	from, err := parseOperationDate(c.Query("date_from"), false)
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	// 原型的时间区间是左闭右开：选到 11-10 就包含 11-10 全天。
	to, err := parseOperationDate(c.Query("date_to"), true)
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	res, err := h.svc.Operations(c.Request.Context(), workbench.OperationQuery{
		OperatorIDs: parseQueryIDs(c.QueryArray("operator_id")),
		Actions:     splitQueryValues(c.QueryArray("action")),
		EntityTypes: splitQueryValues(c.QueryArray("entity_type")),
		EntityID:    entityID,
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		DateFrom:    from,
		DateTo:      to,
		Page:        page,
		Size:        size,
	})
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// OperationOptions 操作记录筛选条的可选项（操作类型 / 对象类型 / 操作人）。
// GET /api/internal/workbench/operations/options
func (h *WorkbenchInboxHandler) OperationOptions(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	res, err := h.svc.OperationOptions(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// splitQueryValues 解析多选筛选值。兼容两种写法：
//   - ?action=APPROVE&action=REJECT（QueryArray）
//   - ?action=APPROVE,REJECT（前端用逗号拼，避免数组序列化成 action[]=）
func splitQueryValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			if v := strings.TrimSpace(part); v != "" {
				out = append(out, v)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseQueryIDs 解析多选的人员 id（逗号分隔或重复参数）；非法值直接跳过。
func parseQueryIDs(values []string) []int64 {
	out := make([]int64, 0, len(values))
	for _, raw := range splitQueryValues(values) {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseOperationDate 解析筛选条的日期。接受 YYYY-MM-DD 与 RFC3339；
// endExclusive=true 时把纯日期右端 +1 天（左闭右开），带时刻的原样返回。
func parseOperationDate(raw string, endExclusive bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		if endExclusive {
			t = t.AddDate(0, 0, 1)
		}
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// MarkNoticeRead 标记本人公告已读。
// POST /api/internal/workbench/notices/:id/read
func (h *WorkbenchInboxHandler) MarkNoticeRead(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	noticeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || noticeID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	if err := h.svc.MarkNoticeRead(c.Request.Context(), noticeID, op.OperatorID); err != nil {
		if errors.Is(err, workbench.ErrNoticeNotFound) {
			response.Error(c, apperr.ErrNotFound)
			return
		}
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"notice_id": noticeID})
}
