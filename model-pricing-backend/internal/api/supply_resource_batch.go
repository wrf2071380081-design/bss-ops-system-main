package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/response"
	"strings"
)

type SupplyResourceStages interface {
	Apply(context.Context, supply.WriteMeta, string, string, string, supply.ResourceStageItem) (*supply.ResourceStageReceipt, error)
}
type resourceBatchResult struct {
	ResourceID string                       `json:"resource_id"`
	Status     int                          `json:"status"`
	Message    string                       `json:"message,omitempty"`
	Receipt    *supply.ResourceStageReceipt `json:"receipt,omitempty"`
}

func resourceBatchHandler(writer SupplyResourceStages) gin.HandlerFunc {
	return func(c *gin.Context) {
		if writer == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		if _, err := planPositiveID(c.Param("id")); err != nil {
			supplyHTTPError(c, err)
			return
		}
		var req struct {
			Action            string                     `json:"action"`
			Items             []supply.ResourceStageItem `json:"items"`
			Reason            string                     `json:"reason"`
			ExternalConfirmed bool                       `json:"external_confirmed"`
			CompletionNote    string                     `json:"completion_note"`
		}
		if err := decodeSupplyRequest(c, &req, []string{"action", "items", "reason"}); err != nil {
			supplyHTTPError(c, err)
			return
		}
		if (req.Action != "CONFIGURE" && req.Action != "CONFIRM" && req.Action != "CANCEL") || len(req.Items) == 0 || len(req.Items) > 50 {
			supplyHTTPError(c, supplyRequestError("items", "choose 1-50 resources and a valid action"))
			return
		}
		seen := map[string]bool{}
		for _, item := range req.Items {
			if seen[item.ResourceID] {
				supplyHTTPError(c, supplyRequestError("resource_id", "duplicate selection"))
				return
			}
			seen[item.ResourceID] = true
		}
		if req.Action != "CANCEL" && (!req.ExternalConfirmed || strings.TrimSpace(req.CompletionNote) == "") {
			supplyHTTPError(c, supplyRequestError("completion", "confirm actual external work and provide evidence"))
			return
		}
		meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		note := strings.TrimSpace(req.CompletionNote)
		if note == "" {
			note = meta.Reason
		}
		if len([]rune(note)) > 500 || strings.ContainsRune(note, 0) {
			supplyHTTPError(c, supplyRequestError("completion_note", "invalid completion evidence"))
			return
		}
		tx, err := supplypg.GORMModelTxFromContext(c.Request.Context())
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		results := []resourceBatchResult{}
		for i, item := range req.Items {
			savepoint := fmt.Sprintf("resource_stage_%d", i)
			if _, err = tx.ExecContext(c.Request.Context(), "SAVEPOINT "+savepoint); err != nil {
				supplyHTTPError(c, err)
				return
			}
			receipt, applyErr := writer.Apply(c.Request.Context(), meta, c.Param("id"), req.Action, note, item)
			if applyErr == nil && (receipt == nil || receipt.ResourceID != item.ResourceID) {
				applyErr = supply.ErrReadContract
			}
			result := resourceBatchResult{ResourceID: item.ResourceID, Status: 200, Receipt: receipt}
			if applyErr != nil {
				if _, err = tx.ExecContext(c.Request.Context(), "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
					supplyHTTPError(c, err)
					return
				}
				result.Status, result.Message = 500, "本条办理失败，可刷新后重试"
				var field *supply.FieldError
				switch {
				case errors.Is(applyErr, supply.ErrForbidden):
					result.Status, result.Message = 403, "只有当前指定办理人可操作"
				case errors.Is(applyErr, supply.ErrRevisionConflict):
					result.Status, result.Message = 409, "版本、办理阶段或接入前提已变化，请刷新核对"
				case errors.Is(applyErr, supply.ErrNotFound):
					result.Status, result.Message = 404, "资源不存在"
				case errors.Is(applyErr, supply.ErrDependencyUnavailable):
					result.Status, result.Message = 503, "办理依赖未就绪"
				case errors.As(applyErr, &field):
					result.Status, result.Message = 400, "本条配置或版本参数不完整"
				}
			}
			if _, err = tx.ExecContext(c.Request.Context(), "RELEASE SAVEPOINT "+savepoint); err != nil {
				supplyHTTPError(c, err)
				return
			}
			results = append(results, result)
		}
		result := gin.H{"results": results}
		middleware.SetIdempotencyResult(c, result)
		response.Success(c, result)
	}
}
