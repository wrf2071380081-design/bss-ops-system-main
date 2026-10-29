// Package api 的 customer_resource_quote.go：客户报价资源口径的 HTTP 层。
//
// 三个接口：
//   - GET  /internal/quote-candidates      报价候选资源（供应域判可报价的那些）
//   - POST /internal/customer-quotes/resource 按供应资源出单（quote_type=RESOURCE）
//   - GET  /internal/resources/:id/affected-customers 这条资源影响哪些客户
//
// 权限口径：
//   - 候选与出单：M9（客户报价）——候选是报价的输入，出单是报价动作。
//   - 受影响客户：M8（客户管理）——返回的是客户身份信息，比报价本身更敏感。
package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/customer"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// resourceQuoteItemBody 是一条资源口径明细。
type resourceQuoteItemBody struct {
	ResourceID   int64  `json:"resource_id" binding:"required"`
	PriceItemKey string `json:"price_item_key" binding:"required"`
	UnitPrice    string `json:"unit_price"`
}

// resourceQuoteBody 是资源口径报价请求体。
type resourceQuoteBody struct {
	CustomerID int64                   `json:"customer_id" binding:"required"`
	ValidTo    *time.Time              `json:"valid_to,omitempty"`
	Items      []resourceQuoteItemBody `json:"items" binding:"required"`
}

// ListResourceQuoteCandidates 报价候选资源（可报价的资源）。
//
// @Summary 客户报价候选资源（已接入且可报价）
// @Tags 客户报价
// @Security BearerAuth
// @Param keyword query string false "模型名/官方 ID/供应商名/供应模型 ID 模糊匹配"
// @Param page query int false "页码"
// @Param size query int false "每页条数，1至200"
// @Success 200 {object} response.Body "code=0；data={list,total,page,size}"
// @Failure 401,403,503 {object} response.Body
// @Router /api/internal/quote-candidates [get]
func (h *CustomerHandler) ListResourceQuoteCandidates(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	page, size := parsePageQuery(c)
	result, err := h.svc.ListResourceQuoteCandidates(c.Request.Context(), keyword, page, size)
	if err != nil {
		response.Error(c, customerErrToAppErr(err))
		return
	}
	response.Success(c, result)
}

// GenerateResourceQuote 按供应资源生成客户报价（M9:E + 幂等）。
//
// @Summary 按供应资源生成客户报价
// @Description items 每条是 (resource_id, price_item_key, unit_price)。服务端按「官方价项 × 采购系数」算采购成本，floor = 成本 /(1-最低毛利)，任一 unit_price < floor → 409（须走特价审批）。落库 status=DRAFT，version_no=MAX+1。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "幂等键"
// @Param body body resourceQuoteBody true "customer_id / items 必填"
// @Success 200 {object} response.Body "code=0；data=报价单"
// @Failure 400 {object} response.Body "code=10001 参数非法 / 资源不可报价 / 没有该价项"
// @Failure 401,403 {object} response.Body
// @Failure 409 {object} response.Body "code=10005 低于 floor（含 floor_violations[]）"
// @Router /api/internal/customer-quotes/resource [post]
func (h *CustomerHandler) GenerateResourceQuote(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	var body resourceQuoteBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	items := make([]customer.ResourceQuoteItemInput, 0, len(body.Items))
	for _, it := range body.Items {
		items = append(items, customer.ResourceQuoteItemInput{
			ResourceID: it.ResourceID, PriceItemKey: it.PriceItemKey, UnitPrice: it.UnitPrice,
		})
	}
	res, err := h.svc.GenerateResourceQuote(c.Request.Context(), customer.GenerateResourceQuoteInput{
		CustomerID: body.CustomerID, ValidTo: body.ValidTo, Items: items,
	}, scopeOf(op), op.OperatorID, requestIDOf(c))
	if err != nil {
		var floorErr *customer.ResourceQuoteFloorError
		if errors.As(err, &floorErr) {
			response.ErrorData(c, apperr.New(apperr.ErrIdempotency.Code,
				fmt.Sprintf("%s（违规 %d 项）", customer.ErrBelowFloor.Error(), len(floorErr.Violations)),
				apperr.ErrIdempotency.Status), map[string]interface{}{
				"floor_violations": floorErr.Violations,
			})
			return
		}
		response.Error(c, customerErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, res)
	response.Success(c, res)
}

// ListResourceAffectedCustomers 查这条供应资源影响哪些客户。
//
// @Summary 供应资源影响的客户
// @Tags 客户报价
// @Security BearerAuth
// @Param id path string true "资源 bigint 字符串 ID"
// @Success 200 {object} response.Body "code=0；data={resource_id,list,total}"
// @Failure 400,401,403,503 {object} response.Body
// @Router /api/internal/resources/{id}/affected-customers [get]
func (h *CustomerHandler) ListResourceAffectedCustomers(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	resourceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || resourceID <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "资源 ID 必须是正整数", apperr.ErrInvalidParams.Status))
		return
	}
	result, err := h.svc.ListCustomersAffectedByResource(c.Request.Context(), scopeOf(op), resourceID)
	if err != nil {
		response.Error(c, customerErrToAppErr(err))
		return
	}
	response.Success(c, result)
}
