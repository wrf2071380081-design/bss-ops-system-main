package api

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"io"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
	"net/http"
	"strings"
)

type supplyNoticeHandler struct {
	reader *supply.NoticeReader
	writer SupplyNoticeWriter
}

// @Summary 当前人员的公告阅读投影
// @Tags supply-notices
// @Security BearerAuth
// @Param region query string false "CN/OVERSEAS"
// @Param resource_id query string false "资源ID"
// @Param read_status query string false "ALL/READ/UNREAD"
// @Param keyword query string false "标题/内容/业务对象"
// @Param page query int false "页码"
// @Param size query int false "1至100"
// @Success 200 {object} response.Body{data=supply.NoticeReadPage}
// @Failure 400,401,403,503 {object} response.Body
// @Router /api/internal/notices [get]
func (h *supplyNoticeHandler) listCurrent(c *gin.Context) { h.list(c, false) }

// @Summary 指定人员的公告阅读投影（只读查询）
// @Tags supply-notices
// @Security BearerAuth
// @Param staff_ids query string false "逗号分隔StaffID，最多100个"
// @Param all_staff query boolean false "全部内部人员；不能与staff_ids并用"
// @Param region query string false "CN/OVERSEAS"
// @Param resource_id query string false "资源ID"
// @Param read_status query string false "ALL/READ/UNREAD"
// @Param keyword query string false "标题/内容/业务对象"
// @Param page query int false "页码"
// @Param size query int false "1至100"
// @Success 200 {object} response.Body{data=supply.NoticeReadPage}
// @Failure 400,401,403,503 {object} response.Body
// @Router /api/internal/notices/read-states [get]
func (h *supplyNoticeHandler) listPeople(c *gin.Context) { h.list(c, true) }
func (h *supplyNoticeHandler) list(c *gin.Context, people bool) {
	allowed := []string{"region", "resource_id", "read_status", "keyword", "page", "size"}
	if people {
		allowed = append(allowed, "staff_ids", "all_staff")
	}
	values, err := supplyQuery(c, allowed...)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	q := supply.NoticeQuery{Region: values.Get("region"), ResourceID: values.Get("resource_id"), ReadStatus: values.Get("read_status"), Keyword: values.Get("keyword"), Page: page, Size: size}
	if ids := values.Get("staff_ids"); ids != "" {
		for _, id := range strings.Split(ids, ",") {
			q.StaffIDs = append(q.StaffIDs, strings.TrimSpace(id))
		}
	}
	if value := values.Get("all_staff"); value != "" {
		if value != "true" && value != "false" {
			supplyHTTPError(c, supplyRequestError("all_staff", "expected true or false"))
			return
		}
		q.AllStaff = value == "true"
	}
	result, err := h.reader.List(c.Request.Context(), supplyActor(c), q)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}

// @Summary 公告详情及被查看人员阅读状态
// @Tags supply-notices
// @Security BearerAuth
// @Param id path string true "公告ID"
// @Param staff_id query string false "查看人员StaffID，默认当前人员"
// @Success 200 {object} response.Body{data=supply.NoticeReadView}
// @Failure 400,401,403,404,503 {object} response.Body
// @Router /api/internal/notices/{id} [get]
func (h *supplyNoticeHandler) get(c *gin.Context) {
	values, err := supplyQuery(c, "staff_id")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.Get(c.Request.Context(), supplyActor(c), c.Param("id"), values.Get("staff_id"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}

// @Summary 当前人员标记公告已读
// @Tags supply-notices
// @Security BearerAuth
// @Param id path string true "公告ID"
// @Param Idempotency-Key header string true "幂等键"
// @Success 200 {object} response.Body{data=supply.NoticeReadReceipt}
// @Failure 400,401,403,404,503 {object} response.Body
// @Router /api/internal/notices/{id}/read [post]
func (h *supplyNoticeHandler) markRead(c *gin.Context) {
	if h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err != nil {
		supplyHTTPError(c, supplyRequestError("body", "request too large"))
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		var empty struct{}
		if err := decodeSupplyRequest(c, &empty, nil); err != nil {
			supplyHTTPError(c, err)
			return
		}
	}
	meta, err := supplyWriteMeta(c, "阅读公告")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.writer.MarkRead(c.Request.Context(), meta, c.Param("id"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if result == nil || result.NoticeID != c.Param("id") || result.StaffID != meta.ActorID || result.ReadAt.IsZero() {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, result)
	response.Success(c, result)
}
