package api

import (
	"errors"
	"strconv"

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
// GET /api/internal/workbench/operations?operator_id=&entity_type=&entity_id=&page=&size=
func (h *WorkbenchInboxHandler) Operations(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	page, size := parsePageQuery(c)
	operatorID, _ := strconv.ParseInt(c.Query("operator_id"), 10, 64)
	entityID, _ := strconv.ParseInt(c.Query("entity_id"), 10, 64)
	res, err := h.svc.Operations(c.Request.Context(), workbench.OperationQuery{
		OperatorID: operatorID,
		EntityType: c.Query("entity_type"),
		EntityID:   entityID,
		Page:       page,
		Size:       size,
	})
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
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
