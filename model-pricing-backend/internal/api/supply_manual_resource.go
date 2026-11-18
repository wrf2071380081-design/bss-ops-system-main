// Package api 的 supply_manual_resource.go：没有货源的模型，允许运营手工登记一条资源（2026-11-18）。
//
// 边界（写进代码，别在下一次改动里丢掉）：
//  1. 只对「当前零资源」的模型生效——已有货源的模型改货源要走供应商侧，不能在这里绕；
//  2. 落库状态固定 PENDING_TEST，**不是** ADMITTED：手工登记不等于准入通过，
//     仍然要过测试与准入，红线「无测试不接入」不变；
//  3. 每次登记写 operation_log，可追溯是谁、什么时候、按什么系数录的。
package api

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

type manualResourceRequest struct {
	SupplierID      string `json:"supplier_id"`
	SupplierModel   string `json:"supplier_model_name"`
	Coefficient     string `json:"coefficient"`
	Region          string `json:"region"`
	Reason          string `json:"reason"`
}

// manualResource 给零货源模型手工登记一条 PENDING_TEST 资源。
//
//	@Summary	手工登记供应资源（仅限零货源模型）
//	@Tags		供应-标准模型
//	@Security	BearerAuth
//	@Router		/api/internal/standard-models/{id}/manual-resource [post]
func (h *supplyModelHandler) manualResource(c *gin.Context) {
	if h.manualResources == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	modelID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || modelID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var req manualResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	supplierID, err := strconv.ParseInt(strings.TrimSpace(req.SupplierID), 10, 64)
	if err != nil || supplierID <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请选择供应商", 400))
		return
	}
	coefficient := strings.TrimSpace(req.Coefficient)
	if coefficient == "" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请填写采购系数（相对官方价的折扣，如 0.5200）", 400))
		return
	}
	value, err := strconv.ParseFloat(coefficient, 64)
	if err != nil || value <= 0 || value > 1 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "采购系数需大于 0 且不超过 1（例如 0.5200）", 400))
		return
	}
	region := strings.ToUpper(strings.TrimSpace(req.Region))
	if region != "CN" && region != "OVERSEAS" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "部署节点只能是 CN 或 OVERSEAS", 400))
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请填写登记说明（例如：人工补录线下已确认的货源）", 400))
		return
	}
	resourceID, err := h.manualResources.Create(c.Request.Context(), supplypg.ManualResourceInput{
		ModelID:       modelID,
		SupplierID:    supplierID,
		SupplierModel: strings.TrimSpace(req.SupplierModel),
		Coefficient:   coefficient,
		Region:        region,
		ActorID:       op.OperatorID,
		RequestID:     c.GetString("request_id"),
		Reason:        reason,
	})
	if err != nil {
		if errors.Is(err, supplypg.ErrManualResourceExists) {
			response.Error(c, apperr.New(apperr.ErrConflict.Code, err.Error(), 409))
			return
		}
		supplyHTTPError(c, err)
		return
	}
	result := gin.H{"id": strconv.FormatInt(resourceID, 10), "state": "PENDING_TEST"}
	middleware.SetIdempotencyResult(c, result)
	response.Success(c, result)
}
